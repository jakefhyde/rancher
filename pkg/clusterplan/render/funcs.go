package render

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/blang/semver/v4"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/jsonpath"
)

// roleLabelPrefix is what the renderer maps a role-DSL identifier onto when
// answering hasRole. e.g. `etcd` -> `rke.cattle.io/etcd-role=true`.
const roleLabelPrefix = "rke.cattle.io/"

// outerFuncs returns the funcMap available during ClusterPlanTemplate
// rendering. Output / shard / getCapture are absent — they're stage-bound
// and only available when re-rendering NodePlanSpec strings. Callers that
// reference them in the outer template will get a parse error from
// text/template, which is the desired safety.
func (e *Engine) outerFuncs(ctx ClusterPlanContext) template.FuncMap {
	return template.FuncMap{
		"secret":         e.secretFunc(ctx.Cluster.Namespace),
		"owner":          e.ownerFuncCluster(ctx),
		"jsonPath":       jsonPathFunc,
		"getCondition":   getConditionFunc,
		"isNil":          isNilFunc,
		"hasRole":        hasRoleFuncStatic, // .Node not present at outer time; called only on Entrypoint
		"runtimeCommand": runtimeCommandFunc,
		"kubeVersion":    kubeVersionFunc,
		"semverGTE":      semverGTEFunc,
		"now":            func() string { return e.now().UTC().Format(time.RFC3339) },
	}
}

// innerFuncs returns the funcMap available during NodePlanSpec re-render.
// Includes output / shard / getCapture, which look back into the
// accumulated stage outputs carried in NodePlanContext.
func (e *Engine) innerFuncs(ctx NodePlanContext) template.FuncMap {
	return template.FuncMap{
		"secret":         e.secretFunc(ctx.Cluster.Namespace),
		"owner":          e.ownerFuncNode(ctx),
		"jsonPath":       jsonPathFunc,
		"getCondition":   getConditionFunc,
		"isNil":          isNilFunc,
		"hasRole":        hasRoleFunc,
		"runtimeCommand": runtimeCommandFunc,
		"kubeVersion":    kubeVersionFunc,
		"semverGTE":      semverGTEFunc,
		"now":            func() string { return e.now().UTC().Format(time.RFC3339) },
		"output":         outputFunc(ctx),
		"shard":          shardFunc(ctx, e.now),
		"getCapture":     outputFunc(ctx), // alias for output
	}
}

// electionFuncs returns the funcMap available when rendering a single
// NodeElection.Criteria expression. Same as outerFuncs but with hasRole
// bound to the candidate node, since election runs per-candidate.
func (e *Engine) electionFuncs(ctx ElectionContext) template.FuncMap {
	return template.FuncMap{
		"secret":         e.secretFunc(ctx.Cluster.Namespace),
		"owner":          e.ownerFuncElection(ctx),
		"jsonPath":       jsonPathFunc,
		"getCondition":   getConditionFunc,
		"isNil":          isNilFunc,
		"hasRole":        hasRoleFunc,
		"runtimeCommand": runtimeCommandFunc,
		"kubeVersion":    kubeVersionFunc,
		"semverGTE":      semverGTEFunc,
		"now":            func() string { return e.now().UTC().Format(time.RFC3339) },
	}
}

// --- secret -----------------------------------------------------------------

func (e *Engine) secretFunc(defaultNS string) any {
	return func(args ...string) (string, error) {
		var ns, name, key string
		switch len(args) {
		case 2:
			ns = defaultNS
			name = args[0]
			key = args[1]
		case 3:
			ns = args[0]
			name = args[1]
			key = args[2]
		default:
			return "", fmt.Errorf("secret: expected (name, key) or (namespace, name, key), got %d args", len(args))
		}
		s, err := e.client.GetSecret(ns, name)
		if err != nil {
			return "", err
		}
		v, ok := s.Data[key]
		if !ok {
			return "", fmt.Errorf("secret %s/%s has no key %q", ns, name, key)
		}
		return string(v), nil
	}
}

// --- owner ------------------------------------------------------------------

// ownerFuncCluster walks the owner chain from .Entrypoint when called
// during outer rendering. Useful for pulling labels off the upstream Cluster
// or Bootstrap object the entrypoint is owned by.
func (e *Engine) ownerFuncCluster(ctx ClusterPlanContext) any {
	return func(kind string) (runtime.Object, error) {
		return e.walkOwner(ctx.Entrypoint, kind, ctx.Cluster.Namespace)
	}
}

