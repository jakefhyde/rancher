package wire

import (
	"fmt"
	"strconv"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatusInputs bundles the data FromWireStatus needs that doesn't live
// inside secret.Data itself: the expected (currently-delivered) plan
// checksum, and the framework-side Output declarations to project against.
type StatusInputs struct {
	// ExpectedChecksum is the sha-256 of the bytes currently in the
	// "plan" key (i.e. what the framework most recently asked the
	// agent to apply). Compare against secret.Data["applied-checksum"]
	// to decide success vs in-progress.
	ExpectedChecksum string

	// MaxFailures is the failure threshold the planner sets on the
	// secret. When secret.Data["failure-count"] meets or exceeds this,
	// the NodePlan transitions to Failed. -1 disables the check.
	MaxFailures int

	// Outputs is the spec.Outputs declaration list — used to project
	// per-instruction stdout into Status.Outputs.
	Outputs []v1alpha1.Output

	// Now overrides the wall-clock when populating LastApplied.
	// Optional; defaults to time.Now.
	Now func() time.Time
}

// FromWireStatus translates the system-agent's reply (carried in the
// plan secret's status keys) into a NodePlanStatus. The result is
// suitable for direct assignment to NodePlan.Status by the adapter.
//
// Phase derivation:
//
//   - applied-checksum == expectedChecksum && failure-count == 0 → Succeeded
//   - failure-count > 0 && (MaxFailures < 0 || failure-count < MaxFailures) → Running
//   - failure-count >= MaxFailures (and MaxFailures > 0)                      → Failed
//   - applied-checksum empty                                                  → Pending
//   - otherwise (checksum mismatch, no failures)                              → Running
//
// Conditions are populated for at-a-glance status: Ready (true when
// Succeeded), and a transient condition naming the failure count when
// non-zero. The function never errors on missing keys — partial agent
// state translates into Phase=Pending.
func FromWireStatus(secretData map[string][]byte, in StatusInputs) (v1alpha1.NodePlanStatus, error) {
	if in.Now == nil {
		in.Now = time.Now
	}
	status := v1alpha1.NodePlanStatus{}

	appliedChecksum := string(secretData[PlanSecretKeyAppliedChecksum])
	failureCount, _ := strconv.Atoi(string(secretData[PlanSecretKeyFailureCount]))

	switch {
	case appliedChecksum == "":
		status.Phase = v1alpha1.NodePlanPhasePending
	case in.MaxFailures > 0 && failureCount >= in.MaxFailures:
		status.Phase = v1alpha1.NodePlanPhaseFailed
	case failureCount > 0:
		status.Phase = v1alpha1.NodePlanPhaseRunning
	case appliedChecksum == in.ExpectedChecksum:
		status.Phase = v1alpha1.NodePlanPhaseSucceeded
		now := metav1.NewTime(in.Now())
		status.Conditions = append(status.Conditions, metav1.Condition{
			Type:               v1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Applied",
			Message:            fmt.Sprintf("plan applied (checksum %s)", appliedChecksum),
			LastTransitionTime: now,
		})
	default:
		// Checksum mismatch with no failure: agent is still applying.
		status.Phase = v1alpha1.NodePlanPhaseRunning
	}

	if failureCount > 0 {
		status.Conditions = append(status.Conditions, metav1.Condition{
			Type:               v1alpha1.ConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ApplyFailed",
			Message:            fmt.Sprintf("agent failure count %d", failureCount),
			LastTransitionTime: metav1.NewTime(in.Now()),
		})
	}

	if outs, err := ExtractOutputs(secretData, in.Outputs); err != nil {
		return status, err
	} else if len(outs) > 0 {
		status.Outputs = outs
	}

	return status, nil
}
