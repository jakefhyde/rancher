// Package beacon implements the per-cluster lock that serialises day-2
// operations. The lock state machine is a small, pure function in
// statemachine.go; the wrangler-facing reconciler in controller.go
// translates Beacon and ClusterPlan events into StateInputs and applies
// the StateOutput.
//
// State machine transitions:
//
//	Free      --acquisition accepted-->                Acquired
//	Acquired  --holder reaches a terminal phase-->     Free
//	Acquired  --acquisition: cancelsOthers && holder.cancellable--> Cancelling
//	Acquired  --acquisition rejected (holder non-cancellable)-->    Acquired
//	                                       (caller surfaces rejection on the requester)
//	Cancelling --holder reaches Cancelled-->           Free
//	                                       (re-runs acquisition automatically)
//	Cancelling --TimeoutSeconds elapsed-->             Free (force, log warning)
//
// The state machine is deliberately ignorant of how the holder
// ClusterPlan unwinds itself when pre-empted — that's the stage
// controller's job. The beacon controller observes ClusterPlan.Status
// transitions and reacts.
package beacon

import (
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StateInput is everything the state machine needs to make a decision.
type StateInput struct {
	// Beacon is the current Beacon — its Spec.Acquisition mailbox and
	// Status carry the inputs.
	Beacon *v1alpha1.Beacon

	// HolderPhase is the holder ClusterPlan's Status.Phase, when there
	// is one. Empty when no holder exists or the holder isn't
	// resolvable. The reconciler is responsible for fetching it.
	HolderPhase string

	// Now is the wall-clock seen by this reconciliation. Surfaces
	// timeout decisions and stamps holder.AcquiredAt / DeadlineAt.
	Now time.Time
}

// StateOutput is what the state machine wants the reconciler to do.
// All actions are described as data — the reconciler decides how to
// persist them (Update / UpdateStatus / event emission).
type StateOutput struct {
	// UpdatedBeacon, when non-nil, is the Beacon the reconciler should
	// persist. The reconciler MUST update Status (the state-machine
	// only ever changes Status fields and clears Spec.Acquisition).
	UpdatedBeacon *v1alpha1.Beacon

	// Reject describes a rejected acquisition. The reconciler is
	// expected to surface the rejection on the requesting ClusterPlan
	// via a status condition + event.
	Reject *PlanAction

	// Preempt describes an in-progress cancellation. Informational —
	// the reconciler may emit an event but does not need to mutate the
	// holder ClusterPlan; the stage controller drives unwinding by
	// observing Beacon.Status.State == Cancelling.
	Preempt *PlanAction
}

// PlanAction names a ClusterPlan involved in a state transition along
// with a Reason/Message pair the reconciler can lift onto its status.
type PlanAction struct {
	PlanName string
	Reason   string
	Message  string
}

// Reconcile is the pure state-machine. Given the current Beacon, the
// holder's phase, and now, it returns the changes the reconciler should
// apply.
func Reconcile(in StateInput) StateOutput {
	out := StateOutput{}
	if in.Beacon == nil {
		return out
	}
	beacon := in.Beacon.DeepCopy()
	state := beacon.Status.State
	if state == "" {
		state = v1alpha1.BeaconStateFree
	}

	// 1. If a holder exists and has reached a terminal phase, release
	//    the lock first — that may free the way for a pending
	//    Spec.Acquisition.
	if holderTerminal(state, in.HolderPhase) {
		state = v1alpha1.BeaconStateFree
		beacon.Status.State = state
		beacon.Status.Active = false
		beacon.Status.ActiveSelectors = nil
		beacon.Status.Holder = nil
		beacon.Status.Cancellation = nil
		out.UpdatedBeacon = beacon
	}

	// 2. If the held op blew its deadline, force-clear.
	if state == v1alpha1.BeaconStateAcquired && beacon.Status.Holder != nil &&
		!beacon.Status.Holder.DeadlineAt.IsZero() &&
		in.Now.After(beacon.Status.Holder.DeadlineAt.Time) {
		holder := beacon.Status.Holder
		state = v1alpha1.BeaconStateFree
		beacon.Status.State = state
		beacon.Status.Active = false
		beacon.Status.ActiveSelectors = nil
		beacon.Status.Holder = nil
		beacon.Status.Cancellation = nil
		out.UpdatedBeacon = beacon
		out.Reject = &PlanAction{
			PlanName: holder.Name.Name,
			Reason:   "BeaconTimeout",
			Message:  fmt.Sprintf("operation %q exceeded its lifecycle timeout", holder.Operation),
		}
	}

	// 3. Process pending acquisition request.
	if beacon.Spec.Acquisition != nil {
		req := beacon.Spec.Acquisition
		switch state {
		case v1alpha1.BeaconStateFree:
			// Accept.
			holder := newHolder(req, in.Now)
			beacon.Status.State = v1alpha1.BeaconStateAcquired
			beacon.Status.Active = true
			beacon.Status.Holder = holder
			beacon.Status.Cancellation = nil
			beacon.Spec.Acquisition = nil
			setCondition(&beacon.Status.Conditions, metav1.Condition{
				Type:               v1alpha1.ConditionBeaconHeld,
				Status:             metav1.ConditionTrue,
				Reason:             "Acquired",
				Message:            fmt.Sprintf("plan %q acquired the beacon", req.ClusterPlan.Name),
				LastTransitionTime: metav1.NewTime(in.Now),
			})
			out.UpdatedBeacon = beacon

		case v1alpha1.BeaconStateAcquired:
			holder := beacon.Status.Holder
			if req.Lifecycle.CancelsOthers && holder != nil && holder.Cancellable {
				// Pre-empt.
				beacon.Status.State = v1alpha1.BeaconStateCancelling
				beacon.Status.Active = true
				beacon.Status.Cancellation = &v1alpha1.BeaconCancellation{
					PreemptingOperation: req.Operation,
					PreemptingPlan:      req.ClusterPlan,
					StartedAt:           metav1.NewTime(in.Now),
				}
				setCondition(&beacon.Status.Conditions, metav1.Condition{
					Type:               v1alpha1.ConditionBeaconHeld,
					Status:             metav1.ConditionTrue,
					Reason:             "Cancelling",
					Message:            fmt.Sprintf("preempting holder %q for %q", holder.Name.Name, req.ClusterPlan.Name),
					LastTransitionTime: metav1.NewTime(in.Now),
				})
				// Do NOT clear Spec.Acquisition — the reconciler
				// re-enters when the holder reaches Cancelled phase
				// and accepts the pending request from Free.
				out.UpdatedBeacon = beacon
				out.Preempt = &PlanAction{
					PlanName: holder.Name.Name,
					Reason:   "Preempted",
					Message:  fmt.Sprintf("preempted by %q", req.ClusterPlan.Name),
				}
			} else {
				// Reject.
				holderName := ""
				holderOp := ""
				if holder != nil {
					holderName = holder.Name.Name
					holderOp = holder.Operation
				}
				beacon.Spec.Acquisition = nil
				out.UpdatedBeacon = beacon
				out.Reject = &PlanAction{
					PlanName: req.ClusterPlan.Name,
					Reason:   "Rejected",
					Message: fmt.Sprintf(
						"beacon held by %q (operation %q); pre-emption requires holder.cancellable=true and requester.cancelsOthers=true",
						holderName, holderOp),
				}
			}

		case v1alpha1.BeaconStateCancelling:
			// Already pre-empting; new requesters wait. We don't
			// touch Spec.Acquisition unless the in-flight cancellation
			// belongs to a *different* plan than the request.
			cancelling := beacon.Status.Cancellation
			if cancelling != nil && cancelling.PreemptingPlan.Name != req.ClusterPlan.Name {
				beacon.Spec.Acquisition = nil
				out.UpdatedBeacon = beacon
				out.Reject = &PlanAction{
					PlanName: req.ClusterPlan.Name,
					Reason:   "Rejected",
					Message: fmt.Sprintf(
						"beacon is currently pre-empting holder for %q; retry once that operation completes",
						cancelling.PreemptingPlan.Name),
				}
			}
		}
	}

	return out
}

// newHolder builds a BeaconHolder record from an acquisition request.
func newHolder(req *v1alpha1.BeaconAcquisitionRequest, now time.Time) *v1alpha1.BeaconHolder {
	deadline := now.Add(time.Duration(req.Lifecycle.TimeoutSeconds) * time.Second)
	return &v1alpha1.BeaconHolder{
		Operation:   req.Operation,
		Name:        req.ClusterPlan,
		UID:         req.UID,
		Cancellable: req.Lifecycle.Cancellable,
		AcquiredAt:  metav1.NewTime(now),
		DeadlineAt:  metav1.NewTime(deadline),
	}
}

// holderTerminal returns true when the holder ClusterPlan has reached
// a phase that means the lock should be released. In Cancelling state
// we wait specifically for Cancelled; in Acquired state we accept any
// terminal phase.
func holderTerminal(state v1alpha1.BeaconState, phase string) bool {
	switch state {
	case v1alpha1.BeaconStateAcquired:
		return phase == v1alpha1.ClusterPlanPhaseSucceeded ||
			phase == v1alpha1.ClusterPlanPhaseFailed ||
			phase == v1alpha1.ClusterPlanPhaseCancelled
	case v1alpha1.BeaconStateCancelling:
		return phase == v1alpha1.ClusterPlanPhaseCancelled ||
			phase == v1alpha1.ClusterPlanPhaseFailed
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
