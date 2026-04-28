package render

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// ClusterReference identifies the parent cluster for templating purposes.
// Mirrors v1alpha1.ClusterReference but kept local to avoid the import cycle
// when this package is consumed by controllers that already import v1alpha1.
type ClusterReference struct {
	Type      string // "capr" | "caprke2" | "imported"
	Namespace string
	Name      string
	UID       string
}

// ClusterPlanContext is the data passed to the *outer* template render. It
// contains everything the cluster-side ClusterPlanTemplate body may
// reference via dot expressions: .Cluster, .Entrypoint, .Inputs, .Timestamp.
type ClusterPlanContext struct {
	// Cluster identifies the parent cluster.
	Cluster ClusterReference

	// Entrypoint is the cluster-side source-of-truth object the template
	// was rendered against (RKEControlPlane, RKE2ControlPlane, or v3.Cluster).
	Entrypoint runtime.Object

	// Inputs carries free-form trigger parameters (snapshot name, S3
	// bucket override, etc.).
	Inputs map[string]string

	// Timestamp is the wall-clock at which rendering began, formatted as
	// time.RFC3339. Surfaced as `.Timestamp` for use in patch values like
	// `lastTransitionTime`.
	Timestamp string
}

// NodePlanContext is the data passed to the *inner* per-node render that
// resolves Files[].Content / Instructions[].Command / Args templated
// strings. It carries everything the outer context does plus the node-of-
// truth object and the accumulated outputs of prior stages.
type NodePlanContext struct {
	Cluster     ClusterReference
	Entrypoint  runtime.Object
	Inputs      map[string]string
	Timestamp   string
	ClusterType string

	// Node is the per-node "source of truth" object. For capr/caprke2
	// this is a CAPI Machine; for imported it is a v3.Node.
	Node runtime.Object

	// NodeLabels mirrors the labels of Node for `hasRole` and selector
	// matching. Mirrored separately so the engine doesn't have to do a
	// runtime reflect against the unstructured Node every call.
	NodeLabels map[string]string

	// ElectedNode, when populated, names the node chosen by an
	// Election in the current pool, surfaced as `.ElectedNode`.
	ElectedNode string

	// Outputs is the accumulated per-node output history from prior
	// stages (and from the current stage if available). Keyed by node
	// identifier (CAPI Machine name or v3.Node name).
	Outputs map[string]NodeOutputs
}

// NodeOutputs is the captured output history for one node.
type NodeOutputs struct {
	// Labels of the producing node — used by output/shard selector
	// matching.
	Labels map[string]string

	// Values keys the captured stdout (or projected JSONPath value) by
	// Output.Name.
	Values map[string]string
}

// ElectionContext is the data passed when rendering a single
// NodeElection.Criteria expression. It is intentionally narrower than
// NodePlanContext — election runs before any stage outputs exist.
type ElectionContext struct {
	Cluster     ClusterReference
	Entrypoint  runtime.Object
	ClusterType string
	Node        runtime.Object
	NodeLabels  map[string]string
}
