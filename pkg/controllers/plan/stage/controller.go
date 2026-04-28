// Package stage drives a rendered ClusterPlan through its
// NodePoolPlan list, one pool at a time. For each pool the controller:
//
//   1. Selects target nodes via the pool's Selector (Election is
//      delegated to pkg/controllers/plan/election; this controller
//      observes the elected node via the target label).
//   2. Materialises one NodePlan per selected node, embedding the
//      pool's inlined NodePlanSpec and labelling the NodePlan with
//      v1alpha1.NodeNameLabel so the downstream nodeplan controller
//      and adapter know where to deliver it.
//   3. Waits for every materialised NodePlan to reach a terminal
//      phase (Succeeded/Failed/Cancelled) before advancing the pool
//      pointer (Status.CurrentStep).
//   4. On final-pool success, marks Phase=Succeeded so the beacon
//      controller releases the lock.
//
// Patches (before/after) and stage-output capture into
// ClusterPlan.Status.PersistentOutputs are deliberately scoped out of
// PR1 — the etcd-snapshot-create operation we're shipping has no
// patches and its outputs are NodePlan-local. Stubbed entry points are
// in place so PR2-PR5 operations can fill them in without restructuring.
package stage

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ClusterPlanClient mutates ClusterPlan status (currentStep, phase).
type ClusterPlanClient interface {
	UpdateStatus(*v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error)
}

// NodePlanCache reads NodePlans for status aggregation.
type NodePlanCache interface {
	List(namespace string, sel labels.Selector) ([]*v1alpha1.NodePlan, error)
}

// NodePlanClient creates per-node NodePlans.
type NodePlanClient interface {
	Create(*v1alpha1.NodePlan) (*v1alpha1.NodePlan, error)
}

// Deps bundles the dependencies the controller operates on.
type Deps struct {
	ClusterPlans  ClusterPlanClient
	NodePlans     NodePlanCache
	NodePlansClient NodePlanClient
	Adapters      adapter.Registry
	Now           func() time.Time
}

// Handler is the stage reconciler.
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

// OnChange drives the next available step on a rendered ClusterPlan.
// Idempotent: repeated invocations on the same step look at the
// existing NodePlans and either advance the step (when all done) or
// no-op (when still in progress).
func (h *Handler) OnChange(_ string, plan *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	if plan == nil || plan.DeletionTimestamp != nil {
		return plan, nil
	}
	if !isRendered(plan) {
		return plan, nil
	}
	if isTerminal(plan.Status.Phase) {
		return plan, nil
	}
	if plan.Status.CurrentStep >= len(plan.Spec.Plan) {
		return h.markSucceeded(plan)
	}

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

	pool := plan.Spec.Plan[plan.Status.CurrentStep]
	stepName := poolName(pool, plan.Status.CurrentStep)

	// Select target nodes. Election handling is delegated to the
	// election controller, which sets Election.TargetLabel on the
	// chosen node — this controller then treats Election as a single-
	// label selector matching that target label.
	sel, err := poolSelector(pool)
	if err != nil {
		return h.fail(plan, "SelectorInvalid",
			fmt.Sprintf("pool %q has invalid selector: %v", stepName, err))
	}
	nodes, err := adp.ListNodes(context.Background(), plan.Spec.ClusterRef, sel)
	if err != nil {
		return plan, fmt.Errorf("stage: list nodes for pool %q: %w", stepName, err)
	}
	if len(nodes) == 0 {
		// Empty pool — skip and advance. (Election pools that haven't
		// yet been resolved fall here too; the election controller
		// will requeue when it applies the target label.)
		return h.advance(plan, stepName, "no nodes match selector")
	}

	// Materialise NodePlan per matched node (idempotent: AlreadyExists
	// is fine).
	for _, node := range nodes {
		if err := h.ensureNodePlan(plan, &pool, stepName, node); err != nil {
			return plan, fmt.Errorf("stage: ensure NodePlan for %q: %w", node.Identifier, err)
		}
	}

	// Aggregate NodePlan phases for this step.
	stepNodePlans, err := h.deps.NodePlans.List(plan.Namespace, labels.SelectorFromSet(labels.Set{
		v1alpha1.ClusterPlanNameLabel:    plan.Name,
		v1alpha1.ClusterPlanPoolIndexLabel: stepIndexValue(plan.Status.CurrentStep),
	}))
	if err != nil {
		return plan, fmt.Errorf("stage: list NodePlans for pool %q: %w", stepName, err)
	}

	switch aggregatePhase(stepNodePlans, len(nodes)) {
	case stepInProgress:
		return plan, nil
	case stepFailed:
		return h.fail(plan, "NodePlanFailed",
			fmt.Sprintf("at least one NodePlan in pool %q failed", stepName))
	case stepSucceeded:
		return h.advance(plan, stepName, "")
	}
	return plan, nil
}

