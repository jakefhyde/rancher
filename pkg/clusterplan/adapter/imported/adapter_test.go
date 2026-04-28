package imported

import (
	"context"
	"testing"

	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/wire"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	testCluster = "c-m-12345"
	testNS      = testCluster
)

type fakeClusterCache struct {
	byName map[string]*apimgmtv3.Cluster
}

func (f *fakeClusterCache) Get(name string) (*apimgmtv3.Cluster, error) {
	c, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "clusters"}, name)
	}
	return c, nil
}

type fakeNodeCache struct {
	byName map[string]*apimgmtv3.Node
}

func (f *fakeNodeCache) Get(_, name string) (*apimgmtv3.Node, error) {
	n, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "nodes"}, name)
	}
	return n, nil
}
func (f *fakeNodeCache) List(_ string, sel labels.Selector) ([]*apimgmtv3.Node, error) {
	var out []*apimgmtv3.Node
	for _, n := range f.byName {
		if sel == nil || sel.Matches(labels.Set(n.Labels)) {
			out = append(out, n)
		}
	}
	return out, nil
}

type fakeNodeClient struct{ cache *fakeNodeCache }

func (f *fakeNodeClient) Update(n *apimgmtv3.Node) (*apimgmtv3.Node, error) {
	f.cache.byName[n.Name] = n
	return n, nil
}

// In-memory downstream-cluster Secret store keyed by cluster name.
type fakeDownstream struct {
	byCluster map[string]*fakeDownstreamSecrets
}

func (f *fakeDownstream) Secrets(clusterName string) (DownstreamSecrets, error) {
	if s, ok := f.byCluster[clusterName]; ok {
		return s, nil
	}
	s := &fakeDownstreamSecrets{byName: map[string]*corev1.Secret{}}
	f.byCluster[clusterName] = s
	return s, nil
}

type fakeDownstreamSecrets struct {
	byName map[string]*corev1.Secret
}

func (f *fakeDownstreamSecrets) Get(name string) (*corev1.Secret, error) {
	s, ok := f.byName[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, name)
	}
	return s, nil
}
func (f *fakeDownstreamSecrets) Create(s *corev1.Secret) (*corev1.Secret, error) {
	f.byName[s.Name] = s
	return s, nil
}
func (f *fakeDownstreamSecrets) Update(s *corev1.Secret) (*corev1.Secret, error) {
	f.byName[s.Name] = s
	return s, nil
}

func newDeps() (*fakeClusterCache, *fakeNodeCache, *fakeNodeClient, *fakeDownstream, Deps) {
	cc := &fakeClusterCache{byName: map[string]*apimgmtv3.Cluster{}}
	nc := &fakeNodeCache{byName: map[string]*apimgmtv3.Node{}}
	ncli := &fakeNodeClient{cache: nc}
	ds := &fakeDownstream{byCluster: map[string]*fakeDownstreamSecrets{}}
	return cc, nc, ncli, ds, Deps{
		Clusters:   cc,
		Nodes:      nc,
		NodeClient: ncli,
		Downstream: ds,
	}
}

func TestType(t *testing.T) {
	if got := New(Deps{}).Type(); got != v1alpha1.ClusterTypeImported {
		t.Errorf("Type = %q, want %q", got, v1alpha1.ClusterTypeImported)
	}
}

func TestGetClusterEntrypoint(t *testing.T) {
	cc, _, _, _, deps := newDeps()
	cc.byName[testCluster] = &apimgmtv3.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: testCluster},
	}
	a := New(deps)
	got, err := a.GetClusterEntrypoint(context.Background(), v1alpha1.ClusterReference{Name: testCluster})
	if err != nil {
		t.Fatalf("GetClusterEntrypoint: %v", err)
	}
	if got.Kind != "Cluster" || got.Object == nil {
		t.Errorf("Entrypoint = %+v", got)
	}
}

