package stage

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const (
	testNS      = "fleet-default"
	testCluster = "my-cluster"
	testPlan    = "my-cluster-snap"
)

func fixedNow() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }

// --- fakes ------------------------------------------------------------------

type fakeClusterPlanClient struct {
	statusUpdates []*v1alpha1.ClusterPlan
}

func (f *fakeClusterPlanClient) UpdateStatus(p *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	f.statusUpdates = append(f.statusUpdates, p)
	return p.DeepCopy(), nil
}

type fakeNodePlanCache struct {
	byName map[string]*v1alpha1.NodePlan
}

func (f *fakeNodePlanCache) List(_ string, sel labels.Selector) ([]*v1alpha1.NodePlan, error) {
	var out []*v1alpha1.NodePlan
	for _, p := range f.byName {
		if sel.Matches(labels.Set(p.Labels)) {
			out = append(out, p)
		}
	}
	return out, nil
}

type fakeNodePlanClient struct {
	byName map[string]*v1alpha1.NodePlan
}

func (f *fakeNodePlanClient) Create(p *v1alpha1.NodePlan) (*v1alpha1.NodePlan, error) {
	if _, ok := f.byName[p.Name]; ok {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "nodeplans"}, p.Name)
	}
	f.byName[p.Name] = p
	return p, nil
}

// fakeAdapter — ListNodes returns whatever was pre-registered.
type fakeAdapter struct {
	nodes []adapter.NodeRecord
}

func (f *fakeAdapter) Type() string { return v1alpha1.ClusterTypeCAPR }
func (f *fakeAdapter) GetClusterEntrypoint(_ context.Context, _ v1alpha1.ClusterReference) (adapter.Entrypoint, error) {
	return adapter.Entrypoint{}, nil
}
func (f *fakeAdapter) ListNodes(_ context.Context, _ v1alpha1.ClusterReference, sel labels.Selector) ([]adapter.NodeRecord, error) {
	var out []adapter.NodeRecord
	for _, n := range f.nodes {
		if sel.Matches(labels.Set(n.Labels)) {
			out = append(out, n)
		}
	}
	return out, nil
}
func (f *fakeAdapter) GetNodeOwners(_ context.Context, _ adapter.NodeRecord) (adapter.OwnerSet, error) {
	return adapter.OwnerSet{}, nil
}
func (f *fakeAdapter) WriteNodePlan(_ context.Context, _ *v1alpha1.NodePlan, _ rkeplan.NodePlan) error {
	return nil
}
func (f *fakeAdapter) ReadNodePlanStatus(_ context.Context, _ *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error) {
	return v1alpha1.NodePlanStatus{}, nil
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

func newDeps(nodes []adapter.NodeRecord) (*fakeClusterPlanClient, *fakeNodePlanCache, *fakeNodePlanClient, Deps) {
	cpc := &fakeClusterPlanClient{}
	npc := &fakeNodePlanCache{byName: map[string]*v1alpha1.NodePlan{}}
	npcli := &fakeNodePlanClient{byName: map[string]*v1alpha1.NodePlan{}}
	registry := adapter.NewRegistry()
	registry.Register(&fakeAdapter{nodes: nodes})
	return cpc, npc, npcli, Deps{
		ClusterPlans:    cpc,
		NodePlans:       npc,
		NodePlansClient: npcli,
		Adapters:        registry,
		Now:             fixedNow,
	}
}

func newRenderedPlan(step int, pools ...v1alpha1.NodePoolPlan) *v1alpha1.ClusterPlan {
	return &v1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{Name: testPlan, Namespace: testNS, UID: types.UID("uid-cp")},
		Spec: v1alpha1.ClusterPlanSpec{
			Operation: "etcd-snapshot-create",
			ClusterRef: v1alpha1.ClusterReference{
				APIVersion: "provisioning.cattle.io/v1",
				Kind:       "Cluster",
				Namespace:  testNS,
				Name:       testCluster,
			},
			Plan: pools,
		},
		Status: v1alpha1.ClusterPlanStatus{
			CurrentStep: step,
			Conditions: []metav1.Condition{{
				Type:   v1alpha1.ConditionRendered,
				Status: metav1.ConditionTrue,
			}},
		},
	}
}

func etcdNodes() []adapter.NodeRecord {
	return []adapter.NodeRecord{
		{ClusterType: v1alpha1.ClusterTypeCAPR, Identifier: "node-a", Labels: map[string]string{"rke.cattle.io/etcd-role": "true"}},
		{ClusterType: v1alpha1.ClusterTypeCAPR, Identifier: "node-b", Labels: map[string]string{"rke.cattle.io/etcd-role": "true"}},
	}
}

func snapshotPool() v1alpha1.NodePoolPlan {
	return v1alpha1.NodePoolPlan{
		Name:     "snapshot",
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"rke.cattle.io/etcd-role": "true"}},
		NodePlanSpec: v1alpha1.NodePlanSpec{
			Instructions: []v1alpha1.Instruction{{Name: "create", Command: "rke2"}},
		},
	}
}

// --- tests ------------------------------------------------------------------

