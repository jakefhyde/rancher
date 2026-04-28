package capr

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/wire"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	capiapi "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

const testNS = "fleet-default"

// fakeMachineCache satisfies MachineCache for tests.
type fakeMachineCache struct {
	byName map[string]*capiapi.Machine
}

func (f *fakeMachineCache) Get(_, name string) (*capiapi.Machine, error) {
	m, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "machines"}, name)
	}
	return m, nil
}
func (f *fakeMachineCache) List(_ string, sel labels.Selector) ([]*capiapi.Machine, error) {
	var out []*capiapi.Machine
	for _, m := range f.byName {
		if sel == nil || sel.Matches(labels.Set(m.Labels)) {
			out = append(out, m)
		}
	}
	return out, nil
}

// fakeMachineClient — Update writes back to byName so subsequent reads see
// the change.
type fakeMachineClient struct {
	cache *fakeMachineCache
}

func (f *fakeMachineClient) Update(m *capiapi.Machine) (*capiapi.Machine, error) {
	f.cache.byName[m.Name] = m
	return m, nil
}

// fakeSecretClient mirrors a single namespace.
type fakeSecretClient struct {
	byName map[string]*corev1.Secret
}

func (f *fakeSecretClient) Get(_ string, name string, _ metav1.GetOptions) (*corev1.Secret, error) {
	s, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, name)
	}
	return s, nil
}
func (f *fakeSecretClient) Update(s *corev1.Secret) (*corev1.Secret, error) {
	f.byName[s.Name] = s
	return s, nil
}

// fakeRKEControlPlaneCache returns a single object indexed by name.
type fakeRKEControlPlaneCache struct {
	byName map[string]*rkev1.RKEControlPlane
}

func (f *fakeRKEControlPlaneCache) Get(_ string, name string) (*rkev1.RKEControlPlane, error) {
	cp, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "rkecontrolplanes"}, name)
	}
	return cp, nil
}

type fakeRKEBootstrapCache struct{}

func (f *fakeRKEBootstrapCache) Get(_, _ string) (*rkev1.RKEBootstrap, error) {
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "rkebootstraps"}, "")
}

func newDeps() (*fakeMachineCache, *fakeMachineClient, *fakeSecretClient, *fakeRKEControlPlaneCache, Deps) {
	mc := &fakeMachineCache{byName: map[string]*capiapi.Machine{}}
	mcli := &fakeMachineClient{cache: mc}
	sc := &fakeSecretClient{byName: map[string]*corev1.Secret{}}
	cpc := &fakeRKEControlPlaneCache{byName: map[string]*rkev1.RKEControlPlane{}}
	deps := Deps{
		Machines:         mc,
		MachinesClient:   mcli,
		Secrets:          sc,
		RKEControlPlanes: cpc,
		RKEBootstraps:    &fakeRKEBootstrapCache{},
	}
	return mc, mcli, sc, cpc, deps
}

func machine(name string, labelMap map[string]string, bootstrap string) *capiapi.Machine {
	m := &capiapi.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS,
			Labels:    labelMap,
		},
	}
	if bootstrap != "" {
		m.Spec.Bootstrap.ConfigRef = capiapi.ContractVersionedObjectReference{
			APIGroup: rkev1.SchemeGroupVersion.Group,
			Kind:     capr.RKEBootstrapKind,
			Name:     bootstrap,
		}
	}
	return m
}

func TestType(t *testing.T) {
	a := New(Deps{})
	if a.Type() != v1alpha1.ClusterTypeCAPR {
		t.Errorf("Type = %q, want %q", a.Type(), v1alpha1.ClusterTypeCAPR)
	}
}

func TestGetClusterEntrypoint(t *testing.T) {
	_, _, _, cpc, deps := newDeps()
	cpc.byName["my-cluster"] = &rkev1.RKEControlPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: testNS},
		Spec:       rkev1.RKEControlPlaneSpec{KubernetesVersion: "v1.31.1+rke2r1"},
	}
	a := New(deps)
	got, err := a.GetClusterEntrypoint(context.Background(), v1alpha1.ClusterReference{
		APIVersion: "provisioning.cattle.io/v1", Kind: "Cluster",
		Namespace: testNS, Name: "my-cluster",
	})
	if err != nil {
		t.Fatalf("GetClusterEntrypoint: %v", err)
	}
	if got.Kind != "RKEControlPlane" {
		t.Errorf("Kind = %q, want RKEControlPlane", got.Kind)
	}
	if got.Object == nil {
		t.Errorf("Object is nil")
	}
}

