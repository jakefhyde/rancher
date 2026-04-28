// Package capr is the v2prov-flavoured adapter implementation for the
// day-2 ops framework. It speaks to:
//
//   - provisioning.cattle.io/v1.Cluster (the "cluster" surface)
//   - rke.cattle.io/v1.RKEControlPlane (the rendering entrypoint)
//   - cluster.x-k8s.io/v1beta2.Machine (the per-node source-of-truth)
//   - rke.cattle.io/v1.RKEBootstrap (the per-node owner walked by the
//     renderer's `owner` template func)
//   - corev1.Secret of type rke.cattle.io/machine-plan (the system-agent
//     transport, written and read via pkg/clusterplan/wire)
//
// All transport mutations go through pkg/clusterplan/wire so that the
// existing system-agent and the existing pkg/controllers/capr/plansecret
// reconciler continue to work unchanged.
package capr

import (
	"context"
	"fmt"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	provv1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/wire"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	capiapi "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// MachineCache is the read API the adapter needs over CAPI Machines.
// Production code passes the wrangler-generated MachineCache; tests pass
// a fake.
type MachineCache interface {
	Get(namespace, name string) (*capiapi.Machine, error)
	List(namespace string, sel labels.Selector) ([]*capiapi.Machine, error)
}

// MachineClient is the mutation surface for election labels.
type MachineClient interface {
	Update(*capiapi.Machine) (*capiapi.Machine, error)
}

// SecretClient is the get/update surface for machine-plan secrets.
type SecretClient interface {
	Get(namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error)
	Update(*corev1.Secret) (*corev1.Secret, error)
}

// RKEControlPlaneCache reads the entrypoint object.
type RKEControlPlaneCache interface {
	Get(namespace, name string) (*rkev1.RKEControlPlane, error)
}

// RKEBootstrapCache reads the owner-chain object referenced by Machine
// .spec.bootstrap.configRef.
type RKEBootstrapCache interface {
	Get(namespace, name string) (*rkev1.RKEBootstrap, error)
}

// ProvisioningClusterCache lets the adapter walk from a Cluster reference
// up to the parent provisioning Cluster (currently only used to validate
// that the provided ref resolves; a future extension may use it for
// adapter-side opt-in checks).
type ProvisioningClusterCache interface {
	Get(namespace, name string) (*provv1.Cluster, error)
}

// Deps bundles the caches/clients the adapter operates on. Constructed
// once at startup in pkg/controllers/plan/plan.go::Register and held
// for the lifetime of the process.
type Deps struct {
	Machines             MachineCache
	MachinesClient       MachineClient
	Secrets              SecretClient
	RKEControlPlanes     RKEControlPlaneCache
	RKEBootstraps        RKEBootstrapCache
	ProvisioningClusters ProvisioningClusterCache
}

// New returns a fully-initialised CAPR adapter.
func New(deps Deps) *Adapter {
	return &Adapter{deps: deps}
}

// Adapter is the v2prov-flavoured adapter implementation.
type Adapter struct {
	deps Deps
}

// Type identifies this adapter as the CAPR (v2prov) implementation.
func (a *Adapter) Type() string { return v1alpha1.ClusterTypeCAPR }

// GetClusterEntrypoint returns the RKEControlPlane that lives in the
// same namespace and with the same name as the provisioning Cluster
// referenced by ref. (Rancher's v2prov flow guarantees this 1:1 naming.)
func (a *Adapter) GetClusterEntrypoint(_ context.Context, ref v1alpha1.ClusterReference) (adapter.Entrypoint, error) {
	cp, err := a.deps.RKEControlPlanes.Get(ref.Namespace, ref.Name)
	if err != nil {
		return adapter.Entrypoint{}, fmt.Errorf("capr.GetClusterEntrypoint: %w", err)
	}
	return adapter.Entrypoint{
		APIVersion: rkev1.SchemeGroupVersion.String(),
		Kind:       "RKEControlPlane",
		Object:     cp,
	}, nil
}

// ListNodes returns one NodeRecord per CAPI Machine in the cluster's
// namespace whose cluster.x-k8s.io/cluster-name label matches the
// referenced cluster, optionally further restricted by sel.
func (a *Adapter) ListNodes(_ context.Context, ref v1alpha1.ClusterReference, sel labels.Selector) ([]adapter.NodeRecord, error) {
	clusterSel := labels.SelectorFromSet(labels.Set{capiapi.ClusterNameLabel: ref.Name})
	machines, err := a.deps.Machines.List(ref.Namespace, clusterSel)
	if err != nil {
		return nil, fmt.Errorf("capr.ListNodes: %w", err)
	}
	out := make([]adapter.NodeRecord, 0, len(machines))
	for _, m := range machines {
		if sel != nil && !sel.Matches(labels.Set(m.Labels)) {
			continue
		}
		out = append(out, adapter.NodeRecord{
			ClusterType: v1alpha1.ClusterTypeCAPR,
			Identifier:  m.Name,
			Labels:      copyMap(m.Labels),
			Object:      m,
		})
	}
	return out, nil
}

// GetNodeOwners walks Machine -> RKEBootstrap and returns the pair so
// the renderer's `owner` func can resolve "RKEBootstrap" without a
// fresh lookup.
func (a *Adapter) GetNodeOwners(_ context.Context, n adapter.NodeRecord) (adapter.OwnerSet, error) {
	machine, err := a.machineFromRecord(n)
	if err != nil {
		return adapter.OwnerSet{}, err
	}
	out := adapter.OwnerSet{Owners: map[string]runtime.Object{
		"Machine": machine,
	}}
	if bootstrap := bootstrapName(machine); bootstrap != "" {
		b, err := a.deps.RKEBootstraps.Get(machine.Namespace, bootstrap)
		if err == nil {
			out.Owners["RKEBootstrap"] = b
		}
	}
	return out, nil
}

// WriteNodePlan marshals wirePlan via pkg/clusterplan/wire and writes
// the bytes into the rke.cattle.io/machine-plan secret derived from
// the target Machine's .spec.bootstrap.configRef.name. The destination
// node is identified by NodeNameLabel on np.
func (a *Adapter) WriteNodePlan(_ context.Context, np *v1alpha1.NodePlan, wirePlan rkeplan.NodePlan) error {
	nodeName := np.Labels[v1alpha1.NodeNameLabel]
	if nodeName == "" {
		return adapter.ErrMissingNodeNameLabel
	}
	machine, err := a.deps.Machines.Get(np.Namespace, nodeName)
	if err != nil {
		return fmt.Errorf("capr.WriteNodePlan: lookup machine: %w", err)
	}
	bootstrap := bootstrapName(machine)
	if bootstrap == "" {
		return fmt.Errorf("capr.WriteNodePlan: machine %s/%s has no bootstrap configRef", np.Namespace, nodeName)
	}
	secretName := capr.PlanSecretFromBootstrapName(bootstrap)
	secret, err := a.deps.Secrets.Get(np.Namespace, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("capr.WriteNodePlan: get secret %s/%s: %w", np.Namespace, secretName, err)
	}
	if secret.Type != capr.SecretTypeMachinePlan {
		return fmt.Errorf("capr.WriteNodePlan: secret %s/%s has wrong type %q", np.Namespace, secretName, secret.Type)
	}

	data, _, err := wire.Marshal(wirePlan)
	if err != nil {
		return fmt.Errorf("capr.WriteNodePlan: marshal: %w", err)
	}

	out := secret.DeepCopy()
	if out.Data == nil {
		out.Data = make(map[string][]byte, 1)
	}
	out.Data[wire.PlanSecretKeyPlan] = data
	if _, err := a.deps.Secrets.Update(out); err != nil {
		return fmt.Errorf("capr.WriteNodePlan: update secret: %w", err)
	}
	return nil
}

// ReadNodePlanStatus reads the agent-side reply from the machine-plan
// secret and projects it via pkg/clusterplan/wire.FromWireStatus.
func (a *Adapter) ReadNodePlanStatus(_ context.Context, np *v1alpha1.NodePlan) (v1alpha1.NodePlanStatus, error) {
	nodeName := np.Labels[v1alpha1.NodeNameLabel]
	if nodeName == "" {
		return v1alpha1.NodePlanStatus{}, adapter.ErrMissingNodeNameLabel
	}
	machine, err := a.deps.Machines.Get(np.Namespace, nodeName)
	if err != nil {
		return v1alpha1.NodePlanStatus{}, err
	}
	bootstrap := bootstrapName(machine)
	if bootstrap == "" {
		return v1alpha1.NodePlanStatus{}, fmt.Errorf("capr.ReadNodePlanStatus: machine has no bootstrap configRef")
	}
	secretName := capr.PlanSecretFromBootstrapName(bootstrap)
	secret, err := a.deps.Secrets.Get(np.Namespace, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return v1alpha1.NodePlanStatus{Phase: v1alpha1.NodePlanPhasePending}, nil
		}
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

// ApplyElectionLabel sets key=value on the target CAPI Machine.
func (a *Adapter) ApplyElectionLabel(_ context.Context, n adapter.NodeRecord, key, value string) error {
	machine, err := a.machineFromRecord(n)
	if err != nil {
		return err
	}
	if machine.Labels[key] == value {
		return nil
	}
	out := machine.DeepCopy()
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	out.Labels[key] = value
	_, err = a.deps.MachinesClient.Update(out)
	return err
}

// RemoveElectionLabel deletes key from the target CAPI Machine.
func (a *Adapter) RemoveElectionLabel(_ context.Context, n adapter.NodeRecord, key string) error {
	machine, err := a.machineFromRecord(n)
	if err != nil {
		return err
	}
	if _, ok := machine.Labels[key]; !ok {
		return nil
	}
	out := machine.DeepCopy()
	delete(out.Labels, key)
	_, err = a.deps.MachinesClient.Update(out)
	return err
}

// Namespace returns the namespace where ClusterPlan / NodePlan / Beacon
// for this CAPR cluster live — the same namespace as the provisioning
// Cluster (typically fleet-default).
func (a *Adapter) Namespace(_ context.Context, ref v1alpha1.ClusterReference) (string, error) {
	return ref.Namespace, nil
}

// SystemAgentReady returns true for any v2prov-provisioned Machine: the
// system-agent is installed by the planner during bootstrap and is
// considered always-present. (When a machine is unreachable, the agent's
// inability to apply will surface as failure-count, not as not-ready.)
func (a *Adapter) SystemAgentReady(_ context.Context, _ adapter.NodeRecord) (bool, error) {
	return true, nil
}

// --- helpers ----------------------------------------------------------------

// machineFromRecord returns the freshest *capi.Machine for the supplied
// record. When the NodeRecord carries an Object we use it to learn the
// namespace and then re-fetch from the cache; this prevents apply →
// remove sequences from operating on a stale snapshot whose label set
// no longer reflects what's stored. When the cache lookup fails we
// fall back to Object as a best-effort.
func (a *Adapter) machineFromRecord(n adapter.NodeRecord) (*capiapi.Machine, error) {
	if m, ok := n.Object.(*capiapi.Machine); ok && m != nil {
		if n.Identifier != "" && m.Namespace != "" {
			if fresh, err := a.deps.Machines.Get(m.Namespace, n.Identifier); err == nil {
				return fresh, nil
			}
		}
		return m, nil
	}
	if n.Identifier == "" {
		return nil, fmt.Errorf("capr: NodeRecord has neither Object nor Identifier")
	}
	return nil, fmt.Errorf("capr: NodeRecord has nil Object; re-fetch not supported without namespace")
}

func bootstrapName(m *capiapi.Machine) string {
	ref := m.Spec.Bootstrap.ConfigRef
	if !ref.IsDefined() {
		return ""
	}
	if ref.Kind != capr.RKEBootstrapKind {
		return ""
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
