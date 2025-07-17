package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +kubebuilder:resource:path=importedcontrolplanes,shortName=icp,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta1=v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ImportedControlPlane struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ImportedControlPlaneSpec `json:"spec,omitempty"`
	// +optional
	Status ImportedControlPlaneStatus `json:"status,omitempty"`
}

type ImportedControlPlaneSpec struct {
}

type ImportedControlPlaneStatus struct {
	// +optional
	Ready bool `json:"ready,omitempty"`
	// +optional
	Initialized bool `json:"initialized,omitempty"`
}
