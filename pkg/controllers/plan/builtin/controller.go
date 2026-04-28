// Package builtin ensures the embedded ClusterPlanTemplates Rancher
// ships out-of-the-box are present on the management cluster. The set
// is read from pkg/clusterplan/builtin and applied via wrangler-style
// apply with a stable SetID so removing a template from the embed
// causes a corresponding garbage-collect on the cluster.
//
// The controller is intentionally small: it has one entry point
// (ApplyBuiltins) and one Register hook that calls it at startup.
// Drift correction (re-applying when an operator deletes a builtin
// out from under us) is a deliberate follow-up — the wrangler apply
// uses --prune semantics so an operator who *modifies* a builtin will
// see Rancher revert it on the next ApplyBuiltins call.
package builtin

import (
	"context"
	"fmt"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	pkgbuiltin "github.com/rancher/rancher/pkg/clusterplan/builtin"
	"k8s.io/apimachinery/pkg/runtime"
)

// SetID is the wrangler-apply set identifier under which all built-in
// templates are owned. Operators who want to take ownership of a
// shipped builtin simply remove the v1alpha1.BuiltinLabel; the apply
// then leaves the template alone.
const SetID = "plan-builtin"

// Applier is the apply surface this controller uses. Production code
// passes wrangler/v3/pkg/apply.Apply via a small wrapper; tests pass a
// fake that records what was applied.
type Applier interface {
	ApplyObjects(setID string, objects ...runtime.Object) error
}

// Handler holds the dependencies needed to materialise the embedded
// builtins on the management cluster.
type Handler struct {
	applier Applier
}

// New returns an initialised handler.
func New(applier Applier) *Handler { return &Handler{applier: applier} }

// ApplyBuiltins reads the embedded templates and applies them to the
// management cluster under SetID. Safe to call repeatedly: wrangler
// apply diffs by SetID and only sends what changed (or what is no
// longer in the set).
func (h *Handler) ApplyBuiltins(_ context.Context) error {
	tmpls, err := pkgbuiltin.ClusterPlanTemplates()
	if err != nil {
		return fmt.Errorf("builtin controller: load templates: %w", err)
	}
	objs := make([]runtime.Object, 0, len(tmpls))
	for _, t := range tmpls {
		objs = append(objs, t)
	}
	if err := h.applier.ApplyObjects(SetID, objs...); err != nil {
		return fmt.Errorf("builtin controller: apply: %w", err)
	}
	return nil
}

// Register calls ApplyBuiltins once. The framework wiring (in
// pkg/controllers/plan/plan.go::Register, future task) is responsible
// for invoking this exactly once at startup, after the
// ClusterPlanTemplate CRDs have been ensured.
func (h *Handler) Register(ctx context.Context) error {
	return h.ApplyBuiltins(ctx)
}

// templatesByName is a small helper returning a map keyed on
// metadata.name; reserved for future drift-detection logic.
func templatesByName(tmpls []*v1alpha1.ClusterPlanTemplate) map[string]*v1alpha1.ClusterPlanTemplate {
	out := make(map[string]*v1alpha1.ClusterPlanTemplate, len(tmpls))
	for _, t := range tmpls {
		out[t.Name] = t
	}
	return out
}
