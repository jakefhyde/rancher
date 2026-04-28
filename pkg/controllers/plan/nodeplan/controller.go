// Package nodeplan delivers each NodePlan to its target node's
// system-agent transport and reflects the agent's reply back into
// NodePlan.Status.
//
// Two distinct reconcile triggers feed this controller:
//
//   1. NodePlan.Spec changes — the controller re-renders any inner
//      template strings via pkg/clusterplan/render, marshals via
//      pkg/clusterplan/wire, and calls adapter.WriteNodePlan to
//      deliver. The NodePlan is then marked Phase=Running so the
//      stage controller can observe in-flight execution.
//
//   2. Reconcile by enqueue — the controller asks the adapter for the
//      latest agent-side status and merges it into NodePlan.Status.
//      Production wiring should arrange for the underlying transport
//      (the rke.cattle.io/machine-plan secret) to enqueue this
//      controller on change so polling is unnecessary; for now we
//      treat every reconcile as both a deliver and a status-read.
//
// The controller is intentionally cluster-type-agnostic — every
// per-type concern is on the other side of adapter.Registry.
package nodeplan

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/render"
	"github.com/rancher/rancher/pkg/clusterplan/wire"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePlanClient mutates NodePlan status.
type NodePlanClient interface {
	UpdateStatus(*v1alpha1.NodePlan) (*v1alpha1.NodePlan, error)
}

// ClusterPlanCache reads the parent ClusterPlan to recover the
// cluster-type discriminator and ClusterRef.
type ClusterPlanCache interface {
	Get(namespace, name string) (*v1alpha1.ClusterPlan, error)
}

// Deps bundles the dependencies the controller operates on.
type Deps struct {
	NodePlans     NodePlanClient
	ClusterPlans  ClusterPlanCache
	Adapters      adapter.Registry
	Engine        *render.Engine
	Now           func() time.Time
}

// Handler is the nodeplan reconciler.
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

