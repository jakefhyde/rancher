package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// +genclient
// +kubebuilder:resource:path=beacons,scope=Namespaced,shortName=bcn
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Operation",type=string,JSONPath=".status.holder.operation"
// +kubebuilder:printcolumn:name="Holder",type=string,JSONPath=".status.holder.name.name"
// +kubebuilder:printcolumn:name="Acquired",type=date,JSONPath=".status.holder.acquiredAt"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Beacon is the per-cluster lock used to serialise day-2 operations and the
// signal system-agents listen on to know when to participate.
//
// One Beacon exists per cluster, named after the cluster, in the same
// namespace as that cluster's ClusterPlans.
//
// Acquisition follows a single-slot mailbox pattern: the trigger controller
// writes Spec.Acquisition; the beacon controller reads it, decides accept
// or reject (or pre-empt for cancelsOthers requests), and clears the slot.
//
// While held, Status.Active is true and Status.ActiveSelectors enumerates
// the node label selectors of every NodePoolPlan that the holder ClusterPlan
// will (or did) create work for. System-agents on each node continuously
// watch their cluster's Beacon: when ActiveSelectors changes such that any
// selector matches the agent's labels, the agent calls the registration
// endpoint (see Status.RegistrationEndpoint) and receives a JSON payload
// containing its NodePlan name and a kubeconfig narrowly scoped to watch
// that NodePlan. Workers stay idle (no NodePlan, no kubeconfig, no RBAC)
// during operations whose selectors do not match them — the etcd-snapshot
// case where workers do nothing is the motivating example.
type Beacon struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec BeaconSpec `json:"spec,omitempty"`

	// +optional
	Status BeaconStatus `json:"status,omitempty"`
}

// BeaconSpec is the desired state of a Beacon.
type BeaconSpec struct {
	// ClusterRef identifies the cluster this Beacon serialises operations
	// for.
	ClusterRef ClusterReference `json:"clusterRef"`

	// Acquisition is the single-slot mailbox: the trigger controller
	// writes a request here when a new ClusterPlan needs the Beacon.
	// The beacon controller clears this field once the request has been
	// accepted (Free → Acquired or Acquired → Cancelling) or rejected.
	// +optional
	Acquisition *BeaconAcquisitionRequest `json:"acquisition,omitempty"`
}

// BeaconAcquisitionRequest is a request to take the Beacon for a specific
// ClusterPlan.
type BeaconAcquisitionRequest struct {
	// Operation is the requesting plan's operation name (e.g.
	// "etcd-snapshot-restore").
	// +kubebuilder:validation:MinLength=1
	Operation string `json:"operation"`

	// ClusterPlan references the requesting plan in the Beacon's namespace.
	ClusterPlan LocalObjectReference `json:"clusterPlan"`

	// UID of the requesting ClusterPlan, used to disambiguate against a
	// stale request after delete-and-recreate.
	UID types.UID `json:"uid"`

	// Lifecycle copied from the requesting plan so the beacon controller
	// can decide accept vs pre-empt without fetching the ClusterPlan.
	Lifecycle OperationLifecycle `json:"lifecycle"`

	// RequestedAt is when the trigger controller wrote the request.
	RequestedAt metav1.Time `json:"requestedAt"`
}

// BeaconStatus is the observed state of a Beacon.
type BeaconStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// State is the high-level lock state.
	// +optional
	State BeaconState `json:"state,omitempty"`

	// Active mirrors State == Acquired || State == Cancelling. Surfaced
	// as a top-level boolean for system-agent watchers that don't want to
	// parse the state enum.
	// +optional
	Active bool `json:"active,omitempty"`

	// ActiveSelectors enumerates the label selectors describing the node
	// set(s) involved in the holder ClusterPlan. A system-agent on a
	// node whose labels match any selector is expected to register and
	// pick up a NodePlan; agents on non-matching nodes stay idle.
	//
	// When State is Free, ActiveSelectors is empty.
	// +optional
	ActiveSelectors []metav1.LabelSelector `json:"activeSelectors,omitempty"`

	// RegistrationEndpoint is the absolute URL system-agents POST to in
	// order to receive their NodePlan name and a narrowly-scoped
	// kubeconfig. Populated by Rancher at startup; included on the
	// Beacon so agents need only know how to find their Beacon.
	// +optional
	RegistrationEndpoint string `json:"registrationEndpoint,omitempty"`

	// Holder is the ClusterPlan currently holding the Beacon, when State
	// is Acquired or Cancelling.
	// +optional
	Holder *BeaconHolder `json:"holder,omitempty"`

	// Cancellation, when set, describes the in-progress pre-emption: the
	// Holder is being unwound to make way for the named preempting plan.
	// +optional
	Cancellation *BeaconCancellation `json:"cancellation,omitempty"`

	// Conditions: Ready, Held, CancellationInProgress.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// BeaconHolder records the current owner of the lock.
type BeaconHolder struct {
	// Operation name of the holder plan.
	// +kubebuilder:validation:MinLength=1
	Operation string `json:"operation"`

	// Name of the holder ClusterPlan in the Beacon's namespace.
	Name LocalObjectReference `json:"name"`

	// UID of the holder ClusterPlan.
	UID types.UID `json:"uid"`

	// Cancellable mirrors Lifecycle.Cancellable so the beacon controller
	// can decide pre-emption without fetching the holder plan.
	// +optional
	Cancellable bool `json:"cancellable,omitempty"`

	// AcquiredAt is when the lock transitioned Free → Acquired.
	AcquiredAt metav1.Time `json:"acquiredAt"`

	// DeadlineAt is the wall-clock deadline (AcquiredAt +
	// Lifecycle.TimeoutSeconds). After this point a periodic sweep
	// forcibly clears the holder and marks it Failed.
	DeadlineAt metav1.Time `json:"deadlineAt"`
}

// BeaconCancellation records an in-progress pre-emption.
type BeaconCancellation struct {
	// PreemptingOperation name (e.g. "etcd-snapshot-restore").
	// +kubebuilder:validation:MinLength=1
	PreemptingOperation string `json:"preemptingOperation"`

	// PreemptingPlan reference in the Beacon's namespace.
	PreemptingPlan LocalObjectReference `json:"preemptingPlan"`

	// StartedAt is when the cancellation began.
	StartedAt metav1.Time `json:"startedAt"`
}

// BeaconState is the observable lock state.
// +kubebuilder:validation:Enum=Free;Acquired;Cancelling
type BeaconState string

const (
	// BeaconStateFree: no holder, ready to accept an acquisition.
	BeaconStateFree BeaconState = "Free"

	// BeaconStateAcquired: a ClusterPlan holds the Beacon and is executing.
	BeaconStateAcquired BeaconState = "Acquired"

	// BeaconStateCancelling: the holder has been pre-empted and is
	// unwinding; once it transitions to Cancelled the Beacon will accept
	// the pending preempting acquisition.
	BeaconStateCancelling BeaconState = "Cancelling"
)
