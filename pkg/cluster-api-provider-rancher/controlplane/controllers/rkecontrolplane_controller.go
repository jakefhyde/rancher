package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/capr"
	caprplanner "github.com/rancher/rancher/pkg/capr/planner"
	"github.com/rancher/rancher/pkg/controllers/management/clusterconnected"
	provcluster "github.com/rancher/rancher/pkg/controllers/provisioningv2/cluster"
	capicontrollers "github.com/rancher/rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta2"
	mgmtcontrollers "github.com/rancher/rancher/pkg/generated/controllers/management.cattle.io/v3"
	provcontrollers "github.com/rancher/rancher/pkg/generated/controllers/provisioning.cattle.io/v1"
	rkecontrollers "github.com/rancher/rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/wrangler/v3/pkg/condition"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"github.com/rancher/wrangler/v3/pkg/relatedresource"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	capi "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

var (
	capiScalingUpCondition   = condition.Cond("ScalingUp")
	capiScalingDownCondition = condition.Cond("ScalingDown")
	capiRollingOutCondition  = condition.Cond("RollingOut")
)

type handler struct {
	planner                   *caprplanner.Planner
	clusterCache              mgmtcontrollers.ClusterCache
	provClusterCache          provcontrollers.ClusterCache
	rkeControlPlaneController rkecontrollers.RKEControlPlaneController
	machineDeploymentClient   capicontrollers.MachineDeploymentClient
	machineDeploymentCache    capicontrollers.MachineDeploymentCache
	machineCache              capicontrollers.MachineCache
	machineClient             capicontrollers.MachineClient
}

func Register(ctx context.Context, clients *wrangler.CAPIContext, planner *caprplanner.Planner) {
	h := handler{
		planner:                   planner,
		clusterCache:              clients.Mgmt.Cluster().Cache(),
		provClusterCache:          clients.Provisioning.Cluster().Cache(),
		rkeControlPlaneController: clients.RKE.RKEControlPlane(),
		machineDeploymentClient:   clients.CAPI.MachineDeployment(),
		machineDeploymentCache:    clients.CAPI.MachineDeployment().Cache(),
		machineCache:              clients.CAPI.Machine().Cache(),
		machineClient:             clients.CAPI.Machine(),
	}

	rkecontrollers.RegisterRKEControlPlaneStatusHandler(ctx, clients.RKE.RKEControlPlane(), "", "planner", h.OnChange)
	relatedresource.Watch(ctx, "planner", func(namespace, name string, obj runtime.Object) ([]relatedresource.Key, error) {
		if secret, ok := obj.(*corev1.Secret); ok {
			var relatedResources []relatedresource.Key
			clusterName := secret.Labels[capr.ClusterNameLabel]
			if clusterName != "" {
				logrus.Tracef("[planner] rkecluster %s/%s enqueue triggered by secret %s/%s", secret.Namespace, clusterName, secret.Namespace, secret.Name)
				relatedResources = append(relatedResources, relatedresource.Key{
					Namespace: secret.Namespace,
					Name:      clusterName,
				})
			}
			authorizedObjects := secret.Annotations[capr.AuthorizedObjectAnnotation]
			if authorizedObjects != "" {
				for _, clusterName = range strings.Split(authorizedObjects, ",") {
					logrus.Tracef("[planner] rkecluster %s/%s enqueue triggered by authorized secret %s/%s", secret.Namespace, clusterName, secret.Namespace, secret.Name)
					relatedResources = append(relatedResources, relatedresource.Key{
						Namespace: secret.Namespace,
						Name:      clusterName,
					})
				}
			}
			return relatedResources, nil
		} else if machine, ok := obj.(*capi.Machine); ok {
			clusterName := machine.Labels[capi.ClusterNameLabel]
			if clusterName != "" {
				logrus.Tracef("[planner] rkecluster %s/%s enqueue triggered by machine %s/%s", machine.Namespace, clusterName, machine.Namespace, machine.Name)
				return []relatedresource.Key{{
					Namespace: machine.Namespace,
					Name:      clusterName,
				}}, nil
			}
		} else if configmap, ok := obj.(*corev1.ConfigMap); ok {
			var relatedResources []relatedresource.Key
			authorizedObjects := configmap.Annotations[capr.AuthorizedObjectAnnotation]
			if authorizedObjects != "" {
				for _, clusterName := range strings.Split(authorizedObjects, ",") {
					logrus.Tracef("[planner] rkecluster %s/%s enqueue triggered by authorized configmap %s/%s", configmap.Namespace, clusterName, configmap.Namespace, configmap.Name)
					relatedResources = append(relatedResources, relatedresource.Key{
						Namespace: configmap.Namespace,
						Name:      clusterName,
					})
				}
			}
			return relatedResources, nil
		}
		return nil, nil
	}, clients.RKE.RKEControlPlane(), clients.Core.Secret(), clients.CAPI.Machine(), clients.Core.ConfigMap())

	rkecontrollers.RegisterRKEControlPlaneStatusHandler(ctx, clients.RKE.RKEControlPlane(),
		"", "rke-control-plane", h.ConnectAgent)
	relatedresource.Watch(ctx, "rke-control-plane-trigger", h.clusterWatch, clients.RKE.RKEControlPlane(), clients.Mgmt.Cluster())

	clients.RKE.RKEControlPlane().OnRemove(ctx, "rke-control-plane-remove", h.OnRemove)
}

