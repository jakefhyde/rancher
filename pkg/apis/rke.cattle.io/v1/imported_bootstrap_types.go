package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=importedbootstraps,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels={"cluster.x-k8s.io/v1beta1=v1","auth.cattle.io/cluster-indexed=true"}

// ImportedBootstrap is a bootstrap.
type ImportedBootstrap struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ImportedBootstrapSpec `json:"spec,omitempty"`
	// +optional
	Status ImportedBootstrapStatus `json:"status,omitempty"`
}

type ImportedBootstrapSpec struct {
	// ClusterName refers to the name of the CAPI Cluster associated with this RKEBootstrap.
	// +optional
	ClusterName string `json:"clusterName,omitempty"`
}

type ImportedBootstrapStatus struct {
	// Ready indicates the BootstrapData field is ready to be consumed.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	DataSecretName *string `json:"dataSecretName,omitempty"`
}
