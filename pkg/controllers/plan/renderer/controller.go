// Package renderer reconciles ClusterPlans that have just acquired the
// per-cluster Beacon. Its job is to:
//
//  1. Resolve the ClusterPlanTemplate referenced by the plan.
//  2. Resolve the cluster-side entrypoint object via the cluster-type
//     adapter.
//  3. Run the rendering engine over the template body to produce a
//     full ClusterPlanSpec.
//  4. Persist the rendered Plan / Files / Probes / Instructions onto
//     ClusterPlan.Spec.
//  5. Seed Beacon.Status.ActiveSelectors from the rendered pool
//     selectors so participating system-agents can discover their work.
//  6. Mark the ClusterPlan as Rendered so the stage controller picks
//     it up.
//
// Render-on-acquire is deliberate: the rendered plan reflects cluster
// state at the moment the Beacon was actually held, not at the moment
// the trigger CR was created. This avoids stale selectors and stale
// owner-walk results that would otherwise need a re-render half-way
// through execution.
package renderer

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/render"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemplateCache reads ClusterPlanTemplate by name (cluster-scoped).
type TemplateCache interface {
	Get(name string) (*v1alpha1.ClusterPlanTemplate, error)
}

// ClusterPlanClient applies the rendered Spec mutation onto a plan.
type ClusterPlanClient interface {
	Update(*v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error)
	UpdateStatus(*v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error)
}

// BeaconCache reads the Beacon for the cluster the plan belongs to.
type BeaconCache interface {
	Get(namespace, name string) (*v1alpha1.Beacon, error)
}

// BeaconClient applies the ActiveSelectors / RegistrationEndpoint
// mutation back onto the Beacon.
type BeaconClient interface {
	UpdateStatus(*v1alpha1.Beacon) (*v1alpha1.Beacon, error)
}

// Deps bundles the dependencies the controller operates on.
type Deps struct {
	Templates           TemplateCache
	ClusterPlans        ClusterPlanClient
	Beacons             BeaconCache
	BeaconsClient       BeaconClient
	Adapters            adapter.Registry
	Engine              *render.Engine
	RegistrationURL     string // populated by the framework wiring at startup
	Now                 func() time.Time
}

// Handler is the renderer reconciler.
type Handler struct {
	deps Deps
}

// New returns an initialised handler.
func New(deps Deps) *Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Handler{deps: deps}
}

// OnChange watches ClusterPlan and renders it when the Beacon is held
// by this plan and rendering hasn't yet happened. Idempotent: subsequent
// invocations after Rendered=True are no-ops.
func (h *Handler) OnChange(_ string, plan *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	if plan == nil || plan.DeletionTimestamp != nil {
		return plan, nil
	}
	if isRendered(plan) {
		return plan, nil
	}
	if plan.Spec.TemplateRef == nil {
		// Plan was authored directly without a template — nothing to
		// render. The stage controller will pick it up once rendered
		// is somehow signalled (operator authoring path is out of
		// scope for PR1).
		return plan, nil
	}

	// Confirm Beacon is held by us. Both arms tolerate "not yet": the
	// trigger/beacon controllers will requeue this plan when state
	// changes.
	beacon, err := h.deps.Beacons.Get(plan.Namespace, plan.Spec.ClusterRef.Name)
	if apierrors.IsNotFound(err) {
		return plan, nil
	}
	if err != nil {
		return plan, fmt.Errorf("renderer: get beacon: %w", err)
	}
	if beacon.Status.Holder == nil || beacon.Status.Holder.Name.Name != plan.Name {
		return plan, nil
	}

	// Resolve the cluster-type adapter.
	clusterType := adapter.DetermineClusterType(plan.Spec.ClusterRef)
	if clusterType == "" {
		return h.fail(plan, "UnknownClusterType",
			fmt.Sprintf("clusterRef %q/%q is not a recognised cluster surface", plan.Spec.ClusterRef.APIVersion, plan.Spec.ClusterRef.Kind))
	}
	adp, ok := h.deps.Adapters.Get(clusterType)
	if !ok {
		return h.fail(plan, "AdapterNotRegistered",
			fmt.Sprintf("no adapter registered for cluster type %q", clusterType))
	}

	// Resolve the template + entrypoint.
	tmpl, err := h.deps.Templates.Get(plan.Spec.TemplateRef.Name)
	if apierrors.IsNotFound(err) {
		return h.fail(plan, "TemplateMissing",
			fmt.Sprintf("ClusterPlanTemplate %q not found", plan.Spec.TemplateRef.Name))
	}
	if err != nil {
		return plan, fmt.Errorf("renderer: get template: %w", err)
	}
	entry, err := adp.GetClusterEntrypoint(context.Background(), plan.Spec.ClusterRef)
	if err != nil {
		return h.fail(plan, "EntrypointResolutionFailed",
			fmt.Sprintf("could not resolve entrypoint: %v", err))
	}

	// Render.
	rendered, err := h.deps.Engine.RenderClusterPlan(tmpl.Spec.Plan, render.ClusterPlanContext{
		Cluster: render.ClusterReference{
			Type:      clusterType,
			Namespace: plan.Spec.ClusterRef.Namespace,
			Name:      plan.Spec.ClusterRef.Name,
		},
		Entrypoint: entry.Object,
		Inputs:     plan.Spec.Inputs,
		Timestamp:  h.deps.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return h.fail(plan, "RenderFailed",
			fmt.Sprintf("template %q failed to render: %v", tmpl.Name, err))
	}

	// Persist the rendered Spec fields.
	out := plan.DeepCopy()
	out.Spec.Plan = rendered.Plan
	out.Spec.Files = rendered.Files
	out.Spec.Probes = rendered.Probes
	out.Spec.Instructions = rendered.Instructions
	updated, err := h.deps.ClusterPlans.Update(out)
	if err != nil {
		return plan, fmt.Errorf("renderer: update spec: %w", err)
	}

	// Compute ActiveSelectors from the rendered plan and lift them
	// onto the Beacon. System-agents on matching nodes will see the
	// activation and curl the registration endpoint to receive their
	// NodePlan-scoped kubeconfig (PR6).
	if err := h.updateBeaconActivation(beacon, rendered); err != nil {
		// Non-fatal: log via wrapped error. The stage controller can
		// still drive execution without the Beacon's selectors —
		// they're an optimisation, not a correctness requirement
		// until PR6.
		return updated, fmt.Errorf("renderer: update beacon activation (non-fatal): %w", err)
	}

	// Mark the plan rendered so the stage controller picks it up.
	updated = updated.DeepCopy()
	updated.Status.ObservedGeneration = updated.Generation
	if updated.Status.Phase == "" || updated.Status.Phase == v1alpha1.ClusterPlanPhasePending || updated.Status.Phase == v1alpha1.ClusterPlanPhaseAcquiring {
		updated.Status.Phase = v1alpha1.ClusterPlanPhaseRunning
	}
	setCondition(&updated.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionRendered,
		Status:             metav1.ConditionTrue,
		Reason:             "TemplateRendered",
		Message:            fmt.Sprintf("ClusterPlanTemplate %q rendered into %d pool(s)", tmpl.Name, len(rendered.Plan)),
		LastTransitionTime: metav1.NewTime(h.deps.Now()),
	})
	return h.deps.ClusterPlans.UpdateStatus(updated)
}

