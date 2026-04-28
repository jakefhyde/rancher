package trigger

import (
	"errors"
	"testing"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	provv1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const (
	testNS      = "fleet-default"
	testCluster = "my-cluster"
)

// fakeProvCache returns the same cluster for any name when set.
type fakeProvCache struct {
	byName map[string]*provv1.Cluster
}

func (f *fakeProvCache) Get(_, name string) (*provv1.Cluster, error) {
	c, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clusters"}, name)
	}
	return c, nil
}

// fakeRequestClient — Create stores by name.
type fakeRequestClient struct {
	byName map[string]*v1alpha1.ETCDSnapshotCreate
}

func (f *fakeRequestClient) Create(r *v1alpha1.ETCDSnapshotCreate) (*v1alpha1.ETCDSnapshotCreate, error) {
	if _, ok := f.byName[r.Name]; ok {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "etcdsnapshotcreates"}, r.Name)
	}
	f.byName[r.Name] = r
	return r, nil
}

func newDeps(optedIn bool) (*fakeProvCache, *fakeRequestClient, Deps) {
	pc := &fakeProvCache{byName: map[string]*provv1.Cluster{}}
	if optedIn {
		pc.byName[testCluster] = &provv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        testCluster,
				Namespace:   testNS,
				Annotations: map[string]string{v1alpha1.UseNewDay2OpsAnnotation: "true"},
			},
		}
	} else {
		pc.byName[testCluster] = &provv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: testCluster, Namespace: testNS},
		}
	}
	rc := &fakeRequestClient{byName: map[string]*v1alpha1.ETCDSnapshotCreate{}}
	return pc, rc, Deps{ProvisioningClusters: pc, Requests: rc}
}

func newRKEControlPlane(gen int) *rkev1.RKEControlPlane {
	cp := &rkev1.RKEControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testCluster,
			Namespace: testNS,
			UID:       types.UID("uid-cp"),
		},
		Spec: rkev1.RKEControlPlaneSpec{
			ClusterName: testCluster,
		},
	}
	if gen > 0 {
		cp.Spec.ETCDSnapshotCreate = &rkev1.ETCDSnapshotCreate{Generation: gen}
	}
	return cp
}

func TestNoOpWhenSnapshotCreateUnset(t *testing.T) {
	_, rc, deps := newDeps(true)
	h := NewRKEControlPlane(deps)
	if _, err := h.OnChange("", newRKEControlPlane(0)); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(rc.byName) != 0 {
		t.Errorf("no request should be created when ETCDSnapshotCreate is nil")
	}
}

func TestNoOpWhenClusterNotOptedIn(t *testing.T) {
	_, rc, deps := newDeps(false)
	h := NewRKEControlPlane(deps)
	if _, err := h.OnChange("", newRKEControlPlane(3)); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(rc.byName) != 0 {
		t.Errorf("no request should be created without opt-in annotation; got %v", rc.byName)
	}
}

func TestNoOpWhenParentMissing(t *testing.T) {
	pc, rc, deps := newDeps(false)
	delete(pc.byName, testCluster)
	h := NewRKEControlPlane(deps)
	if _, err := h.OnChange("", newRKEControlPlane(3)); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	if len(rc.byName) != 0 {
		t.Errorf("no request should be created when parent is missing")
	}
}

func TestCreatesRequestForOptedInGenBump(t *testing.T) {
	_, rc, deps := newDeps(true)
	h := NewRKEControlPlane(deps)
	if _, err := h.OnChange("", newRKEControlPlane(7)); err != nil {
		t.Fatalf("OnChange: %v", err)
	}
	want := testCluster + "-etcd-snapshot-7"
	req, ok := rc.byName[want]
	if !ok {
		t.Fatalf("expected request %q; got %v", want, rc.byName)
	}
	if req.Namespace != testNS {
		t.Errorf("namespace = %q, want %q", req.Namespace, testNS)
	}
	if req.Spec.ClusterRef.Kind != "Cluster" || req.Spec.ClusterRef.Name != testCluster {
		t.Errorf("ClusterRef = %+v", req.Spec.ClusterRef)
	}
	if len(req.OwnerReferences) != 1 || req.OwnerReferences[0].UID != "uid-cp" {
		t.Errorf("OwnerReferences = %+v", req.OwnerReferences)
	}
}

func TestIdempotentForRepeatGen(t *testing.T) {
	_, rc, deps := newDeps(true)
	h := NewRKEControlPlane(deps)
	// First call creates.
	if _, err := h.OnChange("", newRKEControlPlane(7)); err != nil {
		t.Fatalf("first OnChange: %v", err)
	}
	if len(rc.byName) != 1 {
		t.Fatalf("expected 1 request after first call")
	}
	// Second call sees AlreadyExists, returns nil.
	if _, err := h.OnChange("", newRKEControlPlane(7)); err != nil {
		t.Fatalf("second OnChange: %v", err)
	}
	if len(rc.byName) != 1 {
		t.Errorf("idempotency broken: %d requests after second call", len(rc.byName))
	}
}

func TestNewGenCreatesNewRequest(t *testing.T) {
	_, rc, deps := newDeps(true)
	h := NewRKEControlPlane(deps)
	if _, err := h.OnChange("", newRKEControlPlane(7)); err != nil {
		t.Fatalf("OnChange gen=7: %v", err)
	}
	if _, err := h.OnChange("", newRKEControlPlane(8)); err != nil {
		t.Fatalf("OnChange gen=8: %v", err)
	}
	if len(rc.byName) != 2 {
		t.Errorf("expected 2 requests for two gens; got %d (%v)", len(rc.byName), rc.byName)
	}
}

func TestNoOpForNilOrDeletedControlPlane(t *testing.T) {
	_, rc, deps := newDeps(true)
	h := NewRKEControlPlane(deps)
	if _, err := h.OnChange("", nil); err != nil {
		t.Errorf("nil should be a no-op; got %v", err)
	}
	cp := newRKEControlPlane(7)
	now := metav1.Now()
	cp.DeletionTimestamp = &now
	if _, err := h.OnChange("", cp); err != nil {
		t.Errorf("deleting cp should be a no-op; got %v", err)
	}
	if len(rc.byName) != 0 {
		t.Errorf("no request should be created for nil/deleted cp")
	}
}

// Compile-time hint that errors is used (for future expansion).
var _ = errors.New
