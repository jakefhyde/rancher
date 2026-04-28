package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +kubebuilder:resource:path=nodeplans,scope=Namespaced,shortName=np
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodePlan is the unit of work executed by a single system-agent on a single
// node. It carries Files to write, Instructions to run, Probes to evaluate,
// and Outputs to capture. NodePlan is the source of truth — the legacy
// rke.cattle.io/machine-plan secret format is a (temporary) wire adapter
// produced from NodePlanSpec by pkg/clusterplan/wire and consumed by the
// existing system-agent until system-agents learn to read NodePlan directly
// (see the registration redesign in the design plan).
//
// NodePlans usually descend from a ClusterPlan (linked via the
// plan.cattle.io/clusterplan-name label) but a NodePlan is valid standalone:
// an operator may create one directly, and the framework treats it as a
// self-contained unit of work without any required parent.
type NodePlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec NodePlanSpec `json:"spec,omitempty"`

	// +optional
	Status NodePlanStatus `json:"status,omitempty"`
}

// NodePlanSpec is the desired on-node state.
type NodePlanSpec struct {
	// Files are written to the node filesystem before Instructions run.
	// +optional
	Files []File `json:"files,omitempty"`

	// Instructions are executed in order.
	// +optional
	Instructions []Instruction `json:"instructions,omitempty"`

	// Probes are evaluated periodically and gate Instruction execution
	// according to each instruction's strategy.
	// +optional
	Probes []Probe `json:"probes,omitempty"`

	// Outputs declare which Instruction stdouts to capture and surface in
	// Status.Outputs. The capture itself is performed by the agent; the
	// framework consumes them via Status.
	// +optional
	Outputs []Output `json:"outputs,omitempty"`
}

// File is written by the system-agent to the node filesystem.
type File struct {
	// Content of the file. May be base64-encoded by the agent transport for
	// binary safety; templating in the calling layer takes care of encoding.
	Content string `json:"content,omitempty"`

	// Drain=true causes the system-agent to drain the node before applying
	// the file change. Useful for kubelet config, container runtime config,
	// and other host-level mutations whose visibility requires a restart.
	// +optional
	Drain bool `json:"drain,omitempty"`

	// Path is the absolute filesystem path on the node.
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path,omitempty"`

	// Permissions in octal string form, e.g. "0644".
	// +optional
	Permissions string `json:"permissions,omitempty"`
}

// Instruction is one shell command executed by the system-agent.
type Instruction struct {
	// Name is a human-readable identifier referenced by Output.Source.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Command is the executable to run.
	// +kubebuilder:validation:MinLength=1
	Command string `json:"command,omitempty"`

	// Env is a list of "KEY=VALUE" strings.
	// +optional
	Env []string `json:"env,omitempty"`

	// Args are arguments passed to Command.
	// +optional
	Args []string `json:"args,omitempty"`

	// Image, when set, runs the command inside the named container image.
	// When empty, the command runs on the host.
	// +optional
	Image string `json:"image,omitempty"`

	// SaveOutput causes stdout to be retained by the agent so an Output
	// declaration can capture it. Will be removed once preserveStdout /
	// preserveStderr are wired through.
	// +optional
	SaveOutput bool `json:"saveOutput,omitempty"`

	// Strategy controls retries, idempotency, and scheduling.
	// +optional
	Strategy InstructionStrategy `json:"strategy,omitempty"`
}

