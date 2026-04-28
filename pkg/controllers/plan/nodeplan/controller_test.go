package nodeplan

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/render"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	testNS      = "fleet-default"
	testCluster = "my-cluster"
	testPlan    = "my-cluster-snap"
)

func fixedNow() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }

// --- fakes ------------------------------------------------------------------

type fakeNodePlanClient struct {
	statusUpdates []*v1alpha1.NodePlan
}

func (f *fakeNodePlanClient) UpdateStatus(np *v1alpha1.NodePlan) (*v1alpha1.NodePlan, error) {
	f.statusUpdates = append(f.statusUpdates, np)
	return np.DeepCopy(), nil
}

type fakeClusterPlanCache struct {
	byName map[string]*v1alpha1.ClusterPlan
}

func (f *fakeClusterPlanCache) Get(_, name string) (*v1alpha1.ClusterPlan, error) {
	cp, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clusterplans"}, name)
	}
	return cp, nil
}

// fakeAdapter — recordable WriteNodePlan + replayable ReadNodePlanStatus.
type fakeAdapter struct {
	written      []*v1alpha1.NodePlan
	replyPhase   string
	replyOutputs map[string]v1alpha1.OutputStatus
}

func (f *fakeAdapter) Type() string { return v1alpha1.ClusterTypeCAPR }
func (f *fakeAdapter) GetClusterEntrypoint(_ context.Context, _ v1alpha1.ClusterReference) (adapter.Entrypoint, error) {
	return adapter.Entrypoint{}, nil
}
func (f *fakeAdapter) ListNodes(_ context.Context, _ v1alpha1.ClusterReference, _ labels.Selector) ([]adapter.NodeRecord, error) {
	return nil, nil
}
func (f *fakeAdapter) GetNodeOwners(_ context.Context, _ adapter.NodeRecord) (adapter.OwnerSet, error) {
	return adapter.OwnerSet{}, nil
}
func (f *fakeAdapter) WriteNodePlan(_ context.Context, np *v1alpha1.NodePlan, _ rkeplan.NodePlan) error {
	f.written = append(f.written, np)
	return nil
}
func (f *fakeAdapter) ReadNodePlanStatus(_ context.Context, _ *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error) {
	return v1alpha1.NodePlanStatus{
		Phase:   f.replyPhase,
		Outputs: f.replyOutputs,
	}, nil
}
func (f *fakeAdapter) ApplyElectionLabel(_ context.Context, _ adapter.NodeRecord, _, _ string) error {
	return nil
}
func (f *fakeAdapter) RemoveElectionLabel(_ context.Context, _ adapter.NodeRecord, _ string) error {
	return nil
}
func (f *fakeAdapter) Namespace(_ context.Context, _ v1alpha1.ClusterReference) (string, error) {
	return testNS, nil
}
func (f *fakeAdapter) SystemAgentReady(_ context.Context, _ adapter.NodeRecord) (bool, error) {
	return true, nil
}

// --- helpers ----------------------------------------------------------------

func newDeps(adp *fakeAdapter) (*fakeNodePlanClient, *fakeClusterPlanCache, Deps) {
	npcli := &fakeNodePlanClient{}
	cpc := &fakeClusterPlanCache{byName: map[string]*v1alpha1.ClusterPlan{
		testPlan: {
			ObjectMeta: metav1.ObjectMeta{Name: testPlan, Namespace: testNS},
			Spec: v1alpha1.ClusterPlanSpec{
				ClusterRef: v1alpha1.ClusterReference{
					APIVersion: "provisioning.cattle.io/v1",
					Kind:       "Cluster",
					Namespace:  testNS,
					Name:       testCluster,
				},
			},
		},
	}}
	registry := adapter.NewRegistry()
	registry.Register(adp)
	engine := render.New(&render.FakeClient{}, fixedNow)
	return npcli, cpc, Deps{
		NodePlans:    npcli,
		ClusterPlans: cpc,
		Adapters:     registry,
		Engine:       engine,
		Now:          fixedNow,
	}
}

func newPendingNodePlan() *v1alpha1.NodePlan {
	return &v1alpha1.NodePlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPlan + "-0-node-a",
			Namespace: testNS,
			Labels: map[string]string{
				v1alpha1.ClusterPlanNameLabel:    testPlan,
				v1alpha1.ClusterPlanPoolIndexLabel: "0",
				v1alpha1.NodeNameLabel:           "node-a",
			},
		},
		Spec: v1alpha1.NodePlanSpec{
			Instructions: []v1alpha1.Instruction{{Name: "create", Command: "rke2", Args: []string{"etcd-snapshot", "save"}}},
		},
	}
}