func TestListNodesFiltersByLabels(t *testing.T) {
	_, nc, _, _, deps := newDeps()
	nc.byName["n1"] = &apimgmtv3.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: testNS, Labels: map[string]string{"rke.cattle.io/etcd-role": "true"}},
	}
	nc.byName["n2"] = &apimgmtv3.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n2", Namespace: testNS, Labels: map[string]string{"rke.cattle.io/worker-role": "true"}},
	}
	a := New(deps)
	sel, _ := labels.Parse("rke.cattle.io/etcd-role=true")
	got, err := a.ListNodes(context.Background(), v1alpha1.ClusterReference{Name: testCluster}, sel)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(got) != 1 || got[0].Identifier != "n1" {
		t.Errorf("got %+v, want [n1]", got)
	}
}

func TestWriteNodePlanCreatesDownstreamSecret(t *testing.T) {
	_, _, _, ds, deps := newDeps()
	a := New(deps)
	np := &v1alpha1.NodePlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "snap-001-n1",
			Namespace: testNS,
			Labels: map[string]string{
				v1alpha1.NodeNameLabel:        "n1",
				v1alpha1.ClusterPlanNameLabel: testCluster,
			},
		},
		Spec: v1alpha1.NodePlanSpec{
			Instructions: []v1alpha1.Instruction{{Name: "snap", Command: "rke2"}},
		},
	}
	wirePlan, err := wire.ToWire(np.Spec)
	if err != nil {
		t.Fatalf("wire.ToWire: %v", err)
	}
	if err := a.WriteNodePlan(context.Background(), np, wirePlan); err != nil {
		t.Fatalf("WriteNodePlan: %v", err)
	}
	bucket, ok := ds.byCluster[testCluster]
	if !ok {
		t.Fatalf("downstream bucket for cluster missing")
	}
	secret, ok := bucket.byName["n1-machine-plan"]
	if !ok {
		t.Fatalf("downstream secret n1-machine-plan missing; got: %v", bucket.byName)
	}
	if secret.Type != capr.SecretTypeMachinePlan {
		t.Errorf("secret type = %q, want %q", secret.Type, capr.SecretTypeMachinePlan)
	}
	got, err := wire.Unmarshal(secret.Data[wire.PlanSecretKeyPlan])
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Instructions) != 1 || got.Instructions[0].Command != "rke2" {
		t.Errorf("plan didn't round-trip into downstream secret: %+v", got)
	}
}

func TestApplyAndRemoveElectionLabel(t *testing.T) {
	_, nc, _, _, deps := newDeps()
	nc.byName["n1"] = &apimgmtv3.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: testNS, Labels: map[string]string{}},
	}
	a := New(deps)
	rec := adapter.NodeRecord{
		ClusterType: v1alpha1.ClusterTypeImported,
		Identifier:  "n1",
		Object:      nc.byName["n1"],
	}
	if err := a.ApplyElectionLabel(context.Background(), rec, "rke.cattle.io/elected", "true"); err != nil {
		t.Fatalf("ApplyElectionLabel: %v", err)
	}
	if got := nc.byName["n1"].Labels["rke.cattle.io/elected"]; got != "true" {
		t.Errorf("label not applied: %v", nc.byName["n1"].Labels)
	}
	if err := a.RemoveElectionLabel(context.Background(), rec, "rke.cattle.io/elected"); err != nil {
		t.Fatalf("RemoveElectionLabel: %v", err)
	}
	if _, ok := nc.byName["n1"].Labels["rke.cattle.io/elected"]; ok {
		t.Errorf("label not removed: %v", nc.byName["n1"].Labels)
	}
}

func TestNamespacePrefersExplicit(t *testing.T) {
	a := New(Deps{})
	got, _ := a.Namespace(context.Background(), v1alpha1.ClusterReference{Namespace: "explicit", Name: "ignored"})
	if got != "explicit" {
		t.Errorf("got %q, want explicit", got)
	}
	got, _ = a.Namespace(context.Background(), v1alpha1.ClusterReference{Name: "c-m-foo"})
	if got != "c-m-foo" {
		t.Errorf("got %q, want c-m-foo", got)
	}
}
