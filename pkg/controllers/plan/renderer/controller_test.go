package renderer

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
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
	testPlan    = "my-cluster-snap-7"
)

func fixedNow() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }

// --- fakes ------------------------------------------------------------------

type fakeTemplateCache struct {
	byName map[string]*v1alpha1.ClusterPlanTemplate
}

func (f *fakeTemplateCache) Get(name string) (*v1alpha1.ClusterPlanTemplate, error) {
	t, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clusterplantemplates"}, name)
	}
	return t, nil
}

type fakeClusterPlanClient struct {
	updates       []*v1alpha1.ClusterPlan
	statusUpdates []*v1alpha1.ClusterPlan
}

func (f *fakeClusterPlanClient) Update(p *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	f.updates = append(f.updates, p)
	return p.DeepCopy(), nil
}
func (f *fakeClusterPlanClient) UpdateStatus(p *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	f.statusUpdates = append(f.statusUpdates, p)
	return p.DeepCopy(), nil
}

type fakeBeaconCache struct {
	byKey map[string]*v1alpha1.Beacon
}

func (f *fakeBeaconCache) Get(ns, name string) (*v1alpha1.Beacon, error) {
	b, ok := f.byKey[ns+"/"+name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "beacons"}, name)
	}
	return b, nil
}

type fakeBeaconClient struct {
	statusUpdates []*v1alpha1.Beacon
}

func (f *fakeBeaconClient) UpdateStatus(b *v1alpha1.Beacon) (*v1alpha1.Beacon, error) {
	f.statusUpdates = append(f.statusUpdates, b)
	return b.DeepCopy(), nil
}

// fakeAdapter satisfies adapter.Adapter; only GetClusterEntrypoint is
// meaningful for renderer tests, the rest return zero values.
type fakeAdapter struct {
	entrypoint adapter.Entrypoint
}

func (f *fakeAdapter) Type() string { return v1alpha1.ClusterTypeCAPR }
func (f *fakeAdapter) GetClusterEntrypoint(_ context.Context, _ v1alpha1.ClusterReference) (adapter.Entrypoint, error) {
	return f.entrypoint, nil
}
func (f *fakeAdapter) ListNodes(_ context.Context, _ v1alpha1.ClusterReference, _ labels.Selector) ([]adapter.NodeRecord, error) {
	return nil, nil
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

func newDeps() (*fakeTemplateCache, *fakeClusterPlanClient, *fakeBeaconCache, *fakeBeaconClient, *fakeAdapter, Deps) {
	tc := &fakeTemplateCache{byName: map[string]*v1alpha1.ClusterPlanTemplate{}}
	cpc := &fakeClusterPlanClient{}
	bc := &fakeBeaconCache{byKey: map[string]*v1alpha1.Beacon{}}
	bcli := &fakeBeaconClient{}
	registry := adapter.NewRegistry()
	adp := &fakeAdapter{entrypoint: adapter.Entrypoint{
		APIVersion: "rke.cattle.io/v1",
		Kind:       "RKEControlPlane",
		Object: &rkev1.RKEControlPlane{
			ObjectMeta: metav1.ObjectMeta{Name: testCluster, Namespace: testNS},
			Spec:       rkev1.RKEControlPlaneSpec{KubernetesVersion: "v1.31.1+rke2r1"},
		},
	}}
	registry.Register(adp)
	engine := render.New(&render.FakeClient{}, fixedNow)
	return tc, cpc, bc, bcli, adp, Deps{
		Templates:       tc,
		ClusterPlans:    cpc,
		Beacons:         bc,
		BeaconsClient:   bcli,
		Adapters:        registry,
		Engine:          engine,
		RegistrationURL: "https://rancher.example.com/v3/connect/system-agent",
		Now:             fixedNow,
	}
}

func newRenderableTemplate(name, opName string) *v1alpha1.ClusterPlanTemplate {
	return &v1alpha1.ClusterPlanTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ClusterPlanTemplateSpec{
			Operation: opName,
			Entrypoints: []v1alpha1.EntrypointRef{
				{APIVersion: "rke.cattle.io/v1", Kind: "RKEControlPlane"},
			},
			Lifecycle: v1alpha1.OperationLifecycle{TimeoutSeconds: 1800},
			Plan: `
plan:
  - name: snapshot
    selector:
      matchLabels:
        rke.cattle.io/etcd-role: "true"
    instructions:
      - name: create
        command: {{ runtimeCommand .Entrypoint }}
        args: ["etcd-snapshot", "save"]
`,
		},
	}
}

func setupBeacon(deps Deps, beaconHolds bool) (*v1alpha1.ClusterPlan, *v1alpha1.Beacon) {
	plan := &v1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{Name: testPlan, Namespace: testNS},
		Spec: v1alpha1.ClusterPlanSpec{
			TemplateRef: &v1alpha1.LocalClusterScopedRef{Name: "etcd-snapshot-create-capr"},
			Operation:   "etcd-snapshot-create",
			ClusterRef: v1alpha1.ClusterReference{
				APIVersion: "provisioning.cattle.io/v1",
				Kind:       "Cluster",
				Namespace:  testNS,
				Name:       testCluster,
			},
		},
	}
	beacon := &v1alpha1.Beacon{
		ObjectMeta: metav1.ObjectMeta{Name: testCluster, Namespace: testNS},
	}
	if beaconHolds {
		beacon.Status.State = v1alpha1.BeaconStateAcquired
		beacon.Status.Holder = &v1alpha1.BeaconHolder{
			Operation: "etcd-snapshot-create",
			Name:      v1alpha1.LocalObjectReference{Name: testPlan},
		}
	}
	deps.Beacons.(*fakeBeaconCache).byKey[testNS+"/"+testCluster] = beacon
	return plan, beacon
}

