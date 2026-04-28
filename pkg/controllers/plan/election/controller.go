// Package election implements the per-pool node election that the
// stage controller delegates to. For each rendered ClusterPlan with a
// NodePoolPlan that carries an Election block, this controller:
//
//   1. Lists candidate nodes via adapter.ListNodes(Selector).
//   2. For each candidate, evaluates every Election.Criteria expression
//      via render.Engine.RenderCriterion. The first candidate that
//      passes all criteria is the winner.
//   3. Applies Election.TargetLabel="true" to the winner via
//      adapter.ApplyElectionLabel; removes the label from any
//      previously-elected candidate that no longer qualifies (drift).
//
// The stage controller observes the elected node by treating the
// Election as a single-label selector matching TargetLabel — once the
// label exists, ListNodes returns the elected node and the stage
// proceeds to materialise NodePlans.
//
// PR1 ships a deliberately minimal election: first qualifying
// candidate wins; no priority/weighting; re-election only happens
// when the elected node is deleted or its labels drift such that
// criteria evaluate false. Additional policy (deterministic ordering
// by name/UID, priority labels, exclusion sets) belongs in PR2-PR5.
package election

import (
	"context"
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Deps bundles the dependencies the controller operates on.
type Deps struct {
	Adapters adapter.Registry
	Engine   *render.Engine
	Now      func() time.Time
}

// Handler is the election reconciler.
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

// OnChange walks every NodePoolPlan with an Election in the plan and
// ensures the Election.TargetLabel is applied to exactly one candidate.
// Idempotent: re-runs are no-ops once a valid winner exists.
func (h *Handler) OnChange(_ string, plan *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	if plan == nil || plan.DeletionTimestamp != nil {
		return plan, nil
	}
	clusterType := adapter.DetermineClusterType(plan.Spec.ClusterRef)
	if clusterType == "" {
		return plan, nil
	}
	adp, ok := h.deps.Adapters.Get(clusterType)
	if !ok {
		return plan, nil
	}

	for _, pool := range plan.Spec.Plan {
		if pool.Election == nil || pool.Election.TargetLabel == "" {
			continue
		}
		if err := h.electForPool(adp, plan, pool, clusterType); err != nil {
			return plan, fmt.Errorf("election: pool %q: %w", pool.Name, err)
		}
	}
	return plan, nil
}

// electForPool finds (or refreshes) the elected node for one pool.
func (h *Handler) electForPool(adp adapter.Adapter, plan *v1alpha1.ClusterPlan, pool v1alpha1.NodePoolPlan, clusterType string) error {
	candidates, err := adp.ListNodes(context.Background(), plan.Spec.ClusterRef, selectorOrEverything(&pool.Election.Selector))
	if err != nil {
		return err
	}

	// Drift check: if the currently-elected node no longer qualifies,
	// remove the label so the next reconcile picks a fresh winner.
	for _, c := range candidates {
		if c.Labels[pool.Election.TargetLabel] == "true" {
			if h.qualifies(plan, clusterType, c, pool.Election.Criteria) {
				return nil // current winner still good
			}
			if err := adp.RemoveElectionLabel(context.Background(), c, pool.Election.TargetLabel); err != nil {
				return fmt.Errorf("remove stale label: %w", err)
			}
		}
	}

	// Pick the first candidate that satisfies all criteria.
	for _, c := range candidates {
		if !h.qualifies(plan, clusterType, c, pool.Election.Criteria) {
			continue
		}
		return adp.ApplyElectionLabel(context.Background(), c, pool.Election.TargetLabel, "true")
	}
	// No qualifying candidate — leave the pool unrelected; the stage
	// controller's selector-based ListNodes will return zero matches
	// and the stage parks until election resolves.
	return nil
}

// qualifies returns true iff every criterion renders to "true" for the
// candidate.
func (h *Handler) qualifies(plan *v1alpha1.ClusterPlan, clusterType string, candidate adapter.NodeRecord, criteria []string) bool {
	if len(criteria) == 0 {
		return true
	}
	ctx := render.ElectionContext{
		Cluster: render.ClusterReference{
			Type:      clusterType,
			Namespace: plan.Spec.ClusterRef.Namespace,
			Name:      plan.Spec.ClusterRef.Name,
		},
		ClusterType: clusterType,
		Node:        candidate.Object,
		NodeLabels:  candidate.Labels,
	}
	for _, expr := range criteria {
		ok, err := h.deps.Engine.RenderCriterion(expr, ctx)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

// selectorOrEverything returns labels.Everything() for the empty
// selector and otherwise converts the LabelSelector to a runtime
// selector. Conversion errors fall back to Everything (over-selection
// is safer than under-selection here — qualifies() will then reject
// any unfit candidates).
func selectorOrEverything(s *metav1.LabelSelector) labels.Selector {
	if s == nil || (len(s.MatchLabels)+len(s.MatchExpressions) == 0) {
		return labels.Everything()
	}
	sel, err := metav1.LabelSelectorAsSelector(s)
	if err != nil {
		return labels.Everything()
	}
	return sel
}