// InstructionStrategy controls how the agent runs an Instruction.
type InstructionStrategy struct {
	// SuccessThreshold is the number of consecutive successful runs
	// required before the instruction is considered done.
	// +optional
	SuccessThreshold int `json:"successThreshold,omitempty"`

	// FailureThreshold is the number of consecutive failures after which
	// the parent NodePlan is marked Failed.
	// +optional
	FailureThreshold int `json:"failureThreshold,omitempty"`

	// Idempotent indicates the instruction may be safely re-run; when
	// false, an agent restart that interrupts the instruction will mark
	// the plan Failed rather than re-running.
	// +optional
	Idempotent bool `json:"retryOnSuccess,omitempty"`

	// RetryOnStartup re-runs the instruction every time the system-agent
	// process starts.
	// +optional
	RetryOnStartup bool `json:"retryOnStartup,omitempty"`

	// PeriodSeconds, when > 0, runs the instruction on a recurring
	// schedule rather than once.
	// +optional
	PeriodSeconds int `json:"periodSeconds,omitempty"`

	// TimeoutSeconds bounds a single execution attempt.
	// +optional
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// Probe is a periodic readiness check evaluated by the system-agent.
type Probe struct {
	// Name identifies the probe and is referenced by Output and by
	// instruction-status conditions.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// RetryStrategy controls evaluation cadence and the success/failure
	// thresholds that flip the probe's reported status.
	// +optional
	RetryStrategy ProbeStrategy `json:"retryStrategy,omitempty"`

	// HTTPGetAction defines the HTTP probe. Future kinds (TCPSocket,
	// Exec) may be added alongside.
	// +optional
	HTTPGetAction *HTTPGetAction `json:"httpGetAction,omitempty"`
}

// ProbeStrategy parameters mirror corev1.Probe semantics.
type ProbeStrategy struct {
	// InitialDelaySeconds delays the first evaluation.
	// +optional
	InitialDelaySeconds int `json:"initialDelaySeconds,omitempty"`

	// TimeoutSeconds bounds a single probe attempt.
	// +optional
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`

	// SuccessThreshold is the consecutive successes required to flip
	// the probe to Ready.
	// +optional
	SuccessThreshold int `json:"successThreshold,omitempty"`

	// FailureThreshold is the consecutive failures required to flip the
	// probe to NotReady.
	// +optional
	FailureThreshold int `json:"failureThreshold,omitempty"`
}

// HTTPGetAction is an HTTP probe target.
type HTTPGetAction struct {
	// URL of the probe target.
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url,omitempty"`

	// Insecure skips TLS verification.
	// +optional
	Insecure bool `json:"insecure,omitempty"`

	// ClientCert is a PEM-encoded client certificate (or a path on the
	// node, agent-dependent).
	// +optional
	ClientCert string `json:"clientCert,omitempty"`

	// ClientKey is a PEM-encoded client key (or a path on the node).
	// +optional
	ClientKey string `json:"clientKey,omitempty"`

	// CACert is a PEM-encoded CA certificate (or a path on the node).
	// +optional
	CACert string `json:"caCert,omitempty"`
}

// Output declares a value to surface in NodePlanStatus.Outputs.
type Output struct {
	// Name is the key under which the captured value is published.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`

	// Source identifies the producer. The framework recognises the form
	// "<instructionName>" (capture instruction stdout) and
	// "<instructionName>:<jsonPath>" (capture and project as JSON).
	// +kubebuilder:validation:MinLength=1
	Source string `json:"source,omitempty"`

	// Persistent=true keeps the output across the lifetime of the
	// NodePlan and (when this NodePlan descends from a ClusterPlan)
	// promotes it into the parent ClusterPlan's persistent outputs map.
	// +optional
	Persistent bool `json:"persistent,omitempty"`
}

// NodePlanStatus is the observed state of a NodePlan.
type NodePlanStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase mirrors the high-level lifecycle.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions surface fine-grained progress.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Outputs holds captured values keyed by Output.Name.
	// +optional
	Outputs map[string]OutputStatus `json:"outputs,omitempty"`
}

// OutputStatus is one captured value.
type OutputStatus struct {
	// Value is the captured (and optionally JSONPath-projected) string.
	Value string `json:"value,omitempty"`

	// Persistent mirrors Output.Persistent so consumers don't need to
	// look back at the spec.
	// +optional
	Persistent bool `json:"persistent,omitempty"`
}

// NodePlan phase constants are plain string consts so unknown future phases
// can be tolerated by the API server without an enum migration.
const (
	NodePlanPhasePending   = "Pending"
	NodePlanPhaseRunning   = "Running"
	NodePlanPhaseSucceeded = "Succeeded"
	NodePlanPhaseFailed    = "Failed"
)