func (h *handler) OnChange(cp *rkev1.RKEControlPlane, status rkev1.RKEControlPlaneStatus) (rkev1.RKEControlPlaneStatus, error) {
	logrus.Debugf("[planner] rkecluster %s/%s: handler WaitForClient called", cp.Namespace, cp.Name)
	if !cp.DeletionTimestamp.IsZero() {
		return status, nil
	}

	// With the upcoming CAPI v1beta2, status objects were changed to add new fields and conditions. Unfortunately, for
	// clusters without machine deployments or machine pools, the controlplane MUST have the `ScalingUp`, `ScalingDown`,
	// and `RollingOut` conditions. See https://github.com/kubernetes-sigs/cluster-api/issues/11820.
	scalingUpFound := false
	scalingDownFound := false
	rollingOutFound := false

	for _, cond := range status.Conditions {
		if cond.Type == string(capiScalingUpCondition) {
			scalingUpFound = true
		} else if cond.Type == string(capiScalingDownCondition) {
			scalingDownFound = true
		} else if cond.Type == string(capiRollingOutCondition) {
			rollingOutFound = true
		}
	}

	if !scalingUpFound || !scalingDownFound || !rollingOutFound {
		logrus.Debugf("[planner] rkecluster %s/%s: setting CAPI v1beta2 conditions", cp.Namespace, cp.Name)
		capiScalingUpCondition.False(&status)
		capiScalingDownCondition.False(&status)
		capiRollingOutCondition.False(&status)
		return status, nil
	}

	status.ObservedGeneration = cp.Generation

	logrus.Debugf("[planner] rkecluster %s/%s: calling planner process", cp.Namespace, cp.Name)
	status, err := h.planner.Process(cp, status)
	if err != nil {
		// planner.Process can encounter 3 types of errors:
		// * planner.errWaiting - This is an error that indicates we are waiting for something, and will not re-enqueue the object
		// * generic.ErrSkip - These will cause the object to be re-enqueued after 5 seconds.
		// * error - All other errors. This should be an actual error during planner processing.
		if caprplanner.IsErrWaiting(err) {
			logrus.Infof("[planner] rkecluster %s/%s: %v", cp.Namespace, cp.Name, err)
			capr.Ready.SetStatus(&status, "Unknown")
			capr.Ready.Message(&status, err.Error())
			capr.Ready.Reason(&status, "Waiting")
			// Set err to nil so planner doesn't automatically re-enqueue the object, as we're waiting.
			// If the Reconciled condition is already true and the error was NOT an errIgnore/ErrSkip/ErrWaiting and the status.AppliedSpec (from planner.Process) does not match the controlplane spec, set reconciled to unknown.
			if !equality.Semantic.DeepEqual(cp.Spec, status.AppliedSpec) {
				capr.Reconciled.SetStatus(&status, "Unknown")
				capr.Reconciled.Message(&status, "RKEControlPlane has not been fully reconciled yet")
				capr.Reconciled.Reason(&status, "Waiting")
			}
			return status, nil
		}
		if errors.Is(err, generic.ErrSkip) {
			logrus.Debugf("[planner] rkecluster %s/%s: ErrSkip: %v", cp.Namespace, cp.Name, err)
			h.rkeControlPlaneController.EnqueueAfter(cp.Namespace, cp.Name, 5*time.Second)
			return status, err
		}
		// An actual error occurred, so set the Ready and Reconciled conditions to this error and return
		logrus.Errorf("[planner] rkecluster %s/%s: error during plan processing: %v", cp.Namespace, cp.Name, err)
		capr.Ready.SetError(&status, "", err)
		capr.Reconciled.SetError(&status, "", err)
		return status, err
	}
	// No error encountered during planner.Process
	logrus.Debugf("[planner] rkecluster %s/%s: reconciliation complete", cp.Namespace, cp.Name)
	capr.Ready.True(&status)
	capr.Ready.Message(&status, "")
	capr.Ready.Reason(&status, "")
	capr.Stable.True(&status)
	capr.Stable.Message(&status, "")
	capr.Stable.Reason(&status, "")
	status.AppliedSpec = &cp.Spec
	capr.Reconciled.True(&status)
	capr.Reconciled.Message(&status, "")
	capr.Reconciled.Reason(&status, "")
	return status, nil
}

