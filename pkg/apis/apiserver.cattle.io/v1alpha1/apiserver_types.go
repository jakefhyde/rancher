package v1alpha1

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ApiserverSpec struct {
	AgentImage              string                 `json:"agentImage"`
	AuthEndpoint            *AuthEndpoint          `json:"localClusterAuthEndpoint"`
	AgentDeploymentTemplate *appsv1.DeploymentSpec `json:"agentDeploymentTemplate"`
}

type AuthEndpoint struct {
	Enabled bool   `json:"enabled"`
	FQDN    string `json:"fqdn,omitempty"`
	CACerts string `json:"caCerts,omitempty"`
}

type ApiserverStatus struct {
	Conditions []Condition `json:"conditions,omitempty"`
	Connected  bool        `json:"connected,omitempty"`
}

// ConditionType is a valid value for Condition.Type.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=256
type ConditionType string

type Condition struct {
	// type of condition in CamelCase or in foo.example.com/CamelCase.
	// Many .condition.type values are consistent across resources like Available, but because arbitrary conditions
	// can be useful (see .node.status.conditions), the ability to deconflict is important.
	// +required
	Type ConditionType `json:"type"`

	// status of the condition, one of True, False, Unknown.
	// +required
	Status corev1.ConditionStatus `json:"status"`

	// lastTransitionTime is the last time the condition transitioned from one status to another.
	// This should be when the underlying condition changed. If that is not known, then using the time when
	// the API field changed is acceptable.
	// +required
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// reason is the reason for the condition's last transition in CamelCase.
	// The specific API may choose whether or not this field is considered a guaranteed API.
	// This field may be empty.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Reason string `json:"reason,omitempty"`

	// message is a human readable message indicating details about the transition.
	// This field may be empty.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=10240
	Message string `json:"message,omitempty"`
}

type Apiserver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApiserverSpec   `json:"spec"`
	Status ApiserverStatus `json:"status,omitempty"`
}
