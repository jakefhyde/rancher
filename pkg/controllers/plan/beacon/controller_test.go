package beacon

import (
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const testNS = "fleet-default"

// fakeBeaconClient stores the latest object for both Update and UpdateStatus.
type fakeBeaconClient struct {
	updates       []*v1alpha1.Beacon
	statusUpdates []*v1alpha1.Beacon
}

func (f *fakeBeaconClient) Update(b *v1alpha1.Beacon) (*v1alpha1.Beacon, error) {
	f.updates = append(f.updates, b)
	return b.DeepCopy(), nil
}
func (f *fakeBeaconClient) UpdateStatus(b *v1alpha1.Beacon) (*v1alpha1.Beacon, error) {
	f.statusUpdates = append(f.statusUpdates, b)
	return b.DeepCopy(), nil
}

type fakePlanCache struct {
	byName map[string]*v1alpha1.ClusterPlan
}

func (f *fakePlanCache) Get(_, name string) (*v1alpha1.ClusterPlan, error) {
	p, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clusterplans"}, name)
	}
	return p, nil
}

type fakePlansClient struct {
	statusUpdates []*v1alpha1.ClusterPlan
}

func (f *fakePlansClient) UpdateStatus(p *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	f.statusUpdates = append(f.statusUpdates, p)
	return p, nil
}

func newDeps() (*fakeBeaconClient, *fakePlanCache, *fakePlansClient, Deps) {
	bc := &fakeBeaconClient{}
	pc := &fakePlanCache{byName: map[string]*v1alpha1.ClusterPlan{}}
	pcli := &fakePlansClient{}
	return bc, pc, pcli, Deps{
		Beacons:      bc,
		ClusterPlans: pc,
		PlansClient:  pcli,
		Now:          func() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) },
	}
}

func TestOnChangeNilOrDeleted(t *testing.T) {
	bc, _, _, deps := newDeps()
	h := New(deps)
	if _, err := h.OnChange("", nil); err != nil {
		t.Errorf("nil should be no-op: %v", err)
	}
	now := metav1.NewTime(deps.Now())
	deleting := &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: testNS, DeletionTimestamp: &now},
	}
	if _, err := h.OnChange("", deleting); err != nil {
		t.Errorf("deleting should be no-op: %v", err)
	}
	if len(bc.updates)+len(bc.statusUpdates) != 0 {
		t.Errorf("expected no Beacon writes; got %d updates, %d status", len(bc.updates), len(bc.statusUpdates))
	}
}

func TestOnChangeAcceptsAcquisition(t *testing.T) {
	bc, _, _, deps := newDeps()
	h := New(deps)
	beacon := &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: testNS},
		Spec: v1alpha1.BeaconSpec{
			Acquisition: &v1alpha1.BeaconAcquisitionRequest{
				Operation:   "etcd-snapshot-create",
				ClusterPlan: v1alpha1.LocalObjectReference{Name: "plan-a"},
				UID:         types.UID("uid-a"),
				Lifecycle:   v1alpha1.OperationLifecycle{TimeoutSeconds: 1800},
				RequestedAt: metav1.NewTime(deps.Now()),
			},
		},
	}
	got, err := h.OnChange("", beacon)
	if err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if got.Status.State != v1alpha1.BeaconStateAcquired {
		t.Errorf("State = %q, want Acquired", got.Status.State)
	}
	if len(bc.updates) != 1 {
		t.Errorf("expected 1 Update call (Spec mutation); got %d", len(bc.updates))
	}
	if len(bc.statusUpdates) != 1 {
		t.Errorf("expected 1 UpdateStatus call; got %d", len(bc.statusUpdates))
	}
}