func TestRendererSkipsWhenBeaconNotHeld(t *testing.T) {
	tc, cpc, _, _, _, deps := newDeps()
	tc.byName["etcd-snapshot-create-capr"] = newRenderableTemplate("etcd-snapshot-create-capr", "etcd-snapshot-create")
	plan, _ := setupBeacon(deps, false)
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.updates) != 0 || len(cpc.statusUpdates) != 0 {
		t.Errorf("renderer should be a no-op; updates=%d status=%d",
			len(cpc.updates), len(cpc.statusUpdates))
	}
}

func TestRendererPopulatesPlanAndActivation(t *testing.T) {
	tc, cpc, _, bcli, _, deps := newDeps()
	tc.byName["etcd-snapshot-create-capr"] = newRenderableTemplate("etcd-snapshot-create-capr", "etcd-snapshot-create")
	plan, _ := setupBeacon(deps, true)
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.updates) != 1 {
		t.Fatalf("expected 1 spec Update; got %d", len(cpc.updates))
	}
	got := cpc.updates[0]
	if len(got.Spec.Plan) != 1 || got.Spec.Plan[0].Name != "snapshot" {
		t.Errorf("rendered plan unexpected: %+v", got.Spec.Plan)
	}
	if got.Spec.Plan[0].Instructions[0].Command != "rke2" {
		t.Errorf("expected runtimeCommand=rke2; got %q", got.Spec.Plan[0].Instructions[0].Command)
	}

	if len(bcli.statusUpdates) != 1 {
		t.Fatalf("expected 1 beacon status update; got %d", len(bcli.statusUpdates))
	}
	bgot := bcli.statusUpdates[0]
	if bgot.Status.RegistrationEndpoint == "" {
		t.Errorf("RegistrationEndpoint not populated")
	}
	if len(bgot.Status.ActiveSelectors) != 1 {
		t.Errorf("ActiveSelectors len = %d, want 1; got %+v", len(bgot.Status.ActiveSelectors), bgot.Status.ActiveSelectors)
	}

	if len(cpc.statusUpdates) != 1 {
		t.Fatalf("expected 1 status update; got %d", len(cpc.statusUpdates))
	}
	if cpc.statusUpdates[0].Status.Phase != v1alpha1.ClusterPlanPhaseRunning {
		t.Errorf("Phase = %q, want Running", cpc.statusUpdates[0].Status.Phase)
	}
	if !hasCondition(cpc.statusUpdates[0].Status.Conditions, v1alpha1.ConditionRendered, metav1.ConditionTrue) {
		t.Errorf("expected Rendered=True; got %+v", cpc.statusUpdates[0].Status.Conditions)
	}
}

func TestRendererIdempotentAfterRendered(t *testing.T) {
	tc, cpc, _, _, _, deps := newDeps()
	tc.byName["etcd-snapshot-create-capr"] = newRenderableTemplate("etcd-snapshot-create-capr", "etcd-snapshot-create")
	plan, _ := setupBeacon(deps, true)
	plan.Status.Conditions = []metav1.Condition{{
		Type:   v1alpha1.ConditionRendered,
		Status: metav1.ConditionTrue,
	}}
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.updates)+len(cpc.statusUpdates) != 0 {
		t.Errorf("expected no writes when already rendered")
	}
}

func TestRendererTemplateMissingFails(t *testing.T) {
	_, cpc, _, _, _, deps := newDeps()
	plan, _ := setupBeacon(deps, true)
	if _, err := New(deps).OnChange("", plan); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(cpc.statusUpdates) != 1 {
		t.Fatalf("expected 1 fail status update; got %d", len(cpc.statusUpdates))
	}
	got := cpc.statusUpdates[0]
	if got.Status.Phase != v1alpha1.ClusterPlanPhaseFailed {
		t.Errorf("Phase = %q, want Failed", got.Status.Phase)
	}
	if !hasCondition(got.Status.Conditions, v1alpha1.ConditionRendered, metav1.ConditionFalse) {
		t.Errorf("expected Rendered=False; got %+v", got.Status.Conditions)
	}
}

func TestCollectActiveSelectors(t *testing.T) {
	spec := &v1alpha1.ClusterPlanSpec{
		Plan: []v1alpha1.NodePoolPlan{
			{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "1"}}},
			{Election: &v1alpha1.NodeElection{TargetLabel: "elected", Selector: metav1.LabelSelector{MatchLabels: map[string]string{"b": "2"}}}},
			{Election: &v1alpha1.NodeElection{TargetLabel: "x"}}, // empty selector — skipped
		},
		Files: []v1alpha1.NodePoolFile{
			{Selector: []metav1.LabelSelector{{MatchLabels: map[string]string{"c": "3"}}}},
		},
	}
	got := collectActiveSelectors(spec)
	if len(got) != 3 {
		t.Errorf("want 3 selectors; got %d (%+v)", len(got), got)
	}
}

func hasCondition(cs []metav1.Condition, t string, status metav1.ConditionStatus) bool {
	for _, c := range cs {
		if c.Type == t && c.Status == status {
			return true
		}
	}
	return false
}