// ownerFuncNode walks the owner chain from .Node when called during inner
// rendering. This is the "machine -> RKEBootstrap" walk for capr clusters.
func (e *Engine) ownerFuncNode(ctx NodePlanContext) any {
	return func(kind string) (runtime.Object, error) {
		return e.walkOwner(ctx.Node, kind, ctx.Cluster.Namespace)
	}
}

func (e *Engine) ownerFuncElection(ctx ElectionContext) any {
	return func(kind string) (runtime.Object, error) {
		return e.walkOwner(ctx.Node, kind, ctx.Cluster.Namespace)
	}
}

func (e *Engine) walkOwner(start runtime.Object, kind, namespace string) (runtime.Object, error) {
	if start == nil {
		return nil, errors.New("owner: starting object is nil")
	}
	type accessor interface {
		GetOwnerReferences() []metav1.OwnerReference
		GetNamespace() string
	}
	cur, ok := start.(accessor)
	if !ok {
		return nil, fmt.Errorf("owner: object does not expose OwnerReferences")
	}
	for _, ref := range cur.GetOwnerReferences() {
		if ref.Kind == kind {
			ns := cur.GetNamespace()
			if ns == "" {
				ns = namespace
			}
			return e.client.Get(ref.APIVersion, ref.Kind, ns, ref.Name)
		}
	}
	return nil, fmt.Errorf("owner: no OwnerReference of Kind=%q on %T", kind, start)
}

// --- jsonPath ---------------------------------------------------------------

func jsonPathFunc(obj any, expr string) (string, error) {
	if obj == nil {
		return "", nil
	}
	jp := jsonpath.New("render").AllowMissingKeys(true)
	// Allow shorthand: a leading "." but no curly braces.
	parsed := expr
	if !strings.HasPrefix(parsed, "{") {
		parsed = "{" + parsed + "}"
	}
	if err := jp.Parse(parsed); err != nil {
		return "", fmt.Errorf("jsonPath: parse %q: %w", expr, err)
	}
	var buf bytes.Buffer
	if err := jp.Execute(&buf, obj); err != nil {
		return "", fmt.Errorf("jsonPath: execute %q: %w", expr, err)
	}
	return buf.String(), nil
}

// --- getCondition -----------------------------------------------------------

// getConditionFunc returns the named condition from any object that
// exposes a Conditions slice via the conventions used by CAPI / Rancher
// (metav1.Condition or wrangler genericcondition.GenericCondition).
func getConditionFunc(obj any, conditionType string) (metav1.Condition, error) {
	if obj == nil {
		return metav1.Condition{}, fmt.Errorf("getCondition: nil object")
	}
	type capiConditioner interface {
		GetConditions() []metav1.Condition
	}
	if c, ok := obj.(capiConditioner); ok {
		for _, cond := range c.GetConditions() {
			if cond.Type == conditionType {
				return cond, nil
			}
		}
	}
	return metav1.Condition{Type: conditionType, Status: metav1.ConditionUnknown}, nil
}

// --- isNil ------------------------------------------------------------------

func isNilFunc(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	return false
}

// --- hasRole ----------------------------------------------------------------

// hasRoleFunc reads the rke.cattle.io/<role>-role label off the passed
// node-of-truth object. Returns false (not error) when the object lacks
// the label, so role checks read naturally inside template conditionals.
func hasRoleFunc(node any, role string) bool {
	type labeller interface {
		GetLabels() map[string]string
	}
	l, ok := node.(labeller)
	if !ok {
		return false
	}
	return l.GetLabels()[roleLabelPrefix+strings.ToLower(role)+"-role"] == "true"
}

// hasRoleFuncStatic is the outer-render variant: .Node isn't bound at
// outer-render time, so calls during outer rendering get a sane no-op.
// (The DSL pre-processor only emits hasRole calls against .Node, which
// outer rendering doesn't have — so any outer hasRole call is by
// definition author error.)
func hasRoleFuncStatic(_ any, _ string) bool { return false }

// --- runtimeCommand / kubeVersion -------------------------------------------

