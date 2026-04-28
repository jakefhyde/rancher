// Package caprke2 is the upstream cluster-api-provider-rke2 adapter
// implementation. It is a scaffold in this PR — Type() returns
// "caprke2" so the framework knows the adapter exists, and every other
// method returns adapter.ErrNotImplemented. The wire format and
// transport are identical to the CAPR adapter; what's missing is the
// upstream RKE2ControlPlane / RKE2Bootstrap dependency and the
// per-machine plan secret naming convention they imply.
//
// Wiring this up means:
//
//   1. Adding github.com/rancher/cluster-api-provider-rke2 (or whatever
//      module hosts the upstream RKE2ControlPlane / RKE2Bootstrap CRD
//      types) to go.mod, then registering the corresponding
//      controller-gen output in pkg/codegen/main.go for client
//      generation.
//
//   2. Implementing GetClusterEntrypoint to return the upstream
//      RKE2ControlPlane sibling of the supplied provisioning Cluster.
//
//   3. Confirming the per-Machine plan-secret naming the upstream
//      provider uses (typically `<bootstrap>-machine-plan` again, but
//      worth verifying), and re-using pkg/clusterplan/wire.
package caprke2

import (
	"context"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"k8s.io/apimachinery/pkg/labels"
)

// New returns a scaffold CAPRKE2 adapter. The returned value implements
// adapter.Adapter; non-Type methods return adapter.ErrNotImplemented.
func New() *Adapter { return &Adapter{} }

// Adapter is the upstream-CAPRKE2 adapter (scaffold-only).
type Adapter struct{}

// Type identifies this adapter as the upstream CAPRKE2 implementation.
func (a *Adapter) Type() string { return v1alpha1.ClusterTypeCAPRKE2 }

func (a *Adapter) GetClusterEntrypoint(_ context.Context, _ v1alpha1.ClusterReference) (adapter.Entrypoint, error) {
	return adapter.Entrypoint{}, adapter.ErrNotImplemented
}

func (a *Adapter) ListNodes(_ context.Context, _ v1alpha1.ClusterReference, _ labels.Selector) ([]adapter.NodeRecord, error) {
	return nil, adapter.ErrNotImplemented
}

func (a *Adapter) GetNodeOwners(_ context.Context, _ adapter.NodeRecord) (adapter.OwnerSet, error) {
	return adapter.OwnerSet{}, adapter.ErrNotImplemented
}

func (a *Adapter) WriteNodePlan(_ context.Context, _ *v1alpha1.NodePlan, _ rkeplan.NodePlan) error {
	return adapter.ErrNotImplemented
}

func (a *Adapter) ReadNodePlanStatus(_ context.Context, _ *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error) {
	return v1alpha1.NodePlanStatus{}, adapter.ErrNotImplemented
}

func (a *Adapter) ApplyElectionLabel(_ context.Context, _ adapter.NodeRecord, _ string, _ string) error {
	return adapter.ErrNotImplemented
}

func (a *Adapter) RemoveElectionLabel(_ context.Context, _ adapter.NodeRecord, _ string) error {
	return adapter.ErrNotImplemented
}

func (a *Adapter) Namespace(_ context.Context, ref v1alpha1.ClusterReference) (string, error) {
	// Same convention as CAPR: ClusterPlan / NodePlan / Beacon live
	// alongside the provisioning Cluster.
	return ref.Namespace, nil
}

func (a *Adapter) SystemAgentReady(_ context.Context, _ adapter.NodeRecord) (bool, error) {
	return false, adapter.ErrNotImplemented
}
