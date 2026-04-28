package v1alpha1

import (
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +kubebuilder:resource:path=etcdsnapshotcreates,scope=Namespaced,shortName=esc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=".spec.clusterRef.name"
// +kubebuilder:printcolumn:name="Plan",type=string,JSONPath=".status.clusterPlanRef.name"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ETCDSnapshotCreate is the per-operation trigger CR for the
// "etcd-snapshot-create" day-2 operation. Reconciled by
// pkg/controllers/plan/etcdsnapshot, which selects the per-cluster-type
// ClusterPlanTemplate (etcd-snapshot-create-{capr,caprke2,imported}),
// materialises a ClusterPlan from it, and records the resulting
// ClusterPlan reference in Status.ClusterPlanRef.
//
// The CR is the single trigger surface for all three cluster types in
// the new framework. Legacy paths (the
// RKEControlPlane.Spec.ETCDSnapshotCreate.Generation bump for v2prov
// clusters) are translated into ETCDSnapshotCreate CRs by
// pkg/controllers/plan/trigger when the parent cluster is opted-in.
//
// Once the materialised ClusterPlan reaches a terminal phase the CR
// itself can be garbage-collected (the Status.ClusterPlanRef remains a
// useful audit trail until then).
type ETCDSnapshotCreate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ETCDSnapshotCreateSpec `json:"spec,omitempty"`

	// +optional
	Status ETCDSnapshotCreateStatus `json:"status,omitempty"`
}

// ETCDSnapshotCreateSpec is the desired state of an ETCDSnapshotCreate.
type ETCDSnapshotCreateSpec struct {
	// ClusterRef identifies the cluster the snapshot will be taken on.
	ClusterRef ClusterReference `json:"clusterRef"`

	// Name is the desired snapshot name. When empty, the framework
	// generates one of the form "<cluster>-<timestamp>".
	// +optional
	Name string `json:"name,omitempty"`

	// S3, when set, configures S3-backed snapshot upload using the same
	// shape as rke.cattle.io/v1.ETCDSnapshotS3.
	// +optional
	S3 *rkev1.ETCDSnapshotS3 `json:"s3,omitempty"`
}

// ETCDSnapshotCreateStatus is the observed state of an ETCDSnapshotCreate.
type ETCDSnapshotCreateStatus struct {
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ClusterPlanRef points at the ClusterPlan materialised for this
	// trigger, in the same namespace.
	// +optional
	ClusterPlanRef *LocalObjectReference `json:"clusterPlanRef,omitempty"`

	// Conditions surface the trigger's progress: Ready (the trigger is
	// well-formed and a ClusterPlan exists for it), BeaconHeld
	// (whether the materialised plan currently holds the Beacon).
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