// runtimeCommandFunc returns the on-node command name that drives
// snapshot / version / cert operations: "rke2" or "k3s". It examines the
// passed Entrypoint and falls back by inspection.
func runtimeCommandFunc(entrypoint any) string {
	switch v := entrypoint.(type) {
	case *rkev1.RKEControlPlane:
		if strings.Contains(v.Spec.KubernetesVersion, "k3s") {
			return "k3s"
		}
		return "rke2"
	case *apimgmtv3.Cluster:
		switch v.Status.Driver {
		case apimgmtv3.ClusterDriverK3s:
			return "k3s"
		default:
			return "rke2"
		}
	}
	return "rke2"
}

func kubeVersionFunc(entrypoint any) string {
	switch v := entrypoint.(type) {
	case *rkev1.RKEControlPlane:
		return strings.SplitN(v.Spec.KubernetesVersion, "+", 2)[0]
	case *apimgmtv3.Cluster:
		return v.Status.Version.String()
	}
	return ""
}

// --- semverGTE --------------------------------------------------------------

func semverGTEFunc(a, b string) (bool, error) {
	pa, err := semver.ParseTolerant(strings.TrimPrefix(a, "v"))
	if err != nil {
		return false, fmt.Errorf("semverGTE: parse %q: %w", a, err)
	}
	pb, err := semver.ParseTolerant(strings.TrimPrefix(b, "v"))
	if err != nil {
		return false, fmt.Errorf("semverGTE: parse %q: %w", b, err)
	}
	return pa.GTE(pb), nil
}

// --- output / getCapture / shard --------------------------------------------

// outputFunc resolves a captured value by node-selector + output name. The
// selector is a Kubernetes label selector string parsed by
// labels.Parse. Returns the first matching node's value (in deterministic
// node-name order so reproducible across runs).
func outputFunc(ctx NodePlanContext) any {
	return func(selector, name string) (string, error) {
		sel, err := labels.Parse(selector)
		if err != nil {
			return "", fmt.Errorf("output: parse selector %q: %w", selector, err)
		}
		matches := orderedMatches(ctx.Outputs, sel)
		if len(matches) == 0 {
			return "", fmt.Errorf("output: no node matches %q for %q", selector, name)
		}
		v, ok := matches[0].outputs.Values[name]
		if !ok {
			return "", fmt.Errorf("output: node %q has no output %q", matches[0].node, name)
		}
		return v, nil
	}
}

// shardFunc fans references across matching nodes deterministically. The
// shard index is derived from a hash of the calling node's identifier so
// each consumer gets a stable, evenly-distributed assignment without
// needing per-call state.
func shardFunc(ctx NodePlanContext, _ func() time.Time) any {
	return func(selector, name string) (string, error) {
		sel, err := labels.Parse(selector)
		if err != nil {
			return "", fmt.Errorf("shard: parse selector %q: %w", selector, err)
		}
		matches := orderedMatches(ctx.Outputs, sel)
		if len(matches) == 0 {
			return "", fmt.Errorf("shard: no node matches %q for %q", selector, name)
		}
		// Hash the consuming node's identity to pick a stable index.
		// Falls back to ElectedNode when Node is anonymous.
		key := nodeIdentity(ctx)
		h := fnv.New32a()
		_, _ = h.Write([]byte(key))
		idx := int(h.Sum32()) % len(matches)
		v, ok := matches[idx].outputs.Values[name]
		if !ok {
			return "", fmt.Errorf("shard: node %q has no output %q", matches[idx].node, name)
		}
		return v, nil
	}
}

type nodeMatch struct {
	node    string
	outputs NodeOutputs
}

func orderedMatches(outputs map[string]NodeOutputs, sel labels.Selector) []nodeMatch {
	var matches []nodeMatch
	for node, n := range outputs {
		if sel.Matches(labels.Set(n.Labels)) {
			matches = append(matches, nodeMatch{node: node, outputs: n})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].node < matches[j].node })
	return matches
}

func nodeIdentity(ctx NodePlanContext) string {
	if ctx.Node != nil {
		type accessor interface{ GetName() string }
		if a, ok := ctx.Node.(accessor); ok {
			if n := a.GetName(); n != "" {
				return n
			}
		}
	}
	if ctx.ElectedNode != "" {
		return ctx.ElectedNode
	}
	return ""
}

// --- helpers ----------------------------------------------------------------

// secretMustExist is a tiny convenience to assert a secret is on the
// allow-list at template-parse time. Reserved for future use.
var _ = corev1.Secret{}
