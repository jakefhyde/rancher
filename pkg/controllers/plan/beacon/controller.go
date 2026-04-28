package beacon

import (
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BeaconClient is the surface this controller mutates.
type BeaconClient interface {
	Update(*v1alpha1.Beacon) (*v1alpha1.Beacon, error)
	UpdateStatus(*v1alpha1.Beacon) (*v1alpha1.Beacon, error)
}

// ClusterPlanCache reads holder + requester ClusterPlans by namespaced name.
type ClusterPlanCache interface {
	Get(namespace, name string) (*v1alpha1.ClusterPlan, error)
}

// ClusterPlanClient applies status mutations onto rejected / preempted plans.
type ClusterPlanClient interface {
	UpdateStatus(*v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error)
}

// Deps bundles the dependencies the controller operates on.
type Deps struct {
	Beacons      BeaconClient
	ClusterPlans ClusterPlanCache
	PlansClient  ClusterPlanClient
	Now          func() time.Time
}

// Handler reconciles Beacons by translating their Spec/Status (plus the
// holder ClusterPlan's phase) into the next state via Reconcile, then
// persisting the changes.
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

// OnChange handles a Beacon event from wrangler. The function does not
// take a key parameter beyond what wrangler passes (typically
// "<namespace>/<name>") and always returns the (possibly-updated) Beacon
// for wrangler's status diffing.
func (h *Handler) OnChange(_ string, beacon *v1alpha1.Beacon) (*v1alpha1.Beacon, error) {
	if beacon == nil || beacon.DeletionTimestamp != nil {
		return beacon, nil
	}
	holderPhase := h.holderPhase(beacon)

	out := Reconcile(StateInput{
		Beacon:      beacon,
		HolderPhase: holderPhase,
		Now:         h.deps.Now(),
	})

	// Apply rejection / pre-empt notification on the requester /
	// holder before mutating the Beacon — that way an Update conflict
	// on the Beacon doesn't leave us in a state where the rejection
	// was lost. The status writes are individually best-effort:
	// failures are logged via wrapped errors but do not block the
	// Beacon update.
	if out.Reject != nil {
		if err := h.applyConditionToPlan(beacon.Namespace, out.Reject, metav1.ConditionFalse, v1alpha1.ConditionBeaconHeld); err != nil {
			return beacon, fmt.Errorf("beacon: surface rejection on %q: %w", out.Reject.PlanName, err)
		}
	}
	if out.Preempt != nil {
		// Pre-empt is informational; the stage controller observes
		// state == Cancelling on its own. Stamping a condition on the
		// holder makes it visible in `kubectl describe`.
		if err := h.applyConditionToPlan(beacon.Namespace, out.Preempt, metav1.ConditionUnknown, v1alpha1.ConditionCancelled); err != nil {
			return beacon, fmt.Errorf("beacon: surface pre-empt on %q: %w", out.Preempt.PlanName, err)
		}
	}

	if out.UpdatedBeacon != nil {
		// Spec.Acquisition is part of Spec — needs Update().
		// Status changes need UpdateStatus(). When the state machine
		// touches both (which it does on accept/preempt-decline), we
		// do Update first then UpdateStatus. Wrangler's typed clients
		// do this idiomatically; production callers may swap in a
		// patch-based path later.
		updated, err := h.deps.Beacons.Update(out.UpdatedBeacon)
		if err != nil {
			return beacon, fmt.Errorf("beacon: update spec: %w", err)
		}
		// Carry over status onto the just-updated object.
		updated.Status = out.UpdatedBeacon.Status
		updated, err = h.deps.Beacons.UpdateStatus(updated)
		if err != nil {
			return beacon, fmt.Errorf("beacon: update status: %w", err)
		}
		return updated, nil
	}
	return beacon, nil
}

// holderPhase looks up the current holder's Status.Phase, returning ""
// when there is no holder or the holder cannot be fetched.
func (h *Handler) holderPhase(beacon *v1alpha1.Beacon) string {
	if beacon.Status.Holder == nil {
		return ""
	}
	plan, err := h.deps.ClusterPlans.Get(beacon.Namespace, beacon.Status.Holder.Name.Name)
	if apierrors.IsNotFound(err) {
		// Holder was deleted out from under us — treat as Failed so
		// the lock releases.
		return v1alpha1.ClusterPlanPhaseFailed
	}
	if err != nil {
		return ""
	}
	return plan.Status.Phase
}

// applyConditionToPlan stamps a Reject/Preempt action onto the named
// ClusterPlan via UpdateStatus. The condition Type/Status varies by
// caller (rejection ⇒ BeaconHeld=False, pre-empt ⇒ Cancelled=Unknown).
func (h *Handler) applyConditionToPlan(namespace string, action *PlanAction, status metav1.ConditionStatus, condType string) error {
	plan, err := h.deps.ClusterPlans.Get(namespace, action.PlanName)
	if apierrors.IsNotFound(err) {
		// Nothing to do; plan may have been deleted between request
		// and reconciliation.
		return nil
	}
	if err != nil {
		return err
	}
	out := plan.DeepCopy()
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             action.Reason,
		Message:            action.Message,
		LastTransitionTime: metav1.NewTime(h.deps.Now()),
	})
	// Rejection is also a terminal phase for the plan — set Phase=Failed
	// so the requester can observe it via the simpler Phase field.
	if condType == v1alpha1.ConditionBeaconHeld && status == metav1.ConditionFalse && action.Reason == "Rejected" {
		out.Status.Phase = v1alpha1.ClusterPlanPhaseFailed
	}
	_, err = h.deps.PlansClient.UpdateStatus(out)
	return err
}
