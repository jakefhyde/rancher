package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:resource:path=nodeplantemplates,scope=Cluster,shortName=npt
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodePlanTemplate is a per-node template parameterised over the cluster
// type's "node source-of-truth" object chain.
//
// For CAPR clusters the chain is { CAPI Machine, RKEBootstrap }; for
// CAPRKE2 it is { CAPI Machine, RKE2Bootstrap }; for imported clusters it
// is { management.cattle.io/v3.Node }. Each clusterType's owners are
// declared in Spec.SourceOfTruths so the renderer knows which objects the
// template may walk via the `owner` template function.
type NodePlanTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec NodePlanTemplateSpec `json:"spec,omitempty"`

	// +optional
	Status NodePlanTemplateStatus `json:"status,omitempty"`
}

// NodePlanTemplateSpec is the desired state of a NodePlanTemplate.
type NodePlanTemplateSpec struct {
	// SourceOfTruths declares which per-cluster-type "node SoT" objects
	// this template can be rendered against. The renderer picks the entry
	// whose ClusterType matches the parent cluster.
	// +kubebuilder:validation:MinItems=1
	SourceOfTruths []NodeSourceOfTruth `json:"sourceOfTruths"`

	// Spec is a Go text/template document that, when rendered, yields a
	// NodePlanSpec (Files/Instructions/Probes/Outputs). The renderer
	// strict-unmarshals the rendered output into NodePlanSpec and embeds
	// it into the materialised NodePlan (or into the calling
	// NodePoolPlan).
	// +optional
	Spec string `json:"spec,omitempty"`
}

// NodeSourceOfTruth declares the per-cluster-type root object for per-node
// rendering.
type NodeSourceOfTruth struct {
	// ClusterType is one of "capr", "caprke2", "imported".
	// +kubebuilder:validation:Enum=capr;caprke2;imported
	ClusterType string `json:"clusterType"`

	// PrimaryAPIVersion of the root .Node object passed to the template.
	// +kubebuilder:validation:MinLength=1
	PrimaryAPIVersion string `json:"primaryAPIVersion"`

	// PrimaryKind of the root .Node object.
	// +kubebuilder:validation:MinLength=1
	PrimaryKind string `json:"primaryKind"`

	// Owners is an ordered chain of object kinds the template may walk via
	// the `owner` template function. e.g. [{Machine}, {RKEBootstrap}] for
	// CAPR.
	// +optional
	Owners []EntrypointRef `json:"owners,omitempty"`
}

// NodePlanTemplateStatus is the observed state of a NodePlanTemplate.
type NodePlanTemplateStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions surface validation results: Ready (templates parse).
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

