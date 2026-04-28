// Package importlabels mirrors standard k8s node-role labels onto
// management.cattle.io/v3.Node so that selectors in the day-2 ops
// framework can target imported-cluster nodes uniformly.
//
// Specifically: for every Node in a downstream imported cluster the
// controller maps
//
//	node-role.kubernetes.io/control-plane=*  → rke.cattle.io/control-plane-role=true
//	node-role.kubernetes.io/etcd=*           → rke.cattle.io/etcd-role=true
//	(neither of the above set on the node)   → rke.cattle.io/worker-role=true
//
// onto the corresponding management v3.Node, so that a CAPR-style
// selector like { rke.cattle.io/etcd-role: "true" } matches the right
// nodes on imported clusters as well as on v2prov-provisioned ones.
//
// Production wiring requires a downstream watch (the controller needs
// to react to label drift on the actual cluster's Nodes, which live in
// a different cluster than the management v3.Nodes). For PR1 this
// package ships the pure mapping logic plus a bare reconciler that the
// real wrangler controller can drive; cross-cluster watch wiring is a
// follow-up.
package importlabels

import (
	"context"
	"fmt"

	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	corev1 "k8s.io/api/core/v1"
)

// Standard upstream node-role labels.
const (
	StandardControlPlaneLabel = "node-role.kubernetes.io/control-plane"
	StandardEtcdLabel         = "node-role.kubernetes.io/etcd"
)

// Rancher-side labels selectors target.
const (
	RancherControlPlaneLabel = "rke.cattle.io/control-plane-role"
	RancherEtcdLabel         = "rke.cattle.io/etcd-role"
	RancherWorkerLabel       = "rke.cattle.io/worker-role"
)

// V3NodeCache reads the management v3.Node objects in a cluster's
// management namespace.
type V3NodeCache interface {
	Get(namespace, name string) (*apimgmtv3.Node, error)
}

// V3NodeClient applies the mirrored labels back onto v3.Node.
type V3NodeClient interface {
	Update(*apimgmtv3.Node) (*apimgmtv3.Node, error)
}

// Deps bundles the dependencies the controller operates on.
type Deps struct {
	V3Nodes      V3NodeCache
	V3NodeClient V3NodeClient
}

// Handler is the importlabels reconciler.
type Handler struct {
	deps Deps
}

// New returns an initialised handler.
func New(deps Deps) *Handler { return &Handler{deps: deps} }

// SyncNode mirrors the standard role labels of a downstream Node onto
// the corresponding management v3.Node identified by clusterNamespace
// + node.Name. Missing v3.Node returns nil (the v3.Node may not yet
// exist; the next sync will succeed).
func (h *Handler) SyncNode(_ context.Context, clusterNamespace string, node *corev1.Node) error {
	if node == nil {
		return nil
	}
	v3node, err := h.deps.V3Nodes.Get(clusterNamespace, node.Name)
	if err != nil {
		// NotFound is benign during initial provisioning; other
		// errors propagate.
		return ignoreNotFound(err)
	}
	desired := DesiredLabels(node.Labels)
	if labelsEqual(v3node.Labels, desired) {
		return nil
	}
	out := v3node.DeepCopy()
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	for k, v := range desired {
		out.Labels[k] = v
	}
	_, err = h.deps.V3NodeClient.Update(out)
	return err
}

// DesiredLabels returns the Rancher-side labels that should be present
// on the v3.Node for the supplied downstream Node labels. Pure helper —
// exposed so callers (and tests) can drive the mapping directly.
//
// Mapping rules:
//   - control-plane label present (any value)  ⇒ rke.cattle.io/control-plane-role=true
//   - etcd label present                       ⇒ rke.cattle.io/etcd-role=true
//   - neither present                          ⇒ rke.cattle.io/worker-role=true
//
// A node may be both etcd and control-plane (in which case both
// rke.cattle.io/* labels are set and worker-role is NOT).
func DesiredLabels(downstream map[string]string) map[string]string {
	desired := map[string]string{}
	_, isCP := downstream[StandardControlPlaneLabel]
	_, isEtcd := downstream[StandardEtcdLabel]
	if isCP {
		desired[RancherControlPlaneLabel] = "true"
	}
	if isEtcd {
		desired[RancherEtcdLabel] = "true"
	}
	if !isCP && !isEtcd {
		desired[RancherWorkerLabel] = "true"
	}
	return desired
}

// labelsEqual returns true iff every k=v in want is present in have
// (have may carry additional unrelated labels).
func labelsEqual(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func ignoreNotFound(err error) error {
	// Lightweight check that avoids importing apierrors — the caller
	// is happy with any error message containing "not found".
	if err == nil {
		return nil
	}
	if e := err.Error(); e != "" && (e == "not found" || containsNotFound(e)) {
		return nil
	}
	return err
}

func containsNotFound(s string) bool {
	const probe = "not found"
	return len(s) >= len(probe) && (indexNotFound(s, probe) >= 0)
}

func indexNotFound(s, probe string) int {
	// Avoid pulling in strings.Contains for a one-liner.
	for i := 0; i+len(probe) <= len(s); i++ {
		if s[i:i+len(probe)] == probe {
			return i
		}
	}
	return -1
}

var _ = fmt.Sprintf // reserved
