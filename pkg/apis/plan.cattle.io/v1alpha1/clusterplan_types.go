package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +kubebuilder:resource:path=clusterplans,scope=Namespaced,shortName=cp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Operation",type=string,JSONPath=".spec.operation"
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=".spec.clusterRef.name"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Step",type=integer,JSONPath=".status.currentStep"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPlan is the concrete rendered plan for one invocation of a day-2
// operation against one cluster. ClusterPlans live in the cluster's
// namespace: fleet-default for provisioning.cattle.io/v1.Cluster (CAPR /
// CAPRKE2), the management namespace c-XXXXX for imported clusters whose
// v3.Cluster is cluster-scoped.
//
// Spec.Plan holds an ordered list of NodePoolPlan entries — each pool
// targets a node set (via Selector) or a single elected node (via Election),
// inlines a NodePlanSpec describing what to do, and may carry before/after
// patches against arbitrary resources. Cluster-wide Files / Probes /
// Instructions (each carrying their own selectors) sit at the top level
// alongside Plan and are layered onto every matching node before pool
// execution.
//
// The trigger controller materialises a ClusterPlan and writes the Beacon
// acquisition request. Once the Beacon is held, the renderer fills Spec.Plan
// from Spec.TemplateRef. The stage controller drives execution and emits
// per-node NodePlan resources from each pool entry; system-agents discover
// their NodePlan via the Beacon's ActiveSelectors and the registration
// endpoint.
type ClusterPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterPlanSpec `json:"spec,omitempty"`

	// +optional
	Status ClusterPlanStatus `json:"status,omitempty"`
}

// ClusterPlanSpec is the desired state of a ClusterPlan.
type ClusterPlanSpec struct {
	// TemplateRef references the source ClusterPlanTemplate (cluster-scoped).
	// Empty means the plan was authored directly without a template.
	// +optional
	TemplateRef *LocalClusterScopedRef `json:"templateRef,omitempty"`

	// Operation is copied from the template for indexed lookup and for
	// human inspection in `kubectl get clusterplans -o wide`.
	// +kubebuilder:validation:MinLength=1
	Operation string `json:"operation"`

	// ClusterRef points at the parent cluster object. Always interpreted via
	// the cluster-type adapter:
	//   capr/caprke2 → provisioning.cattle.io/v1.Cluster
	//   imported     → management.cattle.io/v3.Cluster
	ClusterRef ClusterReference `json:"clusterRef"`

	// EntrypointRef pins which entrypoint object was used for rendering.
	// Useful when a template lists multiple entrypoints (e.g. "works on
	// either RKEControlPlane or RKE2ControlPlane").
	// +optional
	EntrypointRef *ObjectReference `json:"entrypointRef,omitempty"`

	// Plan is the ordered list of node-pool plans the framework executes
	// in sequence. Empty until the renderer fills it after Beacon
	// acquisition (or when authored directly).
	// +optional
	Plan []NodePoolPlan `json:"plan,omitempty"`

	// Files are cluster-wide files layered onto every matching node's
	// rendered NodePlan, keyed by per-entry selector.
	// +optional
	Files []NodePoolFile `json:"files,omitempty"`

	// Probes are cluster-wide probes layered onto every matching node's
	// rendered NodePlan.
	// +optional
	Probes []NodePoolProbe `json:"probes,omitempty"`

	// Instructions are cluster-wide instructions layered onto every
	// matching node's rendered NodePlan.
	// +optional
	Instructions []NodePoolInstruction `json:"instructions,omitempty"`

	// Inputs carries free-form parameters supplied at trigger time
	// (e.g. snapshotName, S3 bucket override). Available to templates as
	// `.Inputs`.
	// +optional
	Inputs map[string]string `json:"inputs,omitempty"`

	// Lifecycle is copied from the template's lifecycle so the Beacon
	// controller does not need to fetch the template to evaluate cancellation.
	Lifecycle OperationLifecycle `json:"lifecycle"`
}

// NodePoolPlan is one ordered step in a ClusterPlan: a target node set
// plus the NodePlanSpec to apply, plus optional before/after patches
// against arbitrary k8s resources.
type NodePoolPlan struct {
	// Name is optional. When omitted, status references the pool by
	// integer index in Plan.
	// +optional
	Name string `json:"name,omitempty"`

	// Selector selects nodes for this pool. Mutually exclusive with
	// Election. When both are nil, the pool is skipped (treated as
	// "no nodes match"); the framework records this in StageStatus.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Election elects a single node from a candidate pool, applies a
	// label, and re-evaluates eligibility on every reconcile so that
	// drift triggers re-election. Mutually exclusive with Selector.
	// +optional
	Election *NodeElection `json:"election,omitempty"`

	// Patches are applied around pool execution: Before runs prior to
	// emitting NodePlans, After runs once every selected NodePlan has
	// reached NodePlanPhaseSucceeded.
	// +optional
	Patches Patches `json:"patches,omitempty"`

	// Concurrency limits per-node parallelism in this pool. Encoded as
	// IntOrString so callers can say `"all"` or an integer. Defaults to
	// 1 when nil/empty.
	// +optional
	Concurrency *Concurrency `json:"concurrency,omitempty"`

	// NodePlanTemplateRef is an optional reference to a NodePlanTemplate
	// from which the per-node NodePlan is rendered. When unset, the
	// pool's inlined NodePlanSpec is used directly.
	// +optional
	NodePlanTemplateRef *LocalClusterScopedRef `json:"nodePlanTemplateRef,omitempty"`

	// NodePlanSpec is the per-node payload applied to every node in
	// this pool (or to the elected node if Election is set).
	NodePlanSpec `json:",inline"`
}