func TestStageSkipsUntilRendered(t *testing.T) {
	cpc, _, npcli, deps := newDeps(etcdNodes())
	plan := newRenderedPlan(0, snapshotPool())
	plan.Status.Conditions = nil // not yet rendered
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.statusUpdates) != 0 || len(npcli.byName) != 0 {
		t.Errorf("stage should be no-op until Rendered=True")
	}
}

func TestStageMaterialisesNodePlans(t *testing.T) {
	cpc, _, npcli, deps := newDeps(etcdNodes())
	plan := newRenderedPlan(0, snapshotPool())
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(npcli.byName) != 2 {
		t.Errorf("expected 2 NodePlans created (one per matched node); got %d (%v)",
			len(npcli.byName), npcli.byName)
	}
	for _, np := range npcli.byName {
		if np.Labels[v1alpha1.NodeNameLabel] == "" {
			t.Errorf("NodePlan %q missing NodeNameLabel", np.Name)
		}
		if np.Labels[v1alpha1.ClusterPlanNameLabel] != testPlan {
			t.Errorf("NodePlan %q ClusterPlanNameLabel=%q, want %q",
				np.Name, np.Labels[v1alpha1.ClusterPlanNameLabel], testPlan)
		}
		if len(np.OwnerReferences) != 1 || np.OwnerReferences[0].UID != "uid-cp" {
			t.Errorf("NodePlan %q OwnerReferences = %+v", np.Name, np.OwnerReferences)
		}
		if len(np.Spec.Instructions) != 1 || np.Spec.Instructions[0].Command != "rke2" {
			t.Errorf("NodePlan %q Spec not inlined: %+v", np.Name, np.Spec)
		}
	}
	// Plan should not advance yet — NodePlans haven't reported success.
	if len(cpc.statusUpdates) != 0 {
		t.Errorf("plan should not advance before NodePlans complete")
	}
}

func TestStageAdvancesWhenNodePlansSucceed(t *testing.T) {
	cpc, npc, npcli, deps := newDeps(etcdNodes())
	plan := newRenderedPlan(0, snapshotPool())

	// First call materialises NodePlans.
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("first OnChange: %v", err)
	}
	// Mark all materialised NodePlans Succeeded and feed them to the
	// cache so the second call sees them.
	for name, np := range npcli.byName {
		np.Status.Phase = v1alpha1.NodePlanPhaseSucceeded
		npc.byName[name] = np
	}
	// Second call: aggregates phase, advances. With one pool that
	// completes, the plan transitions to Succeeded directly.
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("second OnChange: %v", err)
	}
	if len(cpc.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update; got %d", len(cpc.statusUpdates))
	}
	if cpc.statusUpdates[0].Status.Phase != v1alpha1.ClusterPlanPhaseSucceeded {
		t.Errorf("Phase = %q, want Succeeded", cpc.statusUpdates[0].Status.Phase)
	}
}

func TestStageMarksFailedWhenAnyNodePlanFails(t *testing.T) {
	cpc, npc, npcli, deps := newDeps(etcdNodes())
	plan := newRenderedPlan(0, snapshotPool())

	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("first OnChange: %v", err)
	}
	// Mark one Failed, one Succeeded.
	first := true
	for name, np := range npcli.byName {
		if first {
			np.Status.Phase = v1alpha1.NodePlanPhaseFailed
			first = false
		} else {
			np.Status.Phase = v1alpha1.NodePlanPhaseSucceeded
		}
		npc.byName[name] = np
	}
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("second OnChange: %v", err)
	}
	last := cpc.statusUpdates[len(cpc.statusUpdates)-1]
	if last.Status.Phase != v1alpha1.ClusterPlanPhaseFailed {
		t.Errorf("Phase = %q, want Failed", last.Status.Phase)
	}
}

func TestStageNoOpForTerminalPlan(t *testing.T) {
	cpc, _, _, deps := newDeps(etcdNodes())
	plan := newRenderedPlan(1, snapshotPool())
	plan.Status.Phase = v1alpha1.ClusterPlanPhaseSucceeded
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.statusUpdates) != 0 {
		t.Errorf("stage should be no-op for terminal plan")
	}
}

func TestStageEmptyPoolAdvances(t *testing.T) {
	cpc, _, _, deps := newDeps(nil) // no nodes match the selector
	plan := newRenderedPlan(0, snapshotPool())
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update for empty-pool advance; got %d", len(cpc.statusUpdates))
	}
	// Single-pool plan: advance from 0 to 1 ⇒ markSucceeded fires.
	if cpc.statusUpdates[0].Status.Phase != v1alpha1.ClusterPlanPhaseSucceeded {
		t.Errorf("Phase = %q, want Succeeded", cpc.statusUpdates[0].Status.Phase)
	}
}

func TestStageNoOpNilOrDeleted(t *testing.T) {
	cpc, _, _, deps := newDeps(etcdNodes())
	if _, err := New(deps).OnChange("", nil); err != nil {
		t.Fatalf("nil OnChange: %v", err)
	}
	plan := newRenderedPlan(0, snapshotPool())
	now := metav1.NewTime(fixedNow())
	plan.DeletionTimestamp = &now
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("deleting OnChange: %v", err)
	}
	if len(cpc.statusUpdates) != 0 {
		t.Errorf("expected no writes for nil/deleted")
	}
}
