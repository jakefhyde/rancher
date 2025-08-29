package imported

import (
	"context"

	mgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	capicontrollers "github.com/rancher/rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	mgmtcontrollers "github.com/rancher/rancher/pkg/generated/controllers/management.cattle.io/v3"
	rkecontrollers "github.com/rancher/rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/wrangler"
	name2 "github.com/rancher/wrangler/v3/pkg/name"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

type handler struct {
	capiClusters     capicontrollers.ClusterClient
	capiClusterCache capicontrollers.ClusterCache
	capiMachines     capicontrollers.MachineClient
	capiMachineCache capicontrollers.MachineCache

	mgmtClusters  mgmtcontrollers.ClusterClient
	infraClusters rkecontrollers.ImportedClusterClient
	controlPlanes rkecontrollers.ImportedControlPlaneController
}

func Register(ctx context.Context, clients *wrangler.Context) {
	h := &handler{
		capiClusters:     clients.CAPI.Cluster(),
		capiClusterCache: clients.CAPI.Cluster().Cache(),
		capiMachines:     clients.CAPI.Machine(),
		capiMachineCache: clients.CAPI.Machine().Cache(),
		mgmtClusters:     clients.Mgmt.Cluster(),
		infraClusters:    clients.RKE.ImportedCluster(),
		controlPlanes:    clients.RKE.ImportedControlPlane(),
	}

	mgmtcontrollers.RegisterClusterGeneratingHandler(ctx,
		clients.Mgmt.Cluster(),
		clients.Apply.WithDynamicLookup().
			WithCacheTypes(
				clients.CAPI.Cluster(),
				//TODO (add rest)
			),
		"ImportedClusterCAPI",
		"imported-back-populate",
		h.OnMgmtClusterChange,
		nil)

	clients.RKE.ImportedCluster().OnChange(ctx, "imported-cluster", h.OnImportedCluster)
	clients.RKE.ImportedControlPlane().OnChange(ctx, "imported-controlplane", h.OnImportedControlPlane)
}

func (h *handler) OnMgmtClusterChange(cluster *mgmtv3.Cluster, status mgmtv3.ClusterStatus) ([]runtime.Object, mgmtv3.ClusterStatus, error) {
	if cluster.DeletionTimestamp != nil {
		return nil, status, nil
	}

	if cluster.ObjectMeta.Annotations["objectset.rio.cattle.io/owner-gvk"] == "provisioning.cattle.io/v1, Kind=Cluster" {
		return nil, status, nil
	}

	if cluster.Spec.FleetWorkspaceName == "" {
		return nil, status, nil
	}

	return template(cluster), status, nil
}

func (h *handler) OnImportedCluster(_ string, cluster *rkev1.ImportedCluster) (*rkev1.ImportedCluster, error) {
	if cluster == nil {
		return nil, nil
	}

	if !cluster.Status.Ready {
		cluster = cluster.DeepCopy()
		cluster.Status.Ready = true
		return h.infraClusters.UpdateStatus(cluster)
	}

	return cluster, nil
}

func (h *handler) OnImportedControlPlane(_ string, controlplane *rkev1.ImportedControlPlane) (*rkev1.ImportedControlPlane, error) {
	if controlplane == nil {
		return nil, nil
	}

	if !controlplane.Status.Ready || !controlplane.Status.Initialized {
		controlplane = controlplane.DeepCopy()
		controlplane.Status.Ready = true
		controlplane.Status.Initialized = true
		return h.controlPlanes.UpdateStatus(controlplane)
	}

	return controlplane, nil
}

func template(cluster *mgmtv3.Cluster) []runtime.Object {
	name := name2.SafeConcatName(cluster.Name)
	controlPlane := &rkev1.ImportedControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Spec.FleetWorkspaceName,
			Name:      name,
		},
		Status: rkev1.ImportedControlPlaneStatus{
			Initialized: true,
			Ready:       true,
		},
	}

	infraCluster := &rkev1.ImportedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Spec.FleetWorkspaceName,
			Name:      name,
		},
		Spec: rkev1.ImportedClusterSpec{
			ControlPlaneEndpoint: &capi.APIEndpoint{
				Host: "localhost",
				Port: 6443,
			},
		},
		Status: rkev1.ImportedClusterStatus{
			Ready: true,
		},
	}

	cc := &capi.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Spec.FleetWorkspaceName,
			Name:      name,
		},
		Spec: capi.ClusterSpec{
			ControlPlaneEndpoint: capi.APIEndpoint{
				Host: "localhost",
				Port: 6443,
			},
			ControlPlaneRef: &corev1.ObjectReference{
				APIVersion: "rke.cattle.io/v1",
				Kind:       "ImportedControlPlane",
				Name:       controlPlane.Name,
				Namespace:  controlPlane.Namespace,
			},
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: "rke.cattle.io/v1",
				Kind:       "ImportedCluster",
				Name:       infraCluster.Name,
				Namespace:  infraCluster.Namespace,
			},
		},
	}

	return []runtime.Object{controlPlane, infraCluster, cc}
}