// updateBeaconActivation populates Beacon.Status.ActiveSelectors with
// the union of pool selectors and Beacon.Status.RegistrationEndpoint
// with the framework's startup-injected URL.
func (h *Handler) updateBeaconActivation(beacon *v1alpha1.Beacon, rendered *v1alpha1.ClusterPlanSpec) error {
	out := beacon.DeepCopy()
	out.Status.ActiveSelectors = collectActiveSelectors(rendered)
	if h.deps.RegistrationURL != "" {
		out.Status.RegistrationEndpoint = h.deps.RegistrationURL
	}
	_, err := h.deps.BeaconsClient.UpdateStatus(out)
	return err
}

// collectActiveSelectors gathers every NodePoolPlan.Selector and
// Election.Selector from the rendered spec so system-agents on
// non-matching nodes can stay idle. Cluster-wide Files/Probes/
// Instructions selectors are also included because they too constitute
// "this op cares about that node".
func collectActiveSelectors(spec *v1alpha1.ClusterPlanSpec) []metav1.LabelSelector {
	var out []metav1.LabelSelector
	for _, p := range spec.Plan {
		if p.Selector != nil {
			out = append(out, *p.Selector)
		}
		if p.Election != nil && len(p.Election.Selector.MatchLabels)+len(p.Election.Selector.MatchExpressions) > 0 {
			out = append(out, p.Election.Selector)
		}
	}
	for _, f := range spec.Files {
		out = append(out, f.Selector...)
	}
	for _, pr := range spec.Probes {
		out = append(out, pr.Selector...)
	}
	for _, i := range spec.Instructions {
		out = append(out, i.Selector...)
	}
	return out
}

// fail records a render-side terminal failure and returns the plan.
func (h *Handler) fail(plan *v1alpha1.ClusterPlan, reason, message string) (*v1alpha1.ClusterPlan, error) {
	out := plan.DeepCopy()
	out.Status.ObservedGeneration = plan.Generation
	out.Status.Phase = v1alpha1.ClusterPlanPhaseFailed
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionRendered,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(h.deps.Now()),
	})
	return h.deps.ClusterPlans.UpdateStatus(out)
}

// --- helpers ----------------------------------------------------------------

func isRendered(plan *v1alpha1.ClusterPlan) bool {
	for _, c := range plan.Status.Conditions {
		if c.Type == v1alpha1.ConditionRendered && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func setCondition(conds *[]metav1.Condition, c metav1.Condition) {
	for i := range *conds {
		if (*conds)[i].Type == c.Type {
			(*conds)[i] = c
			return
		}
	}
	*conds = append(*conds, c)
}