// NodeElection elects exactly one node from a candidate pool, applies a
// label to it, and re-evaluates eligibility on every reconcile so drift
// triggers re-election.
type NodeElection struct {
	// TargetLabel is applied to the elected node's "node-of-truth" object
	// (CAPI Machine for capr/caprke2, v3.Node for imported). The value
	// applied is "true".
	// +kubebuilder:validation:MinLength=1
	TargetLabel string `json:"targetLabel,omitempty"`

	// Selector restricts the candidate pool. Empty means any node in
	// the cluster is eligible.
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty"`

	// Criteria are templated boolean expressions (Go text/template). All
	// criteria must render to "true" for a node to remain elected. When
	// any criterion drifts to false, the label is removed and a new
	// election runs over the remaining candidates.
	// +optional
	Criteria []string `json:"criteria,omitempty"`
}

// Patches groups before/after sets applied around a pool's execution.
type Patches struct {
	// Before is applied prior to emitting NodePlans for the pool.
	// +optional
	Before []Patch `json:"before,omitempty"`

	// After is applied once every selected NodePlan has succeeded.
	// +optional
	After []Patch `json:"after,omitempty"`
}

// Patch is a named bundle of one or more patch definitions.
type Patch struct {
	// Name is a human-readable identifier surfaced in status.
	// +optional
	Name string `json:"name,omitempty"`

	// Definitions to apply, in order.
	// +optional
	Definitions []PatchDefinition `json:"definitions,omitempty"`
}

// PatchDefinition selects a target object and the JSON patches to apply.
type PatchDefinition struct {
	// Selector identifies the target.
	Selector PatchSelector `json:"selector,omitempty"`

	// JSONPatches is the RFC 6902 op list (already templated).
	JSONPatches []JSONPatch `json:"jsonPatches,omitempty"`
}

// PatchSelector targets a single object by GVK + namespace/name.
type PatchSelector struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

// JSONPatch is one RFC 6902 op. Value is encoded as a string in this version
// — callers JSON-encode complex values themselves to avoid the OpenAPI
// schema friction of opaque embedded values. May be widened to a structured
// value type in a follow-up.
type JSONPatch struct {
	Op    string `json:"op,omitempty"`
	Path  string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
}

// NodePoolFile is a cluster-wide file layered onto every matching node.
type NodePoolFile struct {
	// Selector selects which nodes receive the file. An empty selector
	// matches all nodes.
	// +optional
	Selector []metav1.LabelSelector `json:"selector,omitempty"`

	// File payload.
	File `json:",inline"`
}

// NodePoolProbe is a cluster-wide probe layered onto every matching node.
type NodePoolProbe struct {
	// Selector selects which nodes evaluate the probe.
	// +optional
	Selector []metav1.LabelSelector `json:"selector,omitempty"`

	// Probe payload.
	Probe `json:",inline"`
}

// NodePoolInstruction is a cluster-wide instruction layered onto every
// matching node.
type NodePoolInstruction struct {
	// Selector selects which nodes run the instruction.
	// +optional
	Selector []metav1.LabelSelector `json:"selector,omitempty"`

	// Instruction payload.
	Instruction `json:",inline"`
}

// NodePoolConcurrency is a per-selector concurrency override that may be
// declared at the ClusterPlan level. Reserved for future use; callers
// currently set Concurrency directly on each NodePoolPlan.
type NodePoolConcurrency struct {
	// Selector restricts which nodes the override applies to.
	// +optional
	Selector []metav1.LabelSelector `json:"selector,omitempty"`

	Concurrency `json:",inline"`
}

// Concurrency is the IntOrString form used in pool concurrency knobs.
// Callers may set "all" to fan out fully or an integer to cap parallelism.
type Concurrency = intstr.IntOrString

// ClusterPlanStatus is the observed state of a ClusterPlan.
type ClusterPlanStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// CurrentStep is the zero-based index of the pool currently in
	// progress within Spec.Plan.
	// +optional
	CurrentStep int `json:"currentStep,omitempty"`

	// Phase is the high-level lifecycle state (see ClusterPlanPhase*
	// constants).
	// +optional
	Phase string `json:"phase,omitempty"`

	// PersistentOutputs holds outputs marked Persistent=true on any
	// NodePlan descended from this ClusterPlan, lifted to the parent
	// for cross-pool access.
	// +optional
	PersistentOutputs map[string]string `json:"persistentOutputs,omitempty"`

	// Conditions surface fine-grained progress: BeaconHeld, Rendered,
	// NodePlansDelivered, InstructionsApplied, PatchesApplied,
	// Cancelled, TimedOut.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// StartedAt is when the Beacon was first requested.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is when the plan reached a terminal phase.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// ClusterPlan phase constants are plain string consts so unknown future
// phases can be tolerated by the API server without an enum migration.
const (
	ClusterPlanPhasePending    = "Pending"
	ClusterPlanPhaseAcquiring  = "Acquiring"
	ClusterPlanPhaseRunning    = "Running"
	ClusterPlanPhaseCancelling = "Cancelling"
	ClusterPlanPhaseSucceeded  = "Succeeded"
	ClusterPlanPhaseFailed     = "Failed"
	ClusterPlanPhaseCancelled  = "Cancelled"
)
