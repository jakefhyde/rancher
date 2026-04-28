package v1alpha1

const (
	// UseNewDay2OpsAnnotation, when set to "true" on a provisioning.cattle.io/v1.Cluster
	// or a management.cattle.io/v3.Cluster, opts that cluster into the new
	// ClusterPlan-based day-2 ops framework. The legacy planner skips its day-2
	// op blocks for opted-in clusters; bootstrap and steady-state config
	// reconciliation continue as before.
	UseNewDay2OpsAnnotation = "plan.cattle.io/use-new-day2-ops"

	// BuiltinLabel marks templates shipped by Rancher and reconciled by the
	// builtin controller. Removing this label from a template stops Rancher
	// from re-applying it, allowing operator overrides.
	BuiltinLabel = "plan.cattle.io/builtin"

	// CancellableAnnotation, when "true" on a ClusterPlanTemplate, declares
	// that operations rendered from the template can be pre-empted by an
	// incoming operation marked CancelsOthers. Mirrored into
	// OperationLifecycle.Cancellable for runtime use.
	CancellableAnnotation = "plan.cattle.io/cancellable"

	// CancelsOthersAnnotation, when "true", declares that an operation
	// rendered from the template may pre-empt a Cancellable holder of the
	// per-cluster Beacon. Mirrored into OperationLifecycle.CancelsOthers.
	CancelsOthersAnnotation = "plan.cattle.io/cancels-others"

	// ClusterPlanHashLabel records a stable hash of the rendered ClusterPlan
	// content on each NodePlan that the framework derives from it, so the
	// stage controller can detect when a ClusterPlan re-render invalidates
	// existing NodePlans.
	ClusterPlanHashLabel = "plan.cattle.io/clusterplan-hash"

	// ClusterPlanNameLabel ties a NodePlan back to the ClusterPlan that
	// produced it (when one exists). NodePlans created standalone by an
	// operator do not carry this label.
	ClusterPlanNameLabel = "plan.cattle.io/clusterplan-name"

	// ClusterPlanPoolIndexLabel records which pool index in the parent
	// ClusterPlan.Spec.Plan produced a given NodePlan.
	ClusterPlanPoolIndexLabel = "plan.cattle.io/pool-index"

	// NodeNameLabel routes a NodePlan to a specific node. The value is
	// the per-cluster-type node identifier (CAPI Machine name for
	// capr/caprke2, management.cattle.io/v3.Node name for imported).
	// Used by the registration endpoint to map an agent's identity to
	// its NodePlan, and by adapters to compute the destination of the
	// rendered wire-format payload.
	NodeNameLabel = "plan.cattle.io/node-name"

	// ClusterTypeCAPR identifies clusters provisioned by Rancher's v2prov
	// framework (provisioning.cattle.io/v1.Cluster + RKEControlPlane).
	ClusterTypeCAPR = "capr"

	// ClusterTypeCAPRKE2 identifies clusters provisioned by the upstream
	// cluster-api-provider-rke2 (RKE2ControlPlane / RKE2Bootstrap).
	ClusterTypeCAPRKE2 = "caprke2"

	// ClusterTypeImported identifies imported RKE2/K3s clusters managed only
	// through the management.cattle.io/v3.Cluster surface.
	ClusterTypeImported = "imported"
)

// Condition vocabulary used across the framework.
const (
	// ConditionReady indicates the object is healthy and progressing.
	ConditionReady = "Ready"

	// ConditionRendered indicates the rendered ClusterPlan has been written
	// from its template successfully.
	ConditionRendered = "Rendered"

	// ConditionBeaconHeld indicates the holder ClusterPlan currently holds
	// the per-cluster Beacon. Status=False with Reason=Rejected means the
	// acquisition was rejected because the Beacon is held by another op.
	ConditionBeaconHeld = "BeaconHeld"

	// ConditionNodePlansDelivered indicates all selected NodePlans have been
	// materialised and the activation selectors on the Beacon have been
	// updated so that participating system-agents can register.
	ConditionNodePlansDelivered = "NodePlansDelivered"

	// ConditionInstructionsApplied indicates every NodePlan participating in
	// the current pool has reached NodePlanPhaseSucceeded.
	ConditionInstructionsApplied = "InstructionsApplied"

	// ConditionPatchesApplied indicates the pool's after-patches have been
	// applied to all referenced resources.
	ConditionPatchesApplied = "PatchesApplied"

	// ConditionCancelled indicates the operation was pre-empted before
	// completion. Status=True is terminal; Status=False with Reason=Pending
	// indicates cancellation is in progress.
	ConditionCancelled = "Cancelled"

	// ConditionTimedOut indicates the operation exceeded its lifecycle
	// timeout while holding the Beacon.
	ConditionTimedOut = "TimedOut"
)

// ClusterReference identifies the parent cluster of a plan/beacon by GVK +
// namespace/name. The cluster-type adapter interprets this reference.
type ClusterReference struct {
	// APIVersion of the cluster object.
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`

	// Kind of the cluster object (e.g. "Cluster").
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Namespace of the cluster object. Empty for cluster-scoped clusters
	// (the imported management.cattle.io/v3.Cluster case).
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the cluster object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ObjectReference identifies an arbitrary k8s object by GVK + namespace/name.
// Used for entrypoint pinning.
type ObjectReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	Name string `json:"name,omitempty"`
}

// LocalObjectReference references a namespaced object by name in the current
// namespace.
type LocalObjectReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// LocalClusterScopedRef references a cluster-scoped object by name.
type LocalClusterScopedRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// EntrypointRef declares the GVK of a cluster-side "source of truth" object
// from which a template is rendered.
type EntrypointRef struct {
	// APIVersion is the group/version, e.g. "rke.cattle.io/v1".
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`

	// Kind is the resource kind, e.g. "RKEControlPlane".
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`
}

// OperationLifecycle declares Beacon lock semantics for operations rendered
// from a ClusterPlanTemplate.
type OperationLifecycle struct {
	// Cancellable=true means an incoming operation marked CancelsOthers may
	// pre-empt this one while it holds the Beacon.
	// +kubebuilder:default=false
	// +optional
	Cancellable bool `json:"cancellable,omitempty"`

	// CancelsOthers=true means rendering this operation may pre-empt a
	// Cancellable holder of the Beacon.
	// +kubebuilder:default=false
	// +optional
	CancelsOthers bool `json:"cancelsOthers,omitempty"`

	// TimeoutSeconds is the maximum wall-clock time the operation may hold
	// the Beacon. If exceeded, the framework forcibly clears the Beacon and
	// marks the operation Failed.
	// +kubebuilder:default=3600
	// +kubebuilder:validation:Minimum=60
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}
