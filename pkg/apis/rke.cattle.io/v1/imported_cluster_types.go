package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=importedclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels={"cluster.x-k8s.io/v1beta1=v1","auth.cattle.io/cluster-indexed=true"}

type ImportedCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImportedClusterSpec `json:"spec"`
	// +optional
	Status ImportedClusterStatus `json:"status,omitempty"`
}

type ImportedClusterSpec struct {
	// +optional
	ControlPlaneEndpoint *capi.APIEndpoint `json:"controlPlaneEndpoint,omitempty"`
}

type ImportedClusterStatus struct {
	// +optional
	Ready bool `json:"ready"`
}
