package plan

import (
	"fmt"

	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/wrangler/v3/pkg/data/convert"
	"k8s.io/apimachinery/pkg/runtime"
	capiv1beta2api "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

type MachineInfo struct {
	Namespace func() string
	PlanName  func() string
}

func NewMachineInfo(u runtime.Object) (MachineInfo, error) {
	k := u.GetObjectKind()
	switch {
	case k.GroupVersionKind().Group == "cluster.x-k8s.io" && k.GroupVersionKind().Kind == "Machine":
		machine := &capiv1beta2api.Machine{}
		err := convert.ToObj(u, machine)
		if err != nil {
			return MachineInfo{}, err
		}
		if machine.Spec.Bootstrap.ConfigRef.APIGroup == "rke.cattle.io" && machine.Spec.Bootstrap.ConfigRef.Kind == "RKEBootstrap" {
			return NewCAPRMachineInfo(machine), nil
		}
	}
	return MachineInfo{}, fmt.Errorf("unknown cluster type %v", k)
}

type ClusterInfo struct {
	Runtime   func() string
	Namespace func() string
}

func NewClusterInfo(u runtime.Object) (ClusterInfo, error) {
	k := u.GetObjectKind()
	switch {
	case k.GroupVersionKind().Group == "rke.cattle.io" && k.GroupVersionKind().Kind == "RKEControlPlane":
		controlPlane := &rkev1.RKEControlPlane{}
		err := convert.ToObj(u, controlPlane)
		if err != nil {
			return ClusterInfo{}, err
		}
		return NewCAPRClusterInfo(controlPlane), nil
	}
	return ClusterInfo{}, fmt.Errorf("unknown cluster type %v", k)
}
