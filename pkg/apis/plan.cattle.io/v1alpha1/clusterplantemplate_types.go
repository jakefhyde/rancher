package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:resource:path=clusterplantemplates,scope=Cluster,shortName=cpt
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Operation",type=string,JSONPath=".spec.operation"
// +kubebuilder:printcolumn:name="Cancellable",type=boolean,JSONPath=".spec.lifecycle.cancellable"
// +kubebuilder:printcolumn:name="Cancels",type=boolean,JSONPath=".spec.lifecycle.cancelsOthers"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPlanTemplate is the Go-templated source of a single day-2 operation.
// One ClusterPlanTemplate exists per built-in operation
// (etcd-snapshot-create, etcd-snapshot-restore, cert-rotation, ...).
// Operators may author their own templates.
//
// At trigger time, the renderer picks the entrypoint matching the cluster's
// type (capr → RKEControlPlane, caprke2 → RKE2ControlPlane, imported →
// management.cattle.io/v3.Cluster) and renders Spec.Stages into a
// ClusterPlan in the cluster's namespace.
type ClusterPlanTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterPlanTemplateSpec `json:"spec,omitempty"`

	// +optional
	Status ClusterPlanTemplateStatus `json:"status,omitempty"`
}

// ClusterPlanTemplateSpec is the desired state of a ClusterPlanTemplate.
type ClusterPlanTemplateSpec struct {
	// Operation is the canonical short name (e.g. "etcd-snapshot-create").
	// Must match the metadata.name for built-in templates.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
	Operation string `json:"operation"`

	// Entrypoints declares which "source-of-truth" object kinds this template
	// can be rendered against. The renderer picks the first listed entry
	// whose kind matches the cluster's resolved entrypoint.
	// +kubebuilder:validation:MinItems=1
	Entrypoints []EntrypointRef `json:"entrypoints"`

	// Lifecycle declares Beacon lock semantics for operations rendered from
	// this template.
	Lifecycle OperationLifecycle `json:"lifecycle"`

	// NodePlanTemplates lists every NodePlanTemplate (cluster-scoped) the
	// stages of this template may reference. Listed here so that the
	// builtin controller can validate references and so that a future
	// validating webhook can statically verify the graph.
	// +optional
	NodePlanTemplates []LocalClusterScopedRef `json:"nodePlanTemplates,omitempty"`

	// Plan is the templated body. Authored as a Go text/template document
	// that, when rendered, yields YAML for ClusterPlanSpec — at minimum
	// the `plan:` list of NodePoolPlan entries, and optionally the
	// cluster-wide files/probes/instructions and inputs.
	// +kubebuilder:validation:MinLength=1
	Plan string `json:"plan"`

	// DefaultImage, when set, is used for any Instruction within the
	// rendered ClusterPlan that omits its own Image.
	// +optional
	DefaultImage string `json:"defaultImage,omitempty"`
}

// ClusterPlanTemplateStatus is the observed state of a ClusterPlanTemplate.
type ClusterPlanTemplateStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions surface validation results: Ready (template parses),
	// Validated (entrypoint kinds resolve), Renderable (referenced
	// NodePlanTemplates exist).
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

