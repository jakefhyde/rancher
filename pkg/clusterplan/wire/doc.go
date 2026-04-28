// Package wire is the temporary projection bridge between the new
// plan.cattle.io/v1alpha1.NodePlanSpec CRD shape and the existing
// system-agent wire format defined by pkg/apis/rke.cattle.io/v1/plan.
//
// The system-agent today reads a JSON-marshalled plan.NodePlan from the
// "plan" key of an rke.cattle.io/machine-plan Secret, applies it, and
// writes status (applied-checksum, applied-output, probe-statuses,
// failure-count) back into the same secret. This package lets the new
// framework participate in that flow without changing the agent: every
// adapter (capr, caprke2, imported) calls ToWire to produce the
// machine-plan secret payload and FromWireStatus to project the agent's
// reply back into a NodePlanStatus.
//
// The package will be deleted in PR6 once the system-agent learns to
// watch its NodePlan natively via the kubeconfig minted by the
// registration endpoint. Anything load-bearing on this package's API
// shape should call out the temporariness in its godoc.
package wire