// ensureNodePlan creates a NodePlan for the supplied node iff one does
// not already exist for (cluster-plan, pool-index, node).
func (h *Handler) ensureNodePlan(plan *v1alpha1.ClusterPlan, pool *v1alpha1.NodePoolPlan, stepName string, node adapter.NodeRecord) error {
	npName := nodePlanName(plan.Name, plan.Status.CurrentStep, node.Identifier)
	np := &v1alpha1.NodePlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: plan.Namespace,
			Labels: map[string]string{
				v1alpha1.ClusterPlanNameLabel:    plan.Name,
				v1alpha1.ClusterPlanPoolIndexLabel: stepIndexValue(plan.Status.CurrentStep),
				v1alpha1.NodeNameLabel:           node.Identifier,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
				Kind:       "ClusterPlan",
				Name:       plan.Name,
				UID:        plan.UID,
				Controller: ptrBool(true),
			}},
		},
		Spec: pool.NodePlanSpec, // inlined per task #10's design
	}
	_, err := h.deps.NodePlansClient.Create(np)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	_ = stepName // reserved for future status surfacing
	return nil
}

// advance increments CurrentStep and persists. When the new step
// equals len(Spec.Plan) the plan transitions to Succeeded.
func (h *Handler) advance(plan *v1alpha1.ClusterPlan, stepName, _ string) (*v1alpha1.ClusterPlan, error) {
	out := plan.DeepCopy()
	out.Status.CurrentStep = plan.Status.CurrentStep + 1
	if out.Status.CurrentStep >= len(plan.Spec.Plan) {
		return h.markSucceeded(out)
	}
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionInstructionsApplied,
		Status:             metav1.ConditionTrue,
		Reason:             "PoolSucceeded",
		Message:            fmt.Sprintf("pool %q completed", stepName),
		LastTransitionTime: metav1.NewTime(h.deps.Now()),
	})
	return h.deps.ClusterPlans.UpdateStatus(out)
}

// markSucceeded transitions Phase=Succeeded so the beacon controller
// releases the lock on its next reconcile.
func (h *Handler) markSucceeded(plan *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	out := plan.DeepCopy()
	out.Status.Phase = v1alpha1.ClusterPlanPhaseSucceeded
	now := metav1.NewTime(h.deps.Now())
	out.Status.CompletedAt = &now
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Completed",
		Message:            "all pools completed successfully",
		LastTransitionTime: now,
	})
	return h.deps.ClusterPlans.UpdateStatus(out)
}

// fail transitions Phase=Failed.
func (h *Handler) fail(plan *v1alpha1.ClusterPlan, reason, message string) (*v1alpha1.ClusterPlan, error) {
	out := plan.DeepCopy()
	out.Status.Phase = v1alpha1.ClusterPlanPhaseFailed
	now := metav1.NewTime(h.deps.Now())
	out.Status.CompletedAt = &now
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
	return h.deps.ClusterPlans.UpdateStatus(out)
}

// --- helpers ----------------------------------------------------------------

// poolSelector returns the LabelSelector to feed adapter.ListNodes for
// the given pool. For Selector-based pools this is just the pool's
// selector. For Election-based pools this is the elected target label
// — which is empty until the election controller has chosen a node, in
// which case ListNodes returns no matches and the stage parks.
func poolSelector(pool v1alpha1.NodePoolPlan) (labels.Selector, error) {
	if pool.Election != nil {
		return labels.SelectorFromSet(labels.Set{
			pool.Election.TargetLabel: "true",
		}), nil
	}
	if pool.Selector == nil {
		return labels.Everything(), nil
	}
	return metav1.LabelSelectorAsSelector(pool.Selector)
}

func poolName(pool v1alpha1.NodePoolPlan, idx int) string {
	if pool.Name != "" {
		return pool.Name
	}
	return fmt.Sprintf("pool-%d", idx)
}

func stepIndexValue(idx int) string { return fmt.Sprintf("%d", idx) }

func nodePlanName(planName string, stepIdx int, nodeID string) string {
	return fmt.Sprintf("%s-%d-%s", planName, stepIdx, nodeID)
}

type stepStatus int

const (
	stepInProgress stepStatus = iota
	stepSucceeded
	stepFailed
)

func aggregatePhase(plans []*v1alpha1.NodePlan, expected int) stepStatus {
	if len(plans) < expected {
		return stepInProgress
	}
	allSucceeded := true
	for _, p := range plans {
		switch p.Status.Phase {
		case v1alpha1.NodePlanPhaseFailed:
			return stepFailed
		case v1alpha1.NodePlanPhaseSucceeded:
			// keep checking
		default:
			allSucceeded = false
		}
	}
	if allSucceeded {
		return stepSucceeded
	}
	return stepInProgress
}

func isRendered(plan *v1alpha1.ClusterPlan) bool {
	for _, c := range plan.Status.Conditions {
		if c.Type == v1alpha1.ConditionRendered && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func isTerminal(phase string) bool {
	switch phase {
	case v1alpha1.ClusterPlanPhaseSucceeded,
		v1alpha1.ClusterPlanPhaseFailed,
		v1alpha1.ClusterPlanPhaseCancelled:
		return true
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

func ptrBool(b bool) *bool { return &b }
