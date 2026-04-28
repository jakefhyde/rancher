package adapter

import (
	"context"
	"errors"
	"sync"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Entrypoint is the cluster-side source-of-truth object the templating
// engine renders against. Carries the originating GVK alongside the
// object so the renderer can pin EntrypointRef on the produced
// ClusterPlan without a second round-trip.
type Entrypoint struct {
	APIVersion string
	Kind       string
	Object     runtime.Object
}

// NodeRecord identifies one node in a cluster-type-specific way and
// carries enough metadata for selector matching without re-fetching.
//
// For CAPR/CAPRKE2 clusters Identifier is the CAPI Machine name; for
// imported clusters it is the management.cattle.io/v3.Node name. Object
// MAY be nil — adapters that prefer lazy fetching are free to populate
// it on demand inside their other methods.
type NodeRecord struct {
	// ClusterType is one of v1alpha1.ClusterType{CAPR,CAPRKE2,Imported}.
	ClusterType string

	// Identifier is the per-type node name.
	Identifier string

	// Labels are the labels mirrored from the node-of-truth object —
	// for capr/caprke2 the CAPI Machine labels, for imported the
	// rke.cattle.io/* labels mirrored onto v3.Node by the
	// importlabels controller.
	Labels map[string]string

	// Object is the underlying node-of-truth object.
	Object runtime.Object
}

// OwnerSet bundles the per-cluster-type owner-chain objects keyed by
// Kind, so the renderer's `owner` template func can resolve them
// without a fresh round-trip. Returned by GetNodeOwners.
//
// For capr the chain is {Machine, RKEBootstrap}; for caprke2
// {Machine, RKE2Bootstrap}; for imported the set is empty.
type OwnerSet struct {
	Owners map[string]runtime.Object
}

// Adapter abstracts the per-cluster-type concerns of the framework.
type Adapter interface {
	// Type returns the cluster-type discriminator ("capr", "caprke2",
	// "imported"). Adapters are looked up by Type() in the Registry.
	Type() string

	// GetClusterEntrypoint resolves the cluster-side source-of-truth
	// object the ClusterPlanTemplate is rendered against.
	GetClusterEntrypoint(ctx context.Context, ref v1alpha1.ClusterReference) (Entrypoint, error)

	// ListNodes returns NodeRecords for every node in the cluster,
	// optionally filtered by sel. A nil/empty selector matches all.
	ListNodes(ctx context.Context, ref v1alpha1.ClusterReference, sel labels.Selector) ([]NodeRecord, error)

	// GetNodeOwners returns the cluster-type-specific owner chain for
	// the supplied node. Used by the renderer's `owner` template func.
	GetNodeOwners(ctx context.Context, n NodeRecord) (OwnerSet, error)

	// WriteNodePlan delivers the rendered wire-format NodePlan to the
	// system-agent transport. Implementations marshal via
	// pkg/clusterplan/wire and place the bytes wherever their cluster
	// type expects (a Secret in the management cluster for capr/caprke2,
	// a Secret in the downstream cluster for imported).
	//
	// The np parameter MUST carry a NodeNameLabel identifying the
	// destination node; ErrMissingNodeNameLabel is returned otherwise.
	WriteNodePlan(ctx context.Context, np *v1alpha1.NodePlan, wirePlan rkeplan.NodePlan) error

	// ReadNodePlanStatus reads the agent's reply (applied-checksum,
	// applied-output, failure-count, probe-statuses) from the
	// per-node transport and projects it into a typed status via
	// pkg/clusterplan/wire.FromWireStatus.
	ReadNodePlanStatus(ctx context.Context, np *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error)

	// ApplyElectionLabel sets key=value on the node-of-truth object so
	// other selectors can target the elected node.
	ApplyElectionLabel(ctx context.Context, n NodeRecord, key, value string) error

	// RemoveElectionLabel removes the supplied key from the
	// node-of-truth object. Used when an election is invalidated.
	RemoveElectionLabel(ctx context.Context, n NodeRecord, key string) error

	// Namespace returns the namespace where ClusterPlan / NodePlan /
	// Beacon resources for this cluster live: typically fleet-default
	// for capr/caprke2 and the management cluster namespace c-XXXXX
	// for imported.
	Namespace(ctx context.Context, ref v1alpha1.ClusterReference) (string, error)

	// SystemAgentReady reports whether a system-agent capable of
	// applying NodePlans is present and reachable on the supplied node.
	// For capr/caprke2 the agent is provisioned by Rancher itself and
	// is generally always ready once the node is bootstrapped. For
	// imported clusters the agent is installed asynchronously by the
	// existing imported-day-2-ops controller and may not be present
	// when the framework first looks; in that case the trigger
	// controller surfaces a friendly status condition without
	// acquiring the Beacon.
	SystemAgentReady(ctx context.Context, n NodeRecord) (bool, error)
}

// Registry holds adapters keyed by Type(). Implementations must be
// concurrency-safe for read; Register is expected to be called only
// during startup.
type Registry interface {
	Get(clusterType string) (Adapter, bool)
	Register(a Adapter)
}

// NewRegistry returns an in-memory Registry. The default implementation
// is safe for concurrent reads after all Registers have completed.
func NewRegistry() Registry { return &mapRegistry{adapters: map[string]Adapter{}} }

type mapRegistry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func (r *mapRegistry) Get(clusterType string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[clusterType]
	return a, ok
}

func (r *mapRegistry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Type()] = a
}

// DetermineClusterType maps a ClusterReference to one of the
// v1alpha1.ClusterType{CAPR,CAPRKE2,Imported} constants based on the
// referenced object's GVK. Returns "" when the GVK is not recognised.
//
// Lives on the adapter package because it's the lookup key the
// Registry uses; controllers and trigger reconcilers consume it
// uniformly.
func DetermineClusterType(ref v1alpha1.ClusterReference) string {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return ""
	}
	switch {
	case gv.Group == "management.cattle.io" && ref.Kind == "Cluster":
		return v1alpha1.ClusterTypeImported
	case gv.Group == "controlplane.cluster.x-k8s.io" && ref.Kind == "RKE2ControlPlane":
		return v1alpha1.ClusterTypeCAPRKE2
	case gv.Group == "provisioning.cattle.io" && ref.Kind == "Cluster":
		return v1alpha1.ClusterTypeCAPR
	}
	return ""
}

// Sentinel errors callers MAY surface as conditions on the relevant
// ClusterPlan / NodePlan / Beacon / ETCDSnapshotCreate.
var (
	// ErrNotImplemented is returned by adapter methods that have not
	// yet been wired up. Currently the entire caprke2 adapter and a
	// few imported-cluster operations beyond etcd-snapshot.
	ErrNotImplemented = errors.New("adapter: not implemented")

	// ErrMissingNodeNameLabel is returned by WriteNodePlan / similar
	// methods when np.Labels[v1alpha1.NodeNameLabel] is empty. The
	// stage controller is responsible for setting this label when it
	// emits a NodePlan; standalone-authored NodePlans must set it
	// themselves.
	ErrMissingNodeNameLabel = errors.New("adapter: NodePlan is missing the plan.cattle.io/node-name label")

	// ErrSystemAgentNotReady is returned by ReadNodePlanStatus /
	// WriteNodePlan when the destination node has no reachable
	// system-agent yet. Used to defer rather than fail.
	ErrSystemAgentNotReady = errors.New("adapter: system-agent not ready on node")

	// ErrUnknownClusterType is returned by the Registry when no
	// adapter is registered for the requested ClusterType.
	ErrUnknownClusterType = errors.New("adapter: no adapter registered for cluster type")
)
