package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type ClusterPlanSpec struct {
	Plan []NodePoolPlan `json:"plan,omitempty"`

	Files        []NodePoolFile        `json:"files,omitempty"`
	Probes       []NodePoolProbe       `json:"probes,omitempty"`
	Instructions []NodePoolInstruction `json:"instructions,omitempty"`
}

type NodePoolPlan struct {
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	Election *NodeElection         `json:"election,omitempty"`
	Patches  Patches               `json:"patches,omitempty"`

	NodePlanSpec `json:",inline"`
}

type Patches struct {
	Before []Patch `json:"before,omitempty"`
	After  []Patch `json:"after,omitempty"`
}

type Patch struct {
	Name        string            `json:"name,omitempty"`
	Definitions []PatchDefinition `json:"definitions,omitempty"`
}

type PatchDefinition struct {
	Selector    PatchSelector `json:"selector,omitempty"` //todo(jhyde): fix
	JSONPatches []JSONPatch   `json:"jsonPatches,omitempty"`
}

type PatchSelector struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

type JSONPatch struct {
	Op    string `json:"op,omitempty"`
	Path  string `json:"path,omitempty"`
	Value string `json:"value,omitempty"`
}

type NodeElection struct {
	TargetLabel string               `json:"targetLabel,omitempty"`
	Selector    metav1.LabelSelector `json:"selector,omitempty"`
	Criteria    []string             `json:"criteria,omitempty"`
}

type NodePoolConcurrency struct {
	Selector    []metav1.LabelSelector `json:"selector,omitempty"`
	Concurrency `json:",inline"`
}

type NodePoolFile struct {
	Selector []metav1.LabelSelector `json:"selector,omitempty"`
	File     `json:",inline"`
}

type NodePoolProbe struct {
	Selector []metav1.LabelSelector `json:"selector,omitempty"`
	Probe    `json:",inline"`
}

type NodePoolInstruction struct {
	Selector    []metav1.LabelSelector `json:"selector,omitempty"`
	Instruction `json:",inline"`
}

type ClusterPlanStatus struct {
	CurrentStep int    `json:"currentStep,omitempty"`
	Phase       string `json:"phase,omitempty"`
}

const ClusterPlanPhasePending = "Pending"
const ClusterPlanPhaseRunning = "Running"
const ClusterPlanPhaseSucceeded = "Succeeded"
const ClusterPlanPhaseFailed = "Failed"

// +genclient
// +kubebuilder:resource:path=clusterplans,scope=Namespaced
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterPlan is the Schema for the clusterplans API
type ClusterPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterPlanSpec `json:"spec,omitempty"`

	// +optional
	Status ClusterPlanStatus `json:"status,omitempty"`
}

// +genclient
// +kubebuilder:resource:path=nodeplans,scope=Namespaced
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodePlan is the Schema for the nodeplans API
type NodePlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec NodePlanSpec `json:"spec,omitempty"`

	// +optional
	Status NodePlanStatus `json:"status,omitempty"`
}

type NodePlanSpec struct {
	Files        []File        `json:"files,omitempty"`
	Instructions []Instruction `json:"instructions,omitempty"`
	Probes       []Probe       `json:"probes,omitempty"`
	Outputs      []Output      `json:"outputs,omitempty"`
}

type File struct {
	Content string `json:"content,omitempty"`

	// +optional
	// Drain determines whether the associated node should be drained when this file is changed
	Drain       bool   `json:"drain,omitempty"`
	Path        string `json:"path,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

type Instruction struct {
	Name    string   `json:"name,omitempty"`
	Command string   `json:"command,omitempty"`
	Env     []string `json:"env,omitempty"`
	Args    []string `json:"args,omitempty"`
	Image   string   `json:"image,omitempty"`

	// todo(jhyde): replace
	//PreserveStdout bool `json:"preserveStdout,omitempty"`
	//PreserveStderr bool `json:"preserveStderr,omitempty"`
	// todo(jhyde): remove
	SaveOutput bool `json:"saveOutput,omitempty"`

	Strategy InstructionStrategy `json:"strategy,omitempty"`
}

type InstructionStrategy struct {
	// How many times an action has to be run before being considered successful
	SuccessThreshold int `json:"successThreshold,omitempty"`

	// How many times an action can fail before marking the plan as failed
	FailureThreshold int `json:"failureThreshold,omitempty"`

	// If not idempotent, mark the whole plan as failed if agent exits before it finishes
	Idempotent bool `json:"retryOnSuccess,omitempty"`

	// If a command should be executed when the agent is started
	RetryOnStartup bool `json:"retryOnStartup,omitempty"`

	PeriodSeconds int `json:"periodSeconds,omitempty"`

	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type Probe struct {
	Name          string        `json:"name,omitempty"`
	RetryStrategy ProbeStrategy `json:"retryStrategy,omitempty"`

	HTTPGetAction *HTTPGetAction `json:"httpGetAction,omitempty"`
}

type ProbeStrategy struct {
	InitialDelaySeconds int `json:"initialDelaySeconds,omitempty"`

	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`

	// How many times an action has to be run before being considered successful
	SuccessThreshold int `json:"successThreshold,omitempty"`

	// How many times an action can fail before marking the plan as failed
	FailureThreshold int `json:"failureThreshold,omitempty"`
}

type HTTPGetAction struct {
	URL        string `json:"url,omitempty"`
	Insecure   bool   `json:"insecure,omitempty"`
	ClientCert string `json:"clientCert,omitempty"`
	ClientKey  string `json:"clientKey,omitempty"`
	CACert     string `json:"caCert,omitempty"`
}

type Output struct {
	Name       string `json:"name,omitempty"`
	Source     string `json:"source,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
}

type Concurrency = intstr.IntOrString

const ClusterPlanHashLabel = "plan.cattle.io/clusterplan-hash"

const NodePlanPhasePending = "Pending"
const NodePlanPhaseRunning = "Running"
const NodePlanPhaseSucceeded = "Succeeded"
const NodePlanPhaseFailed = "Failed"

type NodePlanStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	Phase string `json:"phase,omitempty"`

	Outputs map[string]OutputStatus `json:"outputs,omitempty"`
}

type OutputStatus struct {
	Value      string `json:"value,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
}