func TestListNodesFiltersByClusterAndSelector(t *testing.T) {
	mc, _, _, _, deps := newDeps()
	mc.byName["m1"] = machine("m1", map[string]string{
		capiapi.ClusterNameLabel: "my-cluster",
		"rke.cattle.io/etcd-role": "true",
	}, "boot-1")
	mc.byName["m2"] = machine("m2", map[string]string{
		capiapi.ClusterNameLabel: "my-cluster",
		"rke.cattle.io/worker-role": "true",
	}, "boot-2")
	mc.byName["m3"] = machine("m3", map[string]string{
		capiapi.ClusterNameLabel: "other-cluster",
	}, "boot-3")

	a := New(deps)
	ref := v1alpha1.ClusterReference{Namespace: testNS, Name: "my-cluster"}

	all, err := a.ListNodes(context.Background(), ref, labels.Everything())
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("got %d nodes, want 2 (m1, m2)", len(all))
	}

	etcdSel, _ := labels.Parse("rke.cattle.io/etcd-role=true")
	etcd, err := a.ListNodes(context.Background(), ref, etcdSel)
	if err != nil {
		t.Fatalf("ListNodes(etcd): %v", err)
	}
	if len(etcd) != 1 || etcd[0].Identifier != "m1" {
		t.Errorf("etcd selector = %v, want [m1]", etcd)
	}
}

func TestWriteNodePlanRoundTrip(t *testing.T) {
	mc, _, sc, _, deps := newDeps()
	mc.byName["m1"] = machine("m1", nil, "boot-1")
	planSecretName := capr.PlanSecretFromBootstrapName("boot-1")
	sc.byName[planSecretName] = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: planSecretName, Namespace: testNS},
		Type:       capr.SecretTypeMachinePlan,
		Data:       map[string][]byte{},
	}
	a := New(deps)
	np := &v1alpha1.NodePlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "snap-001-m1",
			Namespace: testNS,
			Labels:    map[string]string{v1alpha1.NodeNameLabel: "m1"},
		},
		Spec: v1alpha1.NodePlanSpec{
			Instructions: []v1alpha1.Instruction{
				{Name: "snap", Command: "rke2", Args: []string{"etcd-snapshot", "save"}, SaveOutput: true},
			},
		},
	}
	wirePlan, err := wire.ToWire(np.Spec)
	if err != nil {
		t.Fatalf("wire.ToWire: %v", err)
	}
	if err := a.WriteNodePlan(context.Background(), np, wirePlan); err != nil {
		t.Fatalf("WriteNodePlan: %v", err)
	}
	stored, ok := sc.byName[planSecretName]
	if !ok {
		t.Fatalf("plan secret %q not present", planSecretName)
	}
	got, err := wire.Unmarshal(stored.Data[wire.PlanSecretKeyPlan])
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Instructions) != 1 || got.Instructions[0].Command != "rke2" {
		t.Errorf("plan in secret didn't round-trip: %+v", got)
	}
}

func TestWriteNodePlanRequiresNodeNameLabel(t *testing.T) {
	_, _, _, _, deps := newDeps()
	a := New(deps)
	np := &v1alpha1.NodePlan{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: testNS},
	}
	err := a.WriteNodePlan(context.Background(), np, rkeplan.NodePlan{})
	if !errors.Is(err, adapter.ErrMissingNodeNameLabel) {
		t.Errorf("got %v, want ErrMissingNodeNameLabel", err)
	}
}

func TestApplyAndRemoveElectionLabel(t *testing.T) {
	mc, _, _, _, deps := newDeps()
	mc.byName["m1"] = machine("m1", map[string]string{"foo": "bar"}, "boot-1")
	a := New(deps)
	rec := adapter.NodeRecord{
		ClusterType: v1alpha1.ClusterTypeCAPR,
		Identifier:  "m1",
		Labels:      mc.byName["m1"].Labels,
		Object:      mc.byName["m1"],
	}
	if err := a.ApplyElectionLabel(context.Background(), rec, "rke.cattle.io/elected", "true"); err != nil {
		t.Fatalf("ApplyElectionLabel: %v", err)
	}
	if got := mc.byName["m1"].Labels["rke.cattle.io/elected"]; got != "true" {
		t.Errorf("label not applied: %v", mc.byName["m1"].Labels)
	}
	if err := a.RemoveElectionLabel(context.Background(), rec, "rke.cattle.io/elected"); err != nil {
		t.Fatalf("RemoveElectionLabel: %v", err)
	}
	if _, ok := mc.byName["m1"].Labels["rke.cattle.io/elected"]; ok {
		t.Errorf("label not removed: %v", mc.byName["m1"].Labels)
	}
}

func TestReadNodePlanStatusReportsPending(t *testing.T) {
	mc, _, _, _, deps := newDeps()
	mc.byName["m1"] = machine("m1", nil, "boot-1")
	a := New(deps)
	np := &v1alpha1.NodePlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "n",
			Namespace: testNS,
			Labels:    map[string]string{v1alpha1.NodeNameLabel: "m1"},
		},
	}
	got, err := a.ReadNodePlanStatus(context.Background(), np)
	if err != nil {
		t.Fatalf("ReadNodePlanStatus: %v", err)
	}
	if got.Phase != v1alpha1.NodePlanPhasePending {
		t.Errorf("Phase = %q, want Pending", got.Phase)
	}
}
