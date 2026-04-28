// Package trigger contains the controllers that translate legacy
// trigger surfaces into per-operation plan.cattle.io/v1alpha1 trigger
// CRs. The new framework's "trigger surface" is one CR per operation
// (currently just ETCDSnapshotCreate); this package's job is to keep
// the existing user-facing trigger flows (e.g. bumping
// RKEControlPlane.Spec.ETCDSnapshotCreate.Generation from the dashboard)
// working unchanged, by translating them into the new CRs when the
// owning cluster is opted into the framework.
//
// Once the framework is the default and the legacy dashboard / API
// behaviour is replaced (PR7), this package becomes unnecessary and
// can be deleted.
package trigger

import (
	"fmt"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	provv1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RKEControlPlaneCache reads the RKEControlPlane resource the
// controller watches.
type RKEControlPlaneCache interface {
	Get(namespace, name string) (*rkev1.RKEControlPlane, error)
}

// ProvisioningClusterCache reads the parent provisioning Cluster so
// the controller can check the per-cluster opt-in annotation.
type ProvisioningClusterCache interface {
	Get(namespace, name string) (*provv1.Cluster, error)
}

// ETCDSnapshotCreateClient is the create surface used to materialise
// trigger CRs.
type ETCDSnapshotCreateClient interface {
	Create(*v1alpha1.ETCDSnapshotCreate) (*v1alpha1.ETCDSnapshotCreate, error)
}

// Deps bundles the dependencies the RKEControlPlane handler operates on.
type Deps struct {
	ProvisioningClusters ProvisioningClusterCache
	Requests             ETCDSnapshotCreateClient
}

// RKEControlPlaneHandler translates RKEControlPlane gen-bumps into
// ETCDSnapshotCreate creations for opted-in clusters.
type RKEControlPlaneHandler struct {
	deps Deps
}

// NewRKEControlPlane returns an initialised handler.
func NewRKEControlPlane(deps Deps) *RKEControlPlaneHandler {
	return &RKEControlPlaneHandler{deps: deps}
}

// OnChange watches RKEControlPlane. When the parent provisioning
// Cluster carries the v1alpha1.UseNewDay2OpsAnnotation and the
// RKEControlPlane.Spec.ETCDSnapshotCreate has a non-zero generation,
// the handler ensures an ETCDSnapshotCreate exists for that
// generation. Idempotency falls out of the deterministic request name:
// AlreadyExists is treated as success.
func (h *RKEControlPlaneHandler) OnChange(_ string, cp *rkev1.RKEControlPlane) (*rkev1.RKEControlPlane, error) {
	if cp == nil || cp.DeletionTimestamp != nil {
		return cp, nil
	}
	if cp.Spec.ETCDSnapshotCreate == nil || cp.Spec.ETCDSnapshotCreate.Generation == 0 {
		return cp, nil
	}

	// Opt-in gate: the parent provisioning Cluster must carry the
	// annotation. We tolerate a missing parent (the planner watcher
	// may fire before the provisioning Cluster cache is hot) and treat
	// it as "not opted in" rather than erroring.
	parent, err := h.deps.ProvisioningClusters.Get(cp.Namespace, cp.Spec.ClusterName)
	if apierrors.IsNotFound(err) {
		return cp, nil
	}
	if err != nil {
		return cp, fmt.Errorf("trigger.rkecontrolplane: get provisioning cluster: %w", err)
	}
	if parent.Annotations[v1alpha1.UseNewDay2OpsAnnotation] != "true" {
		return cp, nil
	}

	// Deterministic name: cluster + gen. Re-running on the same
	// generation (e.g. controller restart) results in AlreadyExists,
	// which is a no-op.
	reqName := fmt.Sprintf("%s-etcd-snapshot-%d", cp.Name, cp.Spec.ETCDSnapshotCreate.Generation)

	req := &v1alpha1.ETCDSnapshotCreate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      reqName,
			Namespace: cp.Namespace,
			Labels: map[string]string{
				v1alpha1.ClusterPlanNameLabel: reqName,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: rkev1.SchemeGroupVersion.String(),
				Kind:       "RKEControlPlane",
				Name:       cp.Name,
				UID:        cp.UID,
				Controller: ptrBool(true),
			}},
		},
		Spec: v1alpha1.ETCDSnapshotCreateSpec{
			ClusterRef: v1alpha1.ClusterReference{
				APIVersion: provv1.SchemeGroupVersion.String(),
				Kind:       "Cluster",
				Namespace:  cp.Namespace,
				Name:       cp.Spec.ClusterName,
			},
			// Snapshot name left empty — the etcdsnapshot reconciler
			// passes "" through to the template, which yields a
			// runtime-default snapshot name on the node.
		},
	}

	if _, err := h.deps.Requests.Create(req); err != nil && !apierrors.IsAlreadyExists(err) {
		return cp, fmt.Errorf("trigger.rkecontrolplane: create ETCDSnapshotCreate: %w", err)
	}
	return cp, nil
}

func ptrBool(b bool) *bool { return &b }
