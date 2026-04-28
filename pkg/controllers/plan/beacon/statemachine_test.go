package beacon

import (
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func now() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }

func freeBeacon() *v1alpha1.Beacon {
	return &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Namespace: "fleet-default", Name: "my-cluster"},
	}
}

func acquisition(planName, op string, cancellable, cancelsOthers bool, timeoutSec int32) *v1alpha1.BeaconAcquisitionRequest {
	return &v1alpha1.BeaconAcquisitionRequest{
		Operation:   op,
		ClusterPlan: v1alpha1.LocalObjectReference{Name: planName},
		UID:         types.UID("uid-" + planName),
		Lifecycle: v1alpha1.OperationLifecycle{
			Cancellable:    cancellable,
			CancelsOthers:  cancelsOthers,
			TimeoutSeconds: timeoutSec,
		},
		RequestedAt: metav1.NewTime(now()),
	}
}

func TestFreeAcceptsAcquisition(t *testing.T) {
	b := freeBeacon()
	b.Spec.Acquisition = acquisition("plan-a", "etcd-snapshot-create", false, false, 1800)
	out := Reconcile(StateInput{Beacon: b, Now: now()})
	if out.UpdatedBeacon == nil {
		t.Fatalf("expected UpdatedBeacon")
	}
	upd := out.UpdatedBeacon
	if upd.Status.State != v1alpha1.BeaconStateAcquired {
		t.Errorf("State = %q, want Acquired", upd.Status.State)
	}
	if !upd.Status.Active {
		t.Errorf("Active should be true")
	}
	if upd.Status.Holder == nil || upd.Status.Holder.Name.Name != "plan-a" {
		t.Errorf("Holder = %+v", upd.Status.Holder)
	}
	if upd.Spec.Acquisition != nil {
		t.Errorf("Acquisition should be cleared after accept")
	}
	wantDeadline := now().Add(1800 * time.Second)
	if got := upd.Status.Holder.DeadlineAt.Time; !got.Equal(wantDeadline) {
		t.Errorf("DeadlineAt = %v, want %v", got, wantDeadline)
	}
}

func TestAcquiredRejectsNonCancellingRequest(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateAcquired
	b.Status.Active = true
	b.Status.Holder = &v1alpha1.BeaconHolder{
		Operation:   "cert-rotation",
		Name:        v1alpha1.LocalObjectReference{Name: "holder-a"},
		Cancellable: true,
		AcquiredAt:  metav1.NewTime(now()),
		DeadlineAt:  metav1.NewTime(now().Add(time.Hour)),
	}
	b.Spec.Acquisition = acquisition("requester-b", "etcd-snapshot-create", false, false, 600)

	out := Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseRunning, Now: now()})
	if out.UpdatedBeacon == nil {
		t.Fatalf("expected UpdatedBeacon")
	}
	if out.UpdatedBeacon.Status.State != v1alpha1.BeaconStateAcquired {
		t.Errorf("State should remain Acquired, got %q", out.UpdatedBeacon.Status.State)
	}
	if out.UpdatedBeacon.Spec.Acquisition != nil {
		t.Errorf("Acquisition should be cleared after rejection")
	}
	if out.Reject == nil || out.Reject.PlanName != "requester-b" {
		t.Errorf("Reject = %+v, want requester-b", out.Reject)
	}
}

func TestAcquiredCancellableYieldsToCancelsOthers(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateAcquired
	b.Status.Active = true
	b.Status.Holder = &v1alpha1.BeaconHolder{
		Operation:   "cert-rotation",
		Name:        v1alpha1.LocalObjectReference{Name: "cert-rot"},
		Cancellable: true,
		AcquiredAt:  metav1.NewTime(now()),
		DeadlineAt:  metav1.NewTime(now().Add(time.Hour)),
	}
	b.Spec.Acquisition = acquisition("etcd-restore", "etcd-snapshot-restore", false, true, 1800)

	out := Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseRunning, Now: now()})
	if out.UpdatedBeacon == nil {
		t.Fatalf("expected UpdatedBeacon")
	}
	if out.UpdatedBeacon.Status.State != v1alpha1.BeaconStateCancelling {
		t.Errorf("State = %q, want Cancelling", out.UpdatedBeacon.Status.State)
	}
	if out.UpdatedBeacon.Spec.Acquisition == nil {
		t.Errorf("Spec.Acquisition should be retained while pre-empting")
	}
	if out.UpdatedBeacon.Status.Cancellation == nil ||
		out.UpdatedBeacon.Status.Cancellation.PreemptingPlan.Name != "etcd-restore" {
		t.Errorf("Cancellation = %+v", out.UpdatedBeacon.Status.Cancellation)
	}
	if out.Preempt == nil || out.Preempt.PlanName != "cert-rot" {
		t.Errorf("Preempt = %+v", out.Preempt)
	}
}

func TestAcquiredNonCancellableRejectsCancelsOthers(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateAcquired
	b.Status.Active = true
	b.Status.Holder = &v1alpha1.BeaconHolder{
		Operation:   "etcd-snapshot-create",
		Name:        v1alpha1.LocalObjectReference{Name: "non-cancel"},
		Cancellable: false,
		AcquiredAt:  metav1.NewTime(now()),
		DeadlineAt:  metav1.NewTime(now().Add(time.Hour)),
	}
	b.Spec.Acquisition = acquisition("preempter", "etcd-snapshot-restore", false, true, 1800)

	out := Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseRunning, Now: now()})
	if out.Reject == nil || out.Reject.PlanName != "preempter" {
		t.Errorf("expected Reject for preempter; got %+v", out.Reject)
	}
}

