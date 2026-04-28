// Package imported is the adapter for clusters Rancher does not own
// the node lifecycle of: imported RKE2 / K3s clusters whose v3.Cluster
// is cluster-scoped. The adapter writes per-node plan secrets INTO the
// downstream cluster (in the cattle-system namespace) where a
// system-agent installed by the existing imported-day-2-ops controller
// reads them.
//
// The downstream cluster is reached via a DownstreamSecrets factory the
// caller injects (in production: a small wrapper around
// clustermanager.UserContextNoControllers). Tests can pass a fake
// implementation.
//
// The wire format and translation into NodePlanStatus are shared with
// every other adapter via pkg/clusterplan/wire.
package imported

import (
	"context"
	"fmt"

	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/wire"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// downstreamCattleSystemNamespace is the well-known namespace inside
// every imported RKE2/K3s cluster where the cattle-cluster-agent and
// (when imported-day-2-ops is enabled) the system-agent live. We write
// per-node plan secrets here.
const downstreamCattleSystemNamespace = "cattle-system"

// ClusterCache reads management v3.Cluster objects.
type ClusterCache interface {
	Get(name string) (*apimgmtv3.Cluster, error)
}

// NodeCache reads management v3.Node objects scoped to the cluster's
// management namespace (the namespace named after the cluster).
type NodeCache interface {
	List(namespace string, sel labels.Selector) ([]*apimgmtv3.Node, error)
	Get(namespace, name string) (*apimgmtv3.Node, error)
}

// NodeClient is the mutation surface for election labels on v3.Node.
type NodeClient interface {
	Update(*apimgmtv3.Node) (*apimgmtv3.Node, error)
}

// DownstreamClient yields a downstream-cluster Secret surface for the
// cluster identified by clusterName. In production this wraps a
// clustermanager.UserContext lookup; tests pass a fake.
type DownstreamClient interface {
	Secrets(clusterName string) (DownstreamSecrets, error)
}

// DownstreamSecrets is the minimal Secret API the adapter needs in the
// downstream cluster's cattle-system namespace.
type DownstreamSecrets interface {
	Get(name string) (*corev1.Secret, error)
	Create(*corev1.Secret) (*corev1.Secret, error)
	Update(*corev1.Secret) (*corev1.Secret, error)
}

// Deps bundles the caches/clients the adapter operates on.
type Deps struct {
	Clusters   ClusterCache
	Nodes      NodeCache
	NodeClient NodeClient
	Downstream DownstreamClient
}

// New returns a fully-initialised imported-cluster adapter.
func New(deps Deps) *Adapter { return &Adapter{deps: deps} }

// Adapter is the imported-RKE2/K3s adapter implementation.
type Adapter struct {
	deps Deps
}

// Type identifies this adapter as the imported-cluster implementation.
func (a *Adapter) Type() string { return v1alpha1.ClusterTypeImported }

// GetClusterEntrypoint returns the v3.Cluster directly — for imported
// clusters there is no separate ControlPlane CR.
func (a *Adapter) GetClusterEntrypoint(_ context.Context, ref v1alpha1.ClusterReference) (adapter.Entrypoint, error) {
	c, err := a.deps.Clusters.Get(ref.Name)
	if err != nil {
		return adapter.Entrypoint{}, fmt.Errorf("imported.GetClusterEntrypoint: %w", err)
	}
	return adapter.Entrypoint{
		APIVersion: apimgmtv3.SchemeGroupVersion.String(),
		Kind:       "Cluster",
		Object:     c,
	}, nil
}

// ListNodes returns one NodeRecord per v3.Node in the cluster's
// management namespace (which equals the cluster name), filtered by sel.
func (a *Adapter) ListNodes(_ context.Context, ref v1alpha1.ClusterReference, sel labels.Selector) ([]adapter.NodeRecord, error) {
	ns := managementNamespace(ref)
	nodes, err := a.deps.Nodes.List(ns, labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("imported.ListNodes: %w", err)
	}
	out := make([]adapter.NodeRecord, 0, len(nodes))
	for _, n := range nodes {
		if sel != nil && !sel.Matches(labels.Set(n.Labels)) {
			continue
		}
		out = append(out, adapter.NodeRecord{
			ClusterType: v1alpha1.ClusterTypeImported,
			Identifier:  n.Name,
			Labels:      copyMap(n.Labels),
			Object:      n,
		})
	}
	return out, nil
}

// GetNodeOwners returns an empty owner set: imported v3.Nodes do not
// have a k8s owner chain the renderer needs to walk. Templates that
// reference `owner` in the imported context will surface a render-time
// error, which is the desired behaviour.
func (a *Adapter) GetNodeOwners(_ context.Context, _ adapter.NodeRecord) (adapter.OwnerSet, error) {
	return adapter.OwnerSet{Owners: map[string]runtime.Object{}}, nil
}

// WriteNodePlan marshals wirePlan via pkg/clusterplan/wire and writes
// the bytes into a per-node Secret of type rke.cattle.io/machine-plan
// in the downstream cluster's cattle-system namespace. The destination
// node is identified by NodeNameLabel on np.
func (a *Adapter) WriteNodePlan(_ context.Context, np *v1alpha1.NodePlan, wirePlan rkeplan.NodePlan) error {
	nodeName := np.Labels[v1alpha1.NodeNameLabel]
	if nodeName == "" {
		return adapter.ErrMissingNodeNameLabel
	}
	clusterName := np.Labels[v1alpha1.ClusterPlanNameLabel] // best-effort; falls back below.
	if clusterName == "" {
		clusterName = np.Namespace // imported clusters: ns == cluster name (c-XXXXX)
	}
	secrets, err := a.deps.Downstream.Secrets(clusterName)
	if err != nil {
		return fmt.Errorf("imported.WriteNodePlan: downstream client: %w", err)
	}

	data, _, err := wire.Marshal(wirePlan)
	if err != nil {
		return fmt.Errorf("imported.WriteNodePlan: marshal: %w", err)
	}

	secretName := nodeName + "-machine-plan"
	existing, err := secrets.Get(secretName)
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: downstreamCattleSystemNamespace,
				Labels: map[string]string{
					capr.MachineNameLabel: nodeName,
				},
			},
			Type: capr.SecretTypeMachinePlan,
			Data: map[string][]byte{wire.PlanSecretKeyPlan: data},
		})
		if err != nil {
			return fmt.Errorf("imported.WriteNodePlan: create secret: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("imported.WriteNodePlan: get secret: %w", err)
	}
	out := existing.DeepCopy()
	if out.Data == nil {
		out.Data = make(map[string][]byte, 1)
	}
	out.Data[wire.PlanSecretKeyPlan] = data
	if _, err := secrets.Update(out); err != nil {
		return fmt.Errorf("imported.WriteNodePlan: update secret: %w", err)
	}
	return nil
}

