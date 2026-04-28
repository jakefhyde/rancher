package adapter

import (
	"context"
	"testing"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"k8s.io/apimachinery/pkg/labels"
)

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get(v1alpha1.ClusterTypeCAPR); ok {
		t.Errorf("empty registry should not return adapter")
	}
	r.Register(&fakeAdapter{kind: v1alpha1.ClusterTypeCAPR})
	r.Register(&fakeAdapter{kind: v1alpha1.ClusterTypeImported})
	for _, k := range []string{v1alpha1.ClusterTypeCAPR, v1alpha1.ClusterTypeImported} {
		a, ok := r.Get(k)
		if !ok {
			t.Errorf("registry missing %q", k)
			continue
		}
		if a.Type() != k {
			t.Errorf("registry returned wrong adapter for %q: got %q", k, a.Type())
		}
	}
	if _, ok := r.Get(v1alpha1.ClusterTypeCAPRKE2); ok {
		t.Errorf("registry returned an adapter for %q which was never registered", v1alpha1.ClusterTypeCAPRKE2)
	}
}

func TestSentinelErrorsExist(t *testing.T) {
	for _, e := range []error{ErrNotImplemented, ErrMissingNodeNameLabel, ErrSystemAgentNotReady, ErrUnknownClusterType} {
		if e == nil || e.Error() == "" {
			t.Errorf("sentinel error has empty message")
		}
	}
}

// --- fake adapter for the registry test ------------------------------------

type fakeAdapter struct {
	kind string
}

func (f *fakeAdapter) Type() string { return f.kind }
func (f *fakeAdapter) GetClusterEntrypoint(_ context.Context, _ v1alpha1.ClusterReference) (Entrypoint, error) {
	return Entrypoint{}, ErrNotImplemented
}
func (f *fakeAdapter) ListNodes(_ context.Context, _ v1alpha1.ClusterReference, _ labels.Selector) ([]NodeRecord, error) {
	return nil, ErrNotImplemented
}
func (f *fakeAdapter) GetNodeOwners(_ context.Context, _ NodeRecord) (OwnerSet, error) {
	return OwnerSet{}, ErrNotImplemented
}
func (f *fakeAdapter) WriteNodePlan(_ context.Context, _ *v1alpha1.NodePlan, _ rkeplan.NodePlan) error {
	return ErrNotImplemented
}
func (f *fakeAdapter) ReadNodePlanStatus(_ context.Context, _ *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error) {
	return v1alpha1.NodePlanStatus{}, ErrNotImplemented
}
func (f *fakeAdapter) ApplyElectionLabel(_ context.Context, _ NodeRecord, _, _ string) error {
	return ErrNotImplemented
}
func (f *fakeAdapter) RemoveElectionLabel(_ context.Context, _ NodeRecord, _ string) error {
	return ErrNotImplemented
}
func (f *fakeAdapter) Namespace(_ context.Context, _ v1alpha1.ClusterReference) (string, error) {
	return "", nil
}
func (f *fakeAdapter) SystemAgentReady(_ context.Context, _ NodeRecord) (bool, error) {
	return false, nil
}
