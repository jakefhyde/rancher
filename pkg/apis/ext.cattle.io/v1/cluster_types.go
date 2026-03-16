package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type ClusterSpec struct {
}

type CAPIClusterSpec struct {
}

type CAPIClusterClassSpec struct {
}

type ImportedClusterSpec struct {
}

type EKSClusterSpec struct {
}

type ClusterStatus struct {
}

type Cluster struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata; More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}
