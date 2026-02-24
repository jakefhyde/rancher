package planner

import (
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/controllers/capr/managesystemagent"
	"github.com/rancher/wrangler/v3/pkg/merr"
	"k8s.io/apimachinery/pkg/api/equality"
)

func (p *Planner) runEtcdSnapshotCreate(clusterPlan *plan.Plan, runtimeCommand, version string) []error {
	servers := collect(clusterPlan, isEtcd)
	if len(servers) == 0 {
		return []error{errors.New("failed to find node to perform etcd snapshot")}
	}

	var errs []error

	for _, server := range servers {
		createPlan, err := p.generateEtcdSnapshotCreatePlan(runtimeCommand, version)
		if err != nil {
			return []error{err}
		}
		// todo(jhyde): remove reference to machine, instead use machine secret
		msg := fmt.Sprintf("etcd snapshot on machine %s/%s", server.Machine.Namespace, server.Machine.Name)
		if err = assignAndCheckPlan(p.store, msg, server, createPlan, "", 3, 3); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// generateEtcdSnapshotCreatePlan generates a plan that contains an instruction to create an etcd snapshot.
func (p *Planner) generateEtcdSnapshotCreatePlan(runtimeCommand, version string) (plan.NodePlan, error) {
	v, err := semver.NewVersion(version)
	if err != nil {
		return plan.NodePlan{}, err
	}

	args := []string{
		"etcd-snapshot",
	}

	// Starting in v1.26, we must specify "save" when creating an etcd snapshot
	if v.GreaterThan(managesystemagent.Kubernetes125) {
		args = append(args, "save")
	}

	return plan.NodePlan{
		Instructions: []plan.OneTimeInstruction{
			{
				Name:    "create",
				Command: runtimeCommand,
				Args:    args,
			},
		},
	}, err
}

// todo(jhyde): ensure cluster does not drain if previously applied plan was not a day2 op.
func (p *Planner) createEtcdSnapshot(_ *rkev1.ETCDSnapshotCreate, runtimeCommand, version string, phase rkev1.ETCDSnapshotPhase, clusterPlan *plan.Plan) (rkev1.ETCDSnapshotPhase, error) {
	// todo(jhyde): move to webhook
	//// Don't create an etcd snapshot if the cluster is not initialized or bootstrapped.
	//if !ptr.Deref(status.Initialization.ControlPlaneInitialized, false) || !capr.Bootstrapped.IsTrue(&status) {
	//	logrus.Warnf("[planner] rkecluster %s/%s: skipping etcd snapshot creation as cluster has not yet been initialized or bootstrapped", controlPlane.Namespace, controlPlane.Name)
	//	return status, nil
	//}

	switch phase {
	case rkev1.ETCDSnapshotPhaseStarted:
		var stateSet bool
		var finErrs []error
		// todo(jhyde): make precondition that cluster is healthy
		//found, joinServer, _, err := p.findInitNode(controlPlane, clusterPlan)
		//if err != nil {
		//	logrus.Errorf("[planner] rkecluster %s/%s: error encountered while searching for init node during etcd snapshot creation: %v", controlPlane.Namespace, controlPlane.Name, err)
		//	return status, err
		//}
		//if !found || joinServer == "" {
		//	logrus.Warnf("[planner] rkecluster %s/%s: skipping etcd snapshot creation as cluster does not have an init node", controlPlane.Namespace, controlPlane.Name)
		//	return status, nil
		//}
		if errs := p.runEtcdSnapshotCreate(clusterPlan, runtimeCommand, version); len(errs) > 0 {
			for _, err := range errs {
				if err == nil {
					continue
				}
				finErrs = append(finErrs, err)
				if !IsErrWaiting(err) {
					// we have a failed snapshot from a node.
					if !stateSet {
						if err != nil {
							finErrs = append(finErrs, err)
						} else {
							stateSet = true
						}
					}
				}
			}
			return rkev1.ETCDSnapshotPhaseFailed, errWaiting(merr.NewErrors(finErrs...).Error())
		}
		return "", nil
	case rkev1.ETCDSnapshotPhaseFailed:
		// todo(jhyde): this feels wrong to have
		return "", nil
	default:
		return rkev1.ETCDSnapshotPhaseStarted, nil
	}
}
