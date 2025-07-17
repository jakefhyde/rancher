package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels={"cluster.x-k8s.io/v1beta1=v1","auth.cattle.io/cluster-indexed=true"}

type ImportedMachine struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImportedMachineSpec `json:"spec,omitempty"`
	// +optional
	Status ImportedMachineStatus `json:"status,omitempty"`
}

type ImportedMachineSpec struct {
	// +optional
	ProviderID string `json:"providerID,omitempty"`
}

type ImportedMachineStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`
	// +optional
	Addresses []capi.MachineAddress `json:"addresses,omitempty"`
}