// OnChange reconciles a NodePlan: deliver if not yet delivered, then
// merge agent-side status. Production wiring should arrange for the
// adapter's transport to enqueue this controller when the agent writes
// status, so the merge happens promptly.
func (h *Handler) OnChange(_ string, np *v1alpha1.NodePlan) (*v1alpha1.NodePlan, error) {
	if np == nil || np.DeletionTimestamp != nil {
		return np, nil
	}

	parent, err := h.parent(np)
	if err != nil {
		// Parent missing can happen during cascading delete; treat as
		// no-op rather than erroring.
		return np, nil
	}
	clusterType := adapter.DetermineClusterType(parent.Spec.ClusterRef)
	if clusterType == "" {
		return h.fail(np, "UnknownClusterType",
			fmt.Sprintf("parent ClusterPlan ClusterRef %q/%q is not recognised", parent.Spec.ClusterRef.APIVersion, parent.Spec.ClusterRef.Kind))
	}
	adp, ok := h.deps.Adapters.Get(clusterType)
	if !ok {
		return h.fail(np, "AdapterNotRegistered",
			fmt.Sprintf("no adapter registered for cluster type %q", clusterType))
	}

	// Render any inner template strings (Files[].Content,
	// Instructions[].Command/Args/Env, Probes[].HTTPGetAction.URL)
	// before marshalling to the wire format. For the etcd-snapshot
	// operation the inlined NodePlanSpec is already concrete (no
	// inner templates); the call is a no-op but keeps the contract
	// consistent for richer ops in the future.
	rendered, err := h.deps.Engine.RenderNodePlanSpec(np.Spec, render.NodePlanContext{
		Cluster: render.ClusterReference{
			Type:      clusterType,
			Namespace: parent.Spec.ClusterRef.Namespace,
			Name:      parent.Spec.ClusterRef.Name,
		},
		ClusterType: clusterType,
		Inputs:      parent.Spec.Inputs,
		Timestamp:   h.deps.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return h.fail(np, "RenderFailed",
			fmt.Sprintf("inner render failed: %v", err))
	}

	wirePlan, err := wire.ToWire(*rendered)
	if err != nil {
		return h.fail(np, "MarshalFailed",
			fmt.Sprintf("wire marshal failed: %v", err))
	}

	// Deliver iff not yet delivered, else just refresh status.
	if needsDeliver(np) {
		if err := adp.WriteNodePlan(context.Background(), np, wirePlan); err != nil {
			return h.fail(np, "DeliveryFailed",
				fmt.Sprintf("adapter could not deliver plan: %v", err))
		}
		out := np.DeepCopy()
		out.Status.Phase = v1alpha1.NodePlanPhaseRunning
		return h.deps.NodePlans.UpdateStatus(out)
	}

	// Read back the agent's status.
	status, err := adp.ReadNodePlanStatus(context.Background(), np)
	if err != nil {
		return np, fmt.Errorf("nodeplan: read status: %w", err)
	}
	if statusUnchanged(np.Status, status) {
		return np, nil
	}
	out := np.DeepCopy()
	out.Status.Phase = status.Phase
	out.Status.Conditions = mergeConditions(out.Status.Conditions, status.Conditions)
	out.Status.Outputs = status.Outputs
	return h.deps.NodePlans.UpdateStatus(out)
}

// parent fetches the parent ClusterPlan named by the
// ClusterPlanNameLabel. Returns errOrphan for standalone NodePlans
// (no ClusterPlanNameLabel) — the caller treats this as a no-op since
// standalone delivery is out of scope for PR1.
func (h *Handler) parent(np *v1alpha1.NodePlan) (*v1alpha1.ClusterPlan, error) {
	parentName := np.Labels[v1alpha1.ClusterPlanNameLabel]
	if parentName == "" {
		return nil, errOrphan
	}
	return h.deps.ClusterPlans.Get(np.Namespace, parentName)
}

// errOrphan is the sentinel returned when a NodePlan lacks a parent
// ClusterPlan label. PR1 treats orphan NodePlans as no-ops (the
// standalone-authoring path is deferred).
var errOrphan = fmt.Errorf("nodeplan: no parent ClusterPlan label set")

// needsDeliver returns true when the plan has not yet been pushed to
// the adapter (Phase empty/Pending) — the deliver step is one-shot per
// NodePlan in PR1.
func needsDeliver(np *v1alpha1.NodePlan) bool {
	switch np.Status.Phase {
	case "", v1alpha1.NodePlanPhasePending:
		return true
	}
	return false
}

// statusUnchanged returns true when nothing material in the candidate
// new status differs from the current — guards against churn-only
// status updates that would re-enqueue this controller in a loop.
func statusUnchanged(current, candidate v1alpha1.NodePlanStatus) bool {
	if current.Phase != candidate.Phase {
		return false
	}
	if len(current.Outputs) != len(candidate.Outputs) {
		return false
	}
	for k, v := range candidate.Outputs {
		if cv, ok := current.Outputs[k]; !ok || cv.Value != v.Value {
			return false
		}
	}
	return true
}

// mergeConditions overlays src onto dst by Type. dst is preserved for
// any Type src doesn't carry.
func mergeConditions(dst, src []metav1.Condition) []metav1.Condition {
	for _, c := range src {
		set := false
		for i := range dst {
			if dst[i].Type == c.Type {
				dst[i] = c
				set = true
				break
			}
		}
		if !set {
			dst = append(dst, c)
		}
	}
	return dst
}

// fail records a terminal failure on the NodePlan.
func (h *Handler) fail(np *v1alpha1.NodePlan, reason, message string) (*v1alpha1.NodePlan, error) {
	out := np.DeepCopy()
	out.Status.Phase = v1alpha1.NodePlanPhaseFailed
	now := metav1.NewTime(h.deps.Now())
	out.Status.Conditions = mergeConditions(out.Status.Conditions, []metav1.Condition{{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}})
	return h.deps.NodePlans.UpdateStatus(out)
}
