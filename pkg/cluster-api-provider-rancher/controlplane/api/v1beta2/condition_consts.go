package v1beta2

import "github.com/rancher/wrangler/v3/pkg/condition"

const (
	RKEControlPlaneProvisionedCondition = condition.Cond("Provisioned")
	RKEControlPlaneReadyCondition       = condition.Cond("Ready")
	RKEControlPlaneStableCondition      = condition.Cond("Stable")
	RKEControlPlaneReconcileCondition   = condition.Cond("Reconciled")
)
