package render

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"sigs.k8s.io/yaml"
)

// Engine is the entry point for rendering ClusterPlanTemplates and
// per-NodePlan strings. Construct via New; concurrent calls are safe so
// long as the supplied RenderClient is concurrency-safe.
type Engine struct {
	client RenderClient
	now    func() time.Time
}

// New returns an Engine wired with the supplied sandboxed client. The now
// callback is overridable for deterministic test output.
func New(client RenderClient, now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{client: client, now: now}
}

// RenderClusterPlan renders a ClusterPlanTemplate body into a
// ClusterPlanSpec. The body must produce strict YAML for ClusterPlanSpec
// once template substitution and the role-DSL pre-processor have run.
//
// Outputs / shard / getCapture are intentionally absent from the funcMap
// available here — references to them at outer-render time will fail to
// parse, which surfaces author error early.
func (e *Engine) RenderClusterPlan(body string, ctx ClusterPlanContext) (*v1alpha1.ClusterPlanSpec, error) {
	rendered, err := e.renderText(body, e.outerFuncs(ctx), ctx)
	if err != nil {
		return nil, fmt.Errorf("RenderClusterPlan: %w", err)
	}
	var spec v1alpha1.ClusterPlanSpec
	if err := yaml.UnmarshalStrict([]byte(rendered), &spec); err != nil {
		return nil, fmt.Errorf("RenderClusterPlan: unmarshal: %w\nrendered:\n%s", err, rendered)
	}
	return &spec, nil
}

// RenderNodePlanSpec re-renders the templated string fields of a
// structured NodePlanSpec at the moment a NodePlan is about to be
// emitted. Files[].Content, Instructions[].Command/Args/Env, and
// HTTPGetAction string fields all flow through the inner funcMap, which
// includes output / shard / getCapture for cross-stage references.
//
// The input is not mutated; a deep copy is returned.
func (e *Engine) RenderNodePlanSpec(spec v1alpha1.NodePlanSpec, ctx NodePlanContext) (*v1alpha1.NodePlanSpec, error) {
	out := spec.DeepCopy()
	funcs := e.innerFuncs(ctx)

	for i := range out.Files {
		v, err := e.renderText(out.Files[i].Content, funcs, ctx)
		if err != nil {
			return nil, fmt.Errorf("Files[%d].Content: %w", i, err)
		}
		out.Files[i].Content = v
	}
	for i := range out.Instructions {
		ins := &out.Instructions[i]
		v, err := e.renderText(ins.Command, funcs, ctx)
		if err != nil {
			return nil, fmt.Errorf("Instructions[%d].Command: %w", i, err)
		}
		ins.Command = v
		for j, a := range ins.Args {
			v, err := e.renderText(a, funcs, ctx)
			if err != nil {
				return nil, fmt.Errorf("Instructions[%d].Args[%d]: %w", i, j, err)
			}
			ins.Args[j] = v
		}
		for j, ev := range ins.Env {
			v, err := e.renderText(ev, funcs, ctx)
			if err != nil {
				return nil, fmt.Errorf("Instructions[%d].Env[%d]: %w", i, j, err)
			}
			ins.Env[j] = v
		}
	}
	for i := range out.Probes {
		if h := out.Probes[i].HTTPGetAction; h != nil {
			v, err := e.renderText(h.URL, funcs, ctx)
			if err != nil {
				return nil, fmt.Errorf("Probes[%d].HTTPGetAction.URL: %w", i, err)
			}
			h.URL = v
		}
	}
	return out, nil
}

// RenderCriterion evaluates a single NodeElection.Criteria expression
// and returns true iff the rendered, trimmed result equals "true"
// (case-insensitive). Any rendering error is propagated; the caller
// decides how to surface it (typically: keep election in pending state).
func (e *Engine) RenderCriterion(expr string, ctx ElectionContext) (bool, error) {
	out, err := e.renderText(expr, e.electionFuncs(ctx), ctx)
	if err != nil {
		return false, err
	}
	out = trimSpace(out)
	return equalFold(out, "true"), nil
}

// renderText is the shared low-level primitive: pre-process the body for
// role-DSL sugar, parse with the supplied funcMap, execute against the
// supplied data. Errors include the rendered body when relevant for
// debuggability.
func (e *Engine) renderText(body string, funcs template.FuncMap, data any) (string, error) {
	pre, err := preprocess(body)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New("plan").Funcs(funcs).Parse(pre)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}
	return buf.String(), nil
}

// trimSpace and equalFold are tiny helpers kept inline so the engine has
// no incidental dependencies beyond the stdlib + apis sub-module.
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
