package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type NodePlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodePlanSpec   `json:"spec,omitempty"`
	Status NodePlanStatus `json:"status,omitempty"`
}

type NodePlanSpec struct {
	Files []File `json:"files,omitempty"`
}

type File struct {
	Content     string `json:"content,omitempty"`
	Path        string `json:"path,omitempty"`
	Permissions string `json:"permissions,omitempty"`
}

type Instruction struct {
	Name    string   `json:"name,omitempty"`
	Command string   `json:"command,omitempty"`
	Env     []string `json:"env,omitempty"`
	Args    []string `json:"args,omitempty"`
	Image   string   `json:"image,omitempty"`

	PreserveStdout bool `json:"preserveStdout,omitempty"`
	PreserveStderr bool `json:"preserveStderr,omitempty"`

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

type NodePlanStatus struct {
}
