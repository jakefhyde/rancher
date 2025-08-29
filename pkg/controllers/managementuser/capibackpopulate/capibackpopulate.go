package capibackpopulate

import (
	"context"

	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	capicontrollers "github.com/rancher/rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	mgmtcontrollers "github.com/rancher/rancher/pkg/generated/controllers/management.cattle.io/v3"
	rkecontrollers "github.com/rancher/rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/types/config"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/v3/pkg/name"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

type handler struct {
	clusterName            string
	capiClusters           capicontrollers.ClusterClient
	capiClusterCache       capicontrollers.ClusterCache
	capiMachines           capicontrollers.MachineClient
	capiMachineCache       capicontrollers.MachineCache
	nodes                  corecontrollers.NodeClient
	nodeCache              corecontrollers.NodeCache
	mgmtClusterCache       mgmtcontrollers.ClusterCache
	importedMachines       rkecontrollers.ImportedMachineClient
	importedMachineCache   rkecontrollers.ImportedMachineCache
	importedBootstraps     rkecontrollers.ImportedBootstrapClient
	importedBootstrapCache rkecontrollers.ImportedBootstrapCache
}

func Register(ctx context.Context, downstream *config.UserContext) {
	if downstream.ClusterName == "local" {
		return
	}

	h := &handler{
		clusterName:            downstream.ClusterName,
		capiClusters:           downstream.Management.Wrangler.CAPI.Cluster(),
		capiClusterCache:       downstream.Management.Wrangler.CAPI.Cluster().Cache(),
		capiMachines:           downstream.Management.Wrangler.CAPI.Machine(),
		capiMachineCache:       downstream.Management.Wrangler.CAPI.Machine().Cache(),
		nodes:                  downstream.Corew.Node(),
		nodeCache:              downstream.Corew.Node().Cache(),
		mgmtClusterCache:       downstream.Management.Wrangler.Mgmt.Cluster().Cache(),
		importedMachines:       downstream.Management.Wrangler.RKE.ImportedMachine(),
		importedMachineCache:   downstream.Management.Wrangler.RKE.ImportedMachine().Cache(),
		importedBootstraps:     downstream.Management.Wrangler.RKE.ImportedBootstrap(),
		importedBootstrapCache: downstream.Management.Wrangler.RKE.ImportedBootstrap().Cache(),
	}

	downstream.Corew.Node().OnChange(ctx, "importedmachinebackpopulate", h.backPopulateMachine)
	downstream.Corew.Node().OnRemove(ctx, "importedmachinebackpopulate-remove", h.backPopulateMachine)
}

func (h *handler) backPopulateMachine(_ string, node *corev1.Node) (*corev1.Node, error) {
	if node == nil {
		return nil, nil
	}
	mgmtCluster, err := h.mgmtClusterCache.Get(h.clusterName)
	if apierrors.IsNotFound(err) {
		return nil, h.nodes.Delete(node.Name, &metav1.DeleteOptions{})
	} else if err != nil {
		return nil, err
	}

	if mgmtCluster.Spec.FleetWorkspaceName == "" {
		return node, nil
	}

	machineName := name.SafeConcatName(mgmtCluster.Name, node.Name)
	if node.DeletionTimestamp == nil {
		machine, err := h.capiMachineCache.Get(mgmtCluster.Spec.FleetWorkspaceName, machineName)
		if apierrors.IsNotFound(err) || err == nil {
			if node.Spec.ProviderID == "" {
				return node, nil
			}
			infraMachine := &rkev1.ImportedMachine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: mgmtCluster.Spec.FleetWorkspaceName,
					Name:      machineName,
				},
				Spec: rkev1.ImportedMachineSpec{
					ProviderID: node.Spec.ProviderID,
				},
			}
			infraMachine, err = h.importedMachines.Create(infraMachine)
			if apierrors.IsAlreadyExists(err) {
				infraMachine, err = h.importedMachineCache.Get(mgmtCluster.Spec.FleetWorkspaceName, machineName)
				if err != nil {
					return node, err
				}
			} else if err != nil {
				return node, err
			}
			if !infraMachine.Status.Ready {
				addresses := capi.MachineAddresses{}
				for _, address := range node.Status.Addresses {
					addresses = append(addresses, capi.MachineAddress{
						Type:    capi.MachineAddressType(address.Type),
						Address: address.Address,
					})
				}

				infraMachine := infraMachine.DeepCopy()
				infraMachine.Status.Ready = true
				infraMachine.Status.Addresses = addresses
				infraMachine, err = h.importedMachines.UpdateStatus(infraMachine)
				if err != nil {
					return node, err
				}
			}

			bootstrap := &rkev1.ImportedBootstrap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: mgmtCluster.Spec.FleetWorkspaceName,
					Name:      machineName,
				},
				Spec: rkev1.ImportedBootstrapSpec{
					ClusterName: h.clusterName,
				},
			}
			bootstrap, err = h.importedBootstraps.Create(bootstrap)
			if apierrors.IsAlreadyExists(err) {
				bootstrap, err = h.importedBootstrapCache.Get(mgmtCluster.Spec.FleetWorkspaceName, machineName)
				if err != nil {
					return node, err
				}
			} else if err != nil {
				return node, err
			}

			if !bootstrap.Status.Ready {
				bootstrap.Status.Ready = true
				bootstrap.Status.DataSecretName = &[]string{"test"}[0]
				bootstrap, err = h.importedBootstraps.UpdateStatus(bootstrap)
				if err != nil {
					return node, err
				}
			}
			// create infra machine object and set the infraRef
			machine = &capi.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: mgmtCluster.Spec.FleetWorkspaceName,
					Name:      machineName,
				},
				Spec: capi.MachineSpec{
					ClusterName: h.clusterName,
					Bootstrap: capi.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: bootstrap.APIVersion,
							Kind:       bootstrap.Kind,
							Name:       bootstrap.Name,
							Namespace:  bootstrap.Namespace,
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: infraMachine.APIVersion,
						Kind:       infraMachine.Kind,
						Name:       infraMachine.Name,
						Namespace:  infraMachine.Namespace,
					},
				},
			}
			_, err = h.capiMachines.Create(machine)
			if apierrors.IsAlreadyExists(err) {
				return node, nil
			} else if err != nil {
				return nil, err
			}
		}
		return nil, err
	}
	return node, h.capiMachines.Delete(mgmtCluster.Spec.FleetWorkspaceName, machineName, &metav1.DeleteOptions{})
}