// --- tests ------------------------------------------------------------------

func TestDeliverOnFirstReconcile(t *testing.T) {
	adp := &fakeAdapter{}
	npcli, _, deps := newDeps(adp)
	np := newPendingNodePlan()
	if _, err := New(deps).OnChange("", np); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(adp.written) != 1 {
		t.Errorf("expected 1 WriteNodePlan call; got %d", len(adp.written))
	}
	if len(npcli.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update; got %d", len(npcli.statusUpdates))
	}
	if npcli.statusUpdates[0].Status.Phase != v1alpha1.NodePlanPhaseRunning {
		t.Errorf("Phase = %q, want Running", npcli.statusUpdates[0].Status.Phase)
	}
}

func TestSecondReconcileReadsStatus(t *testing.T) {
	adp := &fakeAdapter{
		replyPhase: v1alpha1.NodePlanPhaseSucceeded,
		replyOutputs: map[string]v1alpha1.OutputStatus{
			"snapshot-stdout": {Value: "ok", Persistent: true},
		},
	}
	npcli, _, deps := newDeps(adp)
	np := newPendingNodePlan()
	np.Status.Phase = v1alpha1.NodePlanPhaseRunning // already delivered
	if _, err := New(deps).OnChange("", np); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(adp.written) != 0 {
		t.Errorf("should not re-deliver when phase is Running; got %d writes", len(adp.written))
	}
	if len(npcli.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update; got %d", len(npcli.statusUpdates))
	}
	got := npcli.statusUpdates[0]
	if got.Status.Phase != v1alpha1.NodePlanPhaseSucceeded {
		t.Errorf("Phase = %q, want Succeeded", got.Status.Phase)
	}
	if got.Status.Outputs["snapshot-stdout"].Value != "ok" {
		t.Errorf("Outputs not propagated: %+v", got.Status.Outputs)
	}
}

func TestNoUpdateWhenStatusUnchanged(t *testing.T) {
	adp := &fakeAdapter{replyPhase: v1alpha1.NodePlanPhaseSucceeded}
	npcli, _, deps := newDeps(adp)
	np := newPendingNodePlan()
	np.Status.Phase = v1alpha1.NodePlanPhaseSucceeded
	if _, err := New(deps).OnChange("", np); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(npcli.statusUpdates) != 0 {
		t.Errorf("expected no status update for unchanged status; got %d", len(npcli.statusUpdates))
	}
}

func TestOrphanIsNoOp(t *testing.T) {
	adp := &fakeAdapter{}
	npcli, _, deps := newDeps(adp)
	np := newPendingNodePlan()
	delete(np.Labels, v1alpha1.ClusterPlanNameLabel)
	if _, err := New(deps).OnChange("", np); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(adp.written)+len(npcli.statusUpdates) != 0 {
		t.Errorf("orphan NodePlan should be a no-op")
	}
}

func TestUnknownClusterTypeFails(t *testing.T) {
	adp := &fakeAdapter{}
	npcli, cpc, deps := newDeps(adp)
	cpc.byName[testPlan].Spec.ClusterRef.APIVersion = "unknown/v1"
	cpc.byName[testPlan].Spec.ClusterRef.Kind = "Foo"
	np := newPendingNodePlan()
	if _, err := New(deps).OnChange("", np); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(npcli.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update; got %d", len(npcli.statusUpdates))
	}
	if npcli.statusUpdates[0].Status.Phase != v1alpha1.NodePlanPhaseFailed {
		t.Errorf("Phase = %q, want Failed", npcli.statusUpdates[0].Status.Phase)
	}
}

func TestStatusUnchangedPure(t *testing.T) {
	a := v1alpha1.NodePlanStatus{
		Phase:   v1alpha1.NodePlanPhaseSucceeded,
		Outputs: map[string]v1alpha1.OutputStatus{"x": {Value: "1"}},
	}
	if !statusUnchanged(a, a) {
		t.Errorf("identical statuses should be unchanged")
	}
	b := a
	b.Phase = v1alpha1.NodePlanPhaseFailed
	if statusUnchanged(a, b) {
		t.Errorf("phase change should be detected")
	}
	c := a
	c.Outputs = map[string]v1alpha1.OutputStatus{"x": {Value: "2"}}
	if statusUnchanged(a, c) {
		t.Errorf("output value change should be detected")
	}
}

// Compile-time: errors used implicitly.
var _ = errors.New