func (h *handler) ConnectAgent(obj *rkev1.RKEControlPlane, status rkev1.RKEControlPlaneStatus) (rkev1.RKEControlPlaneStatus, error) {
	status.ObservedGeneration = obj.Generation
	cluster, err := h.clusterCache.Get(obj.Spec.ManagementClusterName)
	if err != nil {
		h.rkeControlPlaneController.EnqueueAfter(obj.Namespace, obj.Name, 2*time.Second)
		return status, nil
	}

	status.AgentConnected = clusterconnected.Connected.IsTrue(cluster)
	return status, nil
}


func (h *handler) OnRemove(_ string, cp *rkev1.RKEControlPlane) (*rkev1.RKEControlPlane, error) {
	status := cp.Status
	cp = cp.DeepCopy()

	err := capr.DoRemoveAndUpdateStatus(cp, h.doRemove(cp), h.rkeControlPlaneController.EnqueueAfter)

	if equality.Semantic.DeepEqual(status, cp.Status) {
		return cp, err
	}
	cp, updateErr := h.rkeControlPlaneController.UpdateStatus(cp)
	if updateErr != nil {
		return cp, updateErr
	}

	return cp, err
}

func (h *handler) doRemove(cp *rkev1.RKEControlPlane) func() (string, error) {
	return func() (string, error) {
		logrus.Debugf("[rkecontrolplane] (%s/%s) Peforming removal of rkecontrolplane", cp.Namespace, cp.Name)
		// Control plane nodes are managed by the control plane object. Therefore, the control plane object shouldn't be cleaned up before the control plane nodes are removed.
		machines, err := h.machineCache.List(cp.Namespace, labels.SelectorFromSet(labels.Set{capi.ClusterNameLabel: cp.Name, capr.ControlPlaneRoleLabel: "true"}))
		if err != nil {
			return "", err
		}

		// Some machines may not have gotten the CAPI cluster-name label in previous versions in Rancher.
		// Because of update issues with the conversion webhook in rancher-webhook, we can't use a "migration" to add the label (it will fail because the conversion webhook is not available).
		// In addition, there is no way to "or" label selectors in the API, so we need to do this manually.
		otherMachines, err := h.machineCache.List(cp.Namespace, labels.SelectorFromSet(labels.Set{capr.ClusterNameLabel: cp.Name, capr.ControlPlaneRoleLabel: "true"}))
		if err != nil {
			return "", err
		}

		logrus.Debugf("[rkecontrolplane] (%s/%s) listed %d machines during removal", cp.Namespace, cp.Name, len(machines))
		logrus.Tracef("[rkecontrolplane] (%s/%s) machine list: %+v", cp.Namespace, cp.Name, machines)
		allMachines := append(machines, otherMachines...)

		for _, machine := range allMachines {
			// Only delete custom machines. Custom machines can be added outside the UI, so it is important to check each machine.
			if machine.Spec.InfrastructureRef.APIGroup != capr.RKEAPIGroup || machine.Spec.InfrastructureRef.Kind != "CustomMachine" {
				continue
			}
			if machine.DeletionTimestamp == nil {
				if err = h.machineClient.Delete(machine.Namespace, machine.Name, &metav1.DeleteOptions{}); err != nil {
					return "", fmt.Errorf("error deleting machine %s/%s: %v", machine.Namespace, machine.Name, err)
				}
			}
		}

		return capr.GetMachineDeletionStatus(allMachines)
	}
}

func (h *handler) clusterWatch(_, _ string, obj runtime.Object) ([]relatedresource.Key, error) {
	cluster, ok := obj.(*apimgmtv3.Cluster)
	if !ok {
		return nil, nil
	}

	provClusters, err := h.provClusterCache.GetByIndex(provcluster.ByCluster, cluster.Name)
	if err != nil || len(provClusters) == 0 {
		return nil, err
	}
	return []relatedresource.Key{
		{
			Namespace: provClusters[0].Namespace,
			Name:      provClusters[0].Name,
		},
	}, nil
}