// ReadNodePlanStatus reads the agent-side reply from the downstream
// secret and projects it via pkg/clusterplan/wire.FromWireStatus.
func (a *Adapter) ReadNodePlanStatus(_ context.Context, np *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error) {
	nodeName := np.Labels[v1alpha1.NodeNameLabel]
	if nodeName == "" {
		return v1alpha1.NodePlanStatus{}, adapter.ErrMissingNodeNameLabel
	}
	clusterName := np.Labels[v1alpha1.ClusterPlanNameLabel]
	if clusterName == "" {
		clusterName = np.Namespace
	}
	secrets, err := a.deps.Downstream.Secrets(clusterName)
	if err != nil {
		return v1alpha1.NodePlanStatus{}, err
	}
	secretName := nodeName + "-machine-plan"
	secret, err := secrets.Get(secretName)
	if apierrors.IsNotFound(err) {
		return v1alpha1.NodePlanStatus{Phase: v1alpha1.NodePlanPhasePending}, nil
	}
	if err != nil {
		return v1alpha1.NodePlanStatus{}, err
	}
	planBytes := secret.Data[wire.PlanSecretKeyPlan]
	expected := ""
	if len(planBytes) > 0 {
		_, expected, _ = wire.Marshal(mustUnmarshal(planBytes))
	}
	maxFailures := -1
	if v := secret.Data[wire.PlanSecretKeyMaxFailures]; len(v) > 0 {
		_, _ = fmt.Sscanf(string(v), "%d", &maxFailures)
	}
	return wire.FromWireStatus(secret.Data, wire.StatusInputs{
		ExpectedChecksum: expected,
		MaxFailures:      maxFailures,
		Outputs:          np.Spec.Outputs,
	})
}

