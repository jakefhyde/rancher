package day2ops

import (
	"fmt"

	"github.com/rancher/lasso/pkg/dynamic"
	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/wrangler/v3/pkg/data/convert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	capiv1beta2api "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

type ETCDSnapshotCreate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ETCDSnapshotCreateSpec   `json:"spec,omitempty"`
	Status ETCDSnapshotCreateStatus `json:"status,omitempty"`
}

type RestoreOption = string

const (
	RestoreOptionNone    RestoreOption = "none"
	RestoreOptionVersion RestoreOption = "version"
	RestoreOptionAll     RestoreOption = "all"
)

type ETCDSnapshotCreateSpec struct {
	SnapshotRef   *corev1.ObjectReference `json:"snapshotRef,omitempty"`
	RestoreOption RestoreOption           `json:"restoreOption,omitempty"`
}

type ETCDSnapshotCreateStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	Phase      string             `json:"phase,omitempty"`
}

type ETCDSnapshotCreateHandler struct {
	dynamic dynamic.Controller
}

func (h *ETCDSnapshotCreateHandler) OnChange(e *ETCDSnapshotCreate) error {
	if e == nil {
		return nil
	}

	if e.DeletionTimestamp != nil {
		return nil
	}

	if e.Spec.SnapshotRef == nil {
		return nil
	}

	if len(e.OwnerReferences) == 0 {
		return nil
	}

	var owner *metav1.OwnerReference
	for _, o := range e.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			owner = &o
			break
		}
	}

	if owner == nil {
		return nil
	}

	gvk := schema.FromAPIVersionAndKind(owner.APIVersion, owner.Kind)

	// todo(jhyde): check if namespaced
	namespace := "fleet-default"

	unstr, err := h.dynamic.Get(gvk, namespace, owner.Name)
	if err != nil {
		return err
	}

	info, err := NewClusterInfo(unstr)
	if err != nil {
		return err
	}

	_ = toClusterPlan(e, info)

	//err := reconcileClusterPlan(clusterPlan)
	//if err != nil {
	//	return err
	//}

	return nil
}


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

func NewCAPRMachineInfo(machine *capiv1beta2api.Machine) (info MachineInfo) {
	return MachineInfo{
		Namespace: func() string {
			return machine.Namespace
		},
	}
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
		return NewCAPRInfo(controlPlane), nil
	}
	return ClusterInfo{}, fmt.Errorf("unknown cluster type %v", k)
}

func NewCAPRInfo(controlPlane *rkev1.RKEControlPlane) (info ClusterInfo) {
	return ClusterInfo{
		Runtime: func() string {
			return capr.GetRuntime(controlPlane.Spec.KubernetesVersion)
		},
		Namespace: func() string {
			return controlPlane.Namespace
		},
	}
}

func toClusterPlan(e *ETCDSnapshotCreate, info ClusterInfo) planv1alpha1.ClusterPlan {
	plan := planv1alpha1.ClusterPlan{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{},
		Spec: planv1alpha1.ClusterPlanSpec{
			Plan: []planv1alpha1.NodePoolPlan{
				{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"plan.cattle.io/etcd-role": "true"}},
					NodePlanSpec: planv1alpha1.NodePlanSpec{
						Instructions: []planv1alpha1.Instruction{
							{
								Name:    "create snapshot",
								Command: info.Runtime(),
								Args: []string{
									"etcd-snapshot",
									"save",
								},
							},
						},
					},
				},
			},
		},
		Status: planv1alpha1.ClusterPlanStatus{},
	}
	return plan
}

type ClusterInfo struct {
	Runtime   func() string
	Namespace func() string
}