func TestOnChangeRejectionUpdatesRequesterStatus(t *testing.T) {
	_, pc, pcli, deps := newDeps()
	pc.byName["holder"] = &v1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "holder", Namespace: testNS},
		Status:     v1alpha1.ClusterPlanStatus{Phase: v1alpha1.ClusterPlanPhaseRunning},
	}
	pc.byName["requester"] = &v1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "requester", Namespace: testNS},
	}
	h := New(deps)
	beacon := &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: testNS},
		Status: v1alpha1.BeaconStatus{
			State:  v1alpha1.BeaconStateAcquired,
			Active: true,
			Holder: &v1alpha1.BeaconHolder{
				Operation:   "etcd-snapshot-create",
				Name:        v1alpha1.LocalObjectReference{Name: "holder"},
				Cancellable: false,
				DeadlineAt:  metav1.NewTime(deps.Now().Add(time.Hour)),
			},
		},
		Spec: v1alpha1.BeaconSpec{
			Acquisition: &v1alpha1.BeaconAcquisitionRequest{
				Operation:   "cert-rotation",
				ClusterPlan: v1alpha1.LocalObjectReference{Name: "requester"},
				UID:         types.UID("uid-req"),
				Lifecycle:   v1alpha1.OperationLifecycle{TimeoutSeconds: 600},
			},
		},
	}
	if _, err := h.OnChange("", beacon); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(pcli.statusUpdates) != 1 {
		t.Fatalf("expected 1 plan status update for rejection; got %d", len(pcli.statusUpdates))
	}
	upd := pcli.statusUpdates[0]
	if upd.Name != "requester" {
		t.Errorf("rejection should target requester; got %q", upd.Name)
	}
	if upd.Status.Phase != v1alpha1.ClusterPlanPhaseFailed {
		t.Errorf("requester Phase = %q, want Failed", upd.Status.Phase)
	}
	cond := findCondition(upd.Status.Conditions, v1alpha1.ConditionBeaconHeld)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Rejected" {
		t.Errorf("expected BeaconHeld=False/Rejected; got %+v", cond)
	}
}

func TestOnChangeReleasesOnHolderSucceeded(t *testing.T) {
	bc, pc, _, deps := newDeps()
	pc.byName["holder"] = &v1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{Name: "holder", Namespace: testNS},
		Status:     v1alpha1.ClusterPlanStatus{Phase: v1alpha1.ClusterPlanPhaseSucceeded},
	}
	h := New(deps)
	beacon := &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: testNS},
		Status: v1alpha1.BeaconStatus{
			State:  v1alpha1.BeaconStateAcquired,
			Active: true,
			Holder: &v1alpha1.BeaconHolder{
				Operation: "etcd-snapshot-create",
				Name:      v1alpha1.LocalObjectReference{Name: "holder"},
			},
		},
	}
	got, err := h.OnChange("", beacon)
	if err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if got.Status.State != v1alpha1.BeaconStateFree {
		t.Errorf("State = %q, want Free", got.Status.State)
	}
	if got.Status.Holder != nil {
		t.Errorf("Holder should be cleared")
	}
	if len(bc.statusUpdates) != 1 {
		t.Errorf("expected 1 status update; got %d", len(bc.statusUpdates))
	}
}

func TestHolderPhaseDeletedTreatedAsFailed(t *testing.T) {
	_, _, _, deps := newDeps()
	h := New(deps)
	beacon := &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: testNS},
		Status: v1alpha1.BeaconStatus{
			State: v1alpha1.BeaconStateAcquired,
			Holder: &v1alpha1.BeaconHolder{
				Operation: "x",
				Name:      v1alpha1.LocalObjectReference{Name: "ghost"},
			},
		},
	}
	got, err := h.OnChange("", beacon)
	if err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	// Holder fetch returns NotFound → treated as Failed → beacon
	// transitions to Free.
	if got.Status.State != v1alpha1.BeaconStateFree {
		t.Errorf("State = %q, want Free (deleted holder treated as Failed)", got.Status.State)
	}
}

// --- helpers ----------------------------------------------------------------

func findCondition(cs []metav1.Condition, t string) *metav1.Condition {
	for i := range cs {
		if cs[i].Type == t {
			return &cs[i]
		}
	}
	return nil
}