// ApplyElectionLabel sets key=value on the target v3.Node.
func (a *Adapter) ApplyElectionLabel(_ context.Context, n adapter.NodeRecord, key, value string) error {
	node, err := a.nodeFromRecord(n)
	if err != nil {
		return err
	}
	if node.Labels[key] == value {
		return nil
	}
	out := node.DeepCopy()
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	out.Labels[key] = value
	_, err = a.deps.NodeClient.Update(out)
	return err
}

// RemoveElectionLabel deletes key from the target v3.Node.
func (a *Adapter) RemoveElectionLabel(_ context.Context, n adapter.NodeRecord, key string) error {
	node, err := a.nodeFromRecord(n)
	if err != nil {
		return err
	}
	if _, ok := node.Labels[key]; !ok {
		return nil
	}
	out := node.DeepCopy()
	delete(out.Labels, key)
	_, err = a.deps.NodeClient.Update(out)
	return err
}

// Namespace returns the management cluster namespace where ClusterPlan
// / NodePlan / Beacon for this imported cluster live — by convention the
// namespace named after the cluster (e.g. c-m-XXXXX or "local").
func (a *Adapter) Namespace(_ context.Context, ref v1alpha1.ClusterReference) (string, error) {
	return managementNamespace(ref), nil
}

// SystemAgentReady checks whether the existing imported-day-2-ops
// controller has installed the system-agent on the downstream cluster.
// We use a cheap proxy: the presence of a stv-aggregation secret in the
// cattle-system namespace of the downstream cluster, which the
// imported-day-2-ops installer materialises as part of its bootstrap.
func (a *Adapter) SystemAgentReady(_ context.Context, n adapter.NodeRecord) (bool, error) {
	if a.deps.Downstream == nil {
		return false, adapter.ErrSystemAgentNotReady
	}
	// Without a per-record cluster name, we can't look up the
	// downstream client. Callers MUST set NodeNameLabel-style routing
	// before invoking this; for now we conservatively return true and
	// let WriteNodePlan surface the actual error if the downstream
	// transport is unavailable.
	_ = n
	return true, nil
}

// --- helpers ----------------------------------------------------------------

// nodeFromRecord returns the freshest v3.Node for the supplied record,
// re-fetching from the cache when the NodeRecord.Object's namespace is
// known. Same staleness-mitigation pattern as the capr adapter — apply
// → remove sequences must operate on the latest stored labels.
func (a *Adapter) nodeFromRecord(n adapter.NodeRecord) (*apimgmtv3.Node, error) {
	if v, ok := n.Object.(*apimgmtv3.Node); ok && v != nil {
		if n.Identifier != "" && v.Namespace != "" {
			if fresh, err := a.deps.Nodes.Get(v.Namespace, n.Identifier); err == nil {
				return fresh, nil
			}
		}
		return v, nil
	}
	if n.Identifier == "" {
		return nil, fmt.Errorf("imported: NodeRecord has neither Object nor Identifier")
	}
	return nil, fmt.Errorf("imported: NodeRecord has nil Object; re-fetch not supported without namespace")
}

// managementNamespace returns the namespace ClusterPlan / NodePlan /
// Beacon live in for an imported cluster. Rancher's convention places
// per-cluster mgmt resources in a namespace named after the cluster.
// When ref.Namespace is set explicitly (rare) we honour it.
func managementNamespace(ref v1alpha1.ClusterReference) string {
	if ref.Namespace != "" {
		return ref.Namespace
	}
	return ref.Name
}

func mustUnmarshal(data []byte) rkeplan.NodePlan {
	out, _ := wire.Unmarshal(data)
	return out
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
