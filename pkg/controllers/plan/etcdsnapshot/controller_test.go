package etcdsnapshot

import (
	"errors"
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const (
	testNS      = "fleet-default"
	testCluster = "my-cluster"
	testReqName = "snap-001"
)

func fixedNow() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }

// fakeTemplateCache satisfies TemplateCache.
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

// fakeClusterPlanClient — Create stores by name; Get reads back.
type fakeClusterPlanClient struct {
	byName map[string]*v1alpha1.ClusterPlan
}

func (f *fakeClusterPlanClient) Get(_, name string, _ metav1.GetOptions) (*v1alpha1.ClusterPlan, error) {
	p, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clusterplans"}, name)
	}
	return p, nil
}
func (f *fakeClusterPlanClient) Create(p *v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error) {
	if _, ok := f.byName[p.Name]; ok {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "clusterplans"}, p.Name)
	}
	f.byName[p.Name] = p
	return p, nil
}

// fakeRequestClient just records the latest UpdateStatus.
type fakeRequestClient struct {
	last *v1alpha1.ETCDSnapshotCreate
}

func (f *fakeRequestClient) UpdateStatus(r *v1alpha1.ETCDSnapshotCreate) (*v1alpha1.ETCDSnapshotCreate, error) {
	f.last = r
	return r, nil
}

func newDeps() (*fakeTemplateCache, *fakeClusterPlanClient, *fakeRequestClient, Deps) {
	tc := &fakeTemplateCache{byName: map[string]*v1alpha1.ClusterPlanTemplate{}}
	cp := &fakeClusterPlanClient{byName: map[string]*v1alpha1.ClusterPlan{}}
	rc := &fakeRequestClient{}
	return tc, cp, rc, Deps{
		Templates:    tc,
		ClusterPlans: cp,
		Requests:     rc,
		Now:          fixedNow,
	}
}

func newRequest(clusterAPIVersion, clusterKind string) *v1alpha1.ETCDSnapshotCreate {
	return &v1alpha1.ETCDSnapshotCreate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testReqName,
			Namespace: testNS,
			UID:       types.UID("uid-snap-001"),
		},
		Spec: v1alpha1.ETCDSnapshotCreateSpec{
			ClusterRef: v1alpha1.ClusterReference{
				APIVersion: clusterAPIVersion,
				Kind:       clusterKind,
				Namespace:  testNS,
				Name:       testCluster,
			},
			Name: "manual-snap",
		},
	}
}

func newCAPRTemplate() *v1alpha1.ClusterPlanTemplate {
	return &v1alpha1.ClusterPlanTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "etcd-snapshot-create-capr"},
		Spec: v1alpha1.ClusterPlanTemplateSpec{
			Operation: "etcd-snapshot-create",
			Entrypoints: []v1alpha1.EntrypointRef{
				{APIVersion: "rke.cattle.io/v1", Kind: "RKEControlPlane"},
			},
			Lifecycle: v1alpha1.OperationLifecycle{TimeoutSeconds: 1800},
			Plan:      "plan: []",
		},
	}
}

func TestDetermineClusterType(t *testing.T) {
	cases := []struct {
		ref  v1alpha1.ClusterReference
		want string
	}{
		{v1alpha1.ClusterReference{APIVersion: "management.cattle.io/v3", Kind: "Cluster"}, v1alpha1.ClusterTypeImported},
		{v1alpha1.ClusterReference{APIVersion: "controlplane.cluster.x-k8s.io/v1beta1", Kind: "RKE2ControlPlane"}, v1alpha1.ClusterTypeCAPRKE2},
		{v1alpha1.ClusterReference{APIVersion: "provisioning.cattle.io/v1", Kind: "Cluster"}, v1alpha1.ClusterTypeCAPR},
		{v1alpha1.ClusterReference{APIVersion: "unknown/v1", Kind: "Foo"}, ""},
	}
	for _, c := range cases {
		got := DetermineClusterType(c.ref)
		if got != c.want {
			t.Errorf("DetermineClusterType(%v) = %q, want %q", c.ref, got, c.want)
		}
	}
}

func TestOnChangeCreatesClusterPlan(t *testing.T) {
	tc, cp, rc, deps := newDeps()
	tc.byName["etcd-snapshot-create-capr"] = newCAPRTemplate()
	h := New(deps)

	req := newRequest("provisioning.cattle.io/v1", "Cluster")
	got, err := h.OnChange("", req)
	if err != nil {
		t.Fatalf("OnChange: %v", err)
	}

	if got.Status.ClusterPlanRef == nil {
		t.Fatalf("Status.ClusterPlanRef not set; got: %+v", got.Status)
	}
	planName := got.Status.ClusterPlanRef.Name
	plan, ok := cp.byName[planName]
	if !ok {
		t.Fatalf("ClusterPlan %q not created; have: %v", planName, cp.byName)
	}
	if plan.Spec.Operation != "etcd-snapshot-create" {
		t.Errorf("Spec.Operation = %q", plan.Spec.Operation)
	}
	if plan.Spec.TemplateRef == nil || plan.Spec.TemplateRef.Name != "etcd-snapshot-create-capr" {
		t.Errorf("TemplateRef = %+v, want etcd-snapshot-create-capr", plan.Spec.TemplateRef)
	}
	if plan.Spec.ClusterRef.Name != testCluster {
		t.Errorf("ClusterRef.Name = %q", plan.Spec.ClusterRef.Name)
	}
	if plan.Spec.EntrypointRef == nil || plan.Spec.EntrypointRef.Kind != "RKEControlPlane" {
		t.Errorf("EntrypointRef = %+v, want RKEControlPlane", plan.Spec.EntrypointRef)
	}
	if plan.Spec.Inputs["snapshotName"] != "manual-snap" {
		t.Errorf("Inputs[snapshotName] = %q, want manual-snap", plan.Spec.Inputs["snapshotName"])
	}
	if plan.Namespace != testNS {
		t.Errorf("ClusterPlan namespace = %q, want %q", plan.Namespace, testNS)
	}
	// Owner reference set so deleting the request cascades to its plan.
	if len(plan.OwnerReferences) != 1 || plan.OwnerReferences[0].UID != req.UID {
		t.Errorf("OwnerReferences = %+v", plan.OwnerReferences)
	}
	// Status update was recorded.
	if rc.last == nil || rc.last.Status.ClusterPlanRef == nil {
		t.Errorf("status not updated via UpdateStatus")
	}
	// Ready=True condition.
	if c := getCondition(got.Status.Conditions, v1alpha1.ConditionReady); c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True, got %+v", c)
	}
}

