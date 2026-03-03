package plan

import (
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/capr"
	capiv1beta2api "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func NewCAPRMachineInfo(machine *capiv1beta2api.Machine) (info MachineInfo) {
	return MachineInfo{
		Namespace: func() string {
			return machine.Namespace
		},
		PlanName: func() string {
			return machine.Name
		},
	}
}

func NewCAPRClusterInfo(controlPlane *rkev1.RKEControlPlane) (info ClusterInfo) {
	return ClusterInfo{
		Runtime: func() string {
			return capr.GetRuntime(controlPlane.Spec.KubernetesVersion)
		},
		Namespace: func() string {
			return controlPlane.Namespace
		},
	}
}