func TestHolderSucceededReleasesBeacon(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateAcquired
	b.Status.Active = true
	b.Status.Holder = &v1alpha1.BeaconHolder{
		Operation: "etcd-snapshot-create",
		Name:      v1alpha1.LocalObjectReference{Name: "holder"},
	}
	out := Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseSucceeded, Now: now()})
	if out.UpdatedBeacon == nil {
		t.Fatalf("expected UpdatedBeacon")
	}
	if out.UpdatedBeacon.Status.State != v1alpha1.BeaconStateFree {
		t.Errorf("State = %q, want Free", out.UpdatedBeacon.Status.State)
	}
	if out.UpdatedBeacon.Status.Holder != nil {
		t.Errorf("Holder should be cleared")
	}
	if out.UpdatedBeacon.Status.Active {
		t.Errorf("Active should be false")
	}
}

func TestCancellingWaitsForCancelledPhase(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateCancelling
	b.Status.Active = true
	b.Status.Holder = &v1alpha1.BeaconHolder{
		Operation:   "cert-rotation",
		Name:        v1alpha1.LocalObjectReference{Name: "cert-rot"},
		Cancellable: true,
	}
	b.Status.Cancellation = &v1alpha1.BeaconCancellation{
		PreemptingOperation: "etcd-snapshot-restore",
		PreemptingPlan:      v1alpha1.LocalObjectReference{Name: "etcd-restore"},
	}
	b.Spec.Acquisition = acquisition("etcd-restore", "etcd-snapshot-restore", false, true, 1800)

	// Holder still Running — Cancelling persists.
	out := Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseRunning, Now: now()})
	if out.UpdatedBeacon != nil {
		t.Errorf("expected no update while waiting for holder Cancelled")
	}

	// Holder reaches Cancelled — beacon flips to Free; the pending
	// Acquisition remains (a subsequent reconcile will pick it up via
	// the now-Free path).
	out = Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseCancelled, Now: now()})
	if out.UpdatedBeacon == nil {
		t.Fatalf("expected UpdatedBeacon when holder Cancelled")
	}
	if out.UpdatedBeacon.Status.State != v1alpha1.BeaconStateAcquired {
		// After the release, the same Reconcile call processes the
		// pending acquisition and lands in Acquired — the etcd-restore
		// plan now holds the beacon.
		t.Errorf("State = %q, want Acquired (pending acq processed in same reconcile)", out.UpdatedBeacon.Status.State)
	}
	if out.UpdatedBeacon.Status.Holder == nil || out.UpdatedBeacon.Status.Holder.Name.Name != "etcd-restore" {
		t.Errorf("expected Holder to be etcd-restore; got %+v", out.UpdatedBeacon.Status.Holder)
	}
}

func TestTimeoutReleasesBeacon(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateAcquired
	b.Status.Active = true
	b.Status.Holder = &v1alpha1.BeaconHolder{
		Operation:  "etcd-snapshot-create",
		Name:       v1alpha1.LocalObjectReference{Name: "stalled"},
		AcquiredAt: metav1.NewTime(now().Add(-2 * time.Hour)),
		DeadlineAt: metav1.NewTime(now().Add(-time.Minute)),
	}
	out := Reconcile(StateInput{Beacon: b, HolderPhase: v1alpha1.ClusterPlanPhaseRunning, Now: now()})
	if out.UpdatedBeacon == nil {
		t.Fatalf("expected UpdatedBeacon for timeout")
	}
	if out.UpdatedBeacon.Status.State != v1alpha1.BeaconStateFree {
		t.Errorf("State = %q, want Free", out.UpdatedBeacon.Status.State)
	}
	if out.Reject == nil || out.Reject.Reason != "BeaconTimeout" {
		t.Errorf("expected timeout reject for holder; got %+v", out.Reject)
	}
}

func TestCancellingRejectsThirdRequester(t *testing.T) {
	b := freeBeacon()
	b.Status.State = v1alpha1.BeaconStateCancelling
	b.Status.Active = true
	b.Status.Cancellation = &v1alpha1.BeaconCancellation{
		PreemptingPlan: v1alpha1.LocalObjectReference{Name: "first-restore"},
	}
	// A different plan tries to acquire while we're still cancelling.
	b.Spec.Acquisition = acquisition("second-restore", "etcd-snapshot-restore", false, true, 1800)
	out := Reconcile(StateInput{Beacon: b, Now: now()})
	if out.Reject == nil || out.Reject.PlanName != "second-restore" {
		t.Errorf("expected Reject for second-restore; got %+v", out.Reject)
	}
}

func TestHolderTerminalGuard(t *testing.T) {
	cases := []struct {
		state v1alpha1.BeaconState
		phase string
		want  bool
	}{
		{v1alpha1.BeaconStateAcquired, v1alpha1.ClusterPlanPhaseSucceeded, true},
		{v1alpha1.BeaconStateAcquired, v1alpha1.ClusterPlanPhaseFailed, true},
		{v1alpha1.BeaconStateAcquired, v1alpha1.ClusterPlanPhaseCancelled, true},
		{v1alpha1.BeaconStateAcquired, v1alpha1.ClusterPlanPhaseRunning, false},
		{v1alpha1.BeaconStateCancelling, v1alpha1.ClusterPlanPhaseCancelled, true},
		{v1alpha1.BeaconStateCancelling, v1alpha1.ClusterPlanPhaseFailed, true},
		{v1alpha1.BeaconStateCancelling, v1alpha1.ClusterPlanPhaseRunning, false},
		{v1alpha1.BeaconStateFree, v1alpha1.ClusterPlanPhaseSucceeded, false},
	}
	for _, c := range cases {
		got := holderTerminal(c.state, c.phase)
		if got != c.want {
			t.Errorf("holderTerminal(%q, %q) = %v, want %v", c.state, c.phase, got, c.want)
		}
	}
}