func TestOnChangeIdempotentWhenAlreadyMaterialised(t *testing.T) {
	tc, cp, _, deps := newDeps()
	tc.byName["etcd-snapshot-create-capr"] = newCAPRTemplate()
	h := New(deps)

	req := newRequest("provisioning.cattle.io/v1", "Cluster")
	if _, err := h.OnChange("", req); err != nil {
		t.Fatalf("first OnChange: %v", err)
	}
	firstCount := len(cp.byName)

	// Second call should be a no-op now that req.Status.ClusterPlanRef
	// is set on the cached object — but the controller passes a fresh
	// object each invocation; mimic that by re-fetching the recorded
	// state from the request client.
	req.Status.ClusterPlanRef = &v1alpha1.LocalObjectReference{Name: testReqName + "-plan"}
	if _, err := h.OnChange("", req); err != nil {
		t.Fatalf("second OnChange: %v", err)
	}
	if got := len(cp.byName); got != firstCount {
		t.Errorf("ClusterPlan count grew on idempotent call: %d → %d", firstCount, got)
	}
}

func TestOnChangeUnsupportedClusterType(t *testing.T) {
	_, _, rc, deps := newDeps()
	h := New(deps)

	req := newRequest("unknown.example.com/v1", "Foo")
	got, err := h.OnChange("", req)
	if err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if got.Status.ClusterPlanRef != nil {
		t.Errorf("ClusterPlanRef should not be set for unsupported type")
	}
	c := getCondition(got.Status.Conditions, v1alpha1.ConditionReady)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != "UnsupportedClusterType" {
		t.Errorf("expected Ready=False/UnsupportedClusterType, got %+v", c)
	}
	if rc.last == nil {
		t.Errorf("status update not recorded")
	}
}

func TestOnChangeTemplateMissing(t *testing.T) {
	_, _, _, deps := newDeps()
	h := New(deps)

	req := newRequest("provisioning.cattle.io/v1", "Cluster")
	got, err := h.OnChange("", req)
	if err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	c := getCondition(got.Status.Conditions, v1alpha1.ConditionReady)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != "TemplateMissing" {
		t.Errorf("expected Ready=False/TemplateMissing, got %+v", c)
	}
}

func TestOnChangeImportedRoutesToImportedTemplate(t *testing.T) {
	tc, cp, _, deps := newDeps()
	tc.byName["etcd-snapshot-create-imported"] = &v1alpha1.ClusterPlanTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "etcd-snapshot-create-imported"},
		Spec: v1alpha1.ClusterPlanTemplateSpec{
			Operation: "etcd-snapshot-create",
			Entrypoints: []v1alpha1.EntrypointRef{
				{APIVersion: "management.cattle.io/v3", Kind: "Cluster"},
			},
			Plan: "plan: []",
		},
	}
	h := New(deps)

	req := newRequest("management.cattle.io/v3", "Cluster")
	if _, err := h.OnChange("", req); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	for _, p := range cp.byName {
		if p.Spec.TemplateRef.Name != "etcd-snapshot-create-imported" {
			t.Errorf("imported request routed to wrong template: %q", p.Spec.TemplateRef.Name)
		}
	}
}

func TestOnChangeIgnoresNilOrDeleted(t *testing.T) {
	_, _, _, deps := newDeps()
	h := New(deps)

	if got, err := h.OnChange("", nil); err != nil || got != nil {
		t.Errorf("nil should be a no-op")
	}

	req := newRequest("provisioning.cattle.io/v1", "Cluster")
	now := metav1.NewTime(fixedNow())
	req.DeletionTimestamp = &now
	got, err := h.OnChange("", req)
	if err != nil {
		t.Fatalf("OnChange on deleting req: %v", err)
	}
	if got.Status.ClusterPlanRef != nil {
		t.Errorf("should not materialise plans for deleting requests")
	}
}

// --- helpers ----------------------------------------------------------------

func getCondition(cs []metav1.Condition, t string) *metav1.Condition {
	for i := range cs {
		if cs[i].Type == t {
			return &cs[i]
		}
	}
	return nil
}

// Compile-time check that errors package is used somewhere — keeps the
// import meaningful for future expansion.
var _ = errors.New
