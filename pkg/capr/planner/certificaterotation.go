package planner

import (
	"fmt"
	"strconv"

	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/capr"
)

// rotateCertificates checks if there is a need to rotate any certificates and updates the plan accordingly.
func (p *Planner) rotateCertificates(info DistroInfo, input *rkev1.RotateCertificates, clusterPlan *plan.Plan) error {
	// Assemble our list of nodes in order of etcd-only, etcd with controlplane, controlplane-only, and everything else
	orderedEntriesToRotate := collectOrderedCertificateRotationEntries(clusterPlan)

	for _, node := range orderedEntriesToRotate {
		if !shouldRotateEntry(input, node) {
			continue
		}

		rotatePlan, err := p.rotateCertificatesPlan(info, input, node)
		if err != nil {
			return err
		}

		err = assignAndCheckPlan(p.store, fmt.Sprintf("[%s] certificate rotation", node.Machine.Name), node, rotatePlan, "", 0, 0)
		if err != nil {
			// todo(jhyde): handle CAPI cluster pause/unpause
			//// Ensure the CAPI cluster is paused if we have assigned and are checking a plan.
			//if pauseErr := p.pauseCAPICluster(controlPlane, true); pauseErr != nil {
			//	return status, pauseErr
			//}
			//return status, err
		}
	}

	// todo(jhyde): handle CAPI cluster pause/unpause
	//if err := p.pauseCAPICluster(controlPlane, false); err != nil {
	//	return status, errWaiting("unpausing CAPI cluster")
	//}

	return errWaiting("certificate rotation done")
}

func collectOrderedCertificateRotationEntries(clusterPlan *plan.Plan) []*planEntry {
	orderedEntriesToRotate := collect(clusterPlan, IsOnlyEtcd)                                                        // etcd or etcd + worker
	orderedEntriesToRotate = append(orderedEntriesToRotate, collect(clusterPlan, roleAnd(isControlPlane, isEtcd))...) // etcd + controlplane or etcd + controlplane+worker
	orderedEntriesToRotate = append(orderedEntriesToRotate, collect(clusterPlan, isOnlyControlPlane)...)              // controlplane or controlplane + worker
	orderedEntriesToRotate = append(orderedEntriesToRotate, collect(clusterPlan, isOnlyWorker)...)                    // worker
	return orderedEntriesToRotate
}

// rotateCertificatesPlan rotates the certificates for the services specified, if any, and restarts the service.  If no services are specified
// all certificates are rotated.
func (p *Planner) rotateCertificatesPlan(info DistroInfo, input *rkev1.RotateCertificates, entry *planEntry) (plan.NodePlan, error) {
	rotatePlan := plan.NodePlan{}

	if isOnlyWorker(entry) {
		if isOnlyWindowsWorker(entry) {
			rotatePlan.Instructions = append(rotatePlan.Instructions, windowsIdempotentRestartInstructions(
				"certificate-rotation/restart",
				strconv.FormatInt(input.Generation, 10), "rke2")...)
		} else {
			rotatePlan.Instructions = append(rotatePlan.Instructions, idempotentRestartInstructions(
				"certificate-rotation/restart",
				strconv.FormatInt(input.Generation, 10),
				info.AgentSystemdService())...)
		}
		return rotatePlan, nil
	}

	rotatePlan.Instructions = append(rotatePlan.Instructions, idempotentStopInstruction(
		"certificate-rotation/stop",
		strconv.FormatInt(input.Generation, 10),
		info.ServerSystemdService()))

	args := []string{
		"certificate",
		"rotate",
	}

	if len(input.Services) > 0 {
		for _, service := range input.Services {
			args = append(args, "-s", service)
		}
	}

	runtime := info.Runtime()

	rotatePlan.Instructions = append(rotatePlan.Instructions, idempotentInstruction(
		"certificate-rotation/rotate",
		strconv.FormatInt(input.Generation, 10),
		runtime,
		args,
		[]string{},
	))
	if isControlPlane(entry) {
		// The following kube-scheduler and kube-controller-manager certificates are self-signed by the respective services and are used by CAPR for secure healthz probes against the service.
		if rotationContainsService(input, "controller-manager") {
			//if kcmCertDir := getArgValue(config[KubeControllerManagerArg], CertDirArgument, "="); kcmCertDir != "" && getArgValue(config[KubeControllerManagerArg], TLSCertFileArgument, "=") == "" {
			//	rotatePlan.Instructions = append(rotatePlan.Instructions, []plan.OneTimeInstruction{
			//		idempotentInstruction(
			//			"certificate-rotation/rm-kcm-cert",
			//			strconv.FormatInt(input.Generation, 10),
			//			"rm",
			//			[]string{
			//				"-f",
			//				fmt.Sprintf("%s/%s", kcmCertDir, DefaultKubeControllerManagerCert),
			//			},
			//			[]string{},
			//		),
			//		idempotentInstruction(
			//			"certificate-rotation/rm-kcm-key",
			//			strconv.FormatInt(input.Generation, 10),
			//			"rm",
			//			[]string{
			//				"-f",
			//				fmt.Sprintf("%s/%s", kcmCertDir, strings.ReplaceAll(DefaultKubeControllerManagerCert, ".crt", ".key")),
			//			},
			//			[]string{},
			//		),
			//	}...)
			//	if runtime == capr.RuntimeRKE2 {
			//		rotatePlan.Instructions = append(rotatePlan.Instructions, idempotentInstruction(
			//			"certificate-rotation/rm-kcm-spm",
			//			strconv.FormatInt(input.Generation, 10),
			//			"rm",
			//			[]string{
			//				"-f",
			//				path.Join(info.DataDirectory(), "/agent/pod-manifests/kube-controller-manager.yaml"),
			//			},
			//			[]string{},
			//		))
			//	}
			//}
		}
		if rotationContainsService(input, "scheduler") {
			//if ksCertDir := getArgValue(config[KubeSchedulerArg], CertDirArgument, "="); ksCertDir != "" && getArgValue(config[KubeSchedulerArg], TLSCertFileArgument, "=") == "" {
			//	rotatePlan.Instructions = append(rotatePlan.Instructions, []plan.OneTimeInstruction{
			//		idempotentInstruction(
			//			"certificate-rotation/rm-ks-cert",
			//			strconv.FormatInt(input.Generation, 10),
			//			"rm",
			//			[]string{
			//				"-f",
			//				fmt.Sprintf("%s/%s", ksCertDir, DefaultKubeSchedulerCert),
			//			},
			//			[]string{},
			//		),
			//		idempotentInstruction(
			//			"certificate-rotation/rm-ks-key",
			//			strconv.FormatInt(input.Generation, 10),
			//			"rm",
			//			[]string{
			//				"-f",
			//				fmt.Sprintf("%s/%s", ksCertDir, strings.ReplaceAll(DefaultKubeSchedulerCert, ".crt", ".key")),
			//			},
			//			[]string{},
			//		),
			//	}...)
			//	if runtime == capr.RuntimeRKE2 {
			//		rotatePlan.Instructions = append(rotatePlan.Instructions, idempotentInstruction(
			//			"certificate-rotation/rm-ks-spm",
			//			strconv.FormatInt(input.Generation, 10),
			//			"rm",
			//			[]string{
			//				"-f",
			//				path.Join(info.DataDirectory(), "agent/pod-manifests/kube-scheduler.yaml"),
			//			},
			//			[]string{},
			//		))
			//	}
			//}
		}
	}
	if runtime == capr.RuntimeRKE2 {
		if generated, instruction := generateManifestRemovalInstruction(info, entry); generated {
			rotatePlan.Instructions = append(rotatePlan.Instructions, convertToIdempotentInstruction(
				"certificate-rotation/manifest-removal",
				strconv.FormatInt(input.Generation, 10),
				instruction))
		}
	}
	rotatePlan.Instructions = append(rotatePlan.Instructions, idempotentRestartInstructions(
		"certificate-rotation/restart",
		strconv.FormatInt(input.Generation, 10),
		info.ServerSystemdService())...)
	return rotatePlan, nil
}

// rotationContainsService searches the rotation.Services slice the specified service. If the length of the services slice is 0, it returns true.
func rotationContainsService(rotation *rkev1.RotateCertificates, service string) bool {
	if rotation == nil {
		return false
	}
	if len(rotation.Services) == 0 {
		return true
	}
	for _, desiredService := range rotation.Services {
		if desiredService == service {
			return true
		}
	}
	return false
}

// shouldRotateEntry returns true if the rotated services are applicable to the entry's roles.
func shouldRotateEntry(rotation *rkev1.RotateCertificates, entry *planEntry) bool {
	relevantServices := map[string]struct{}{}

	if len(rotation.Services) == 0 {
		return true
	}

	if isWorker(entry) {
		relevantServices["rke2-server"] = struct{}{}
		relevantServices["k3s-server"] = struct{}{}
		relevantServices["api-server"] = struct{}{}
		relevantServices["kubelet"] = struct{}{}
		relevantServices["kube-proxy"] = struct{}{}
		relevantServices["auth-proxy"] = struct{}{}
	}

	if isControlPlane(entry) {
		relevantServices["rke2-server"] = struct{}{}
		relevantServices["k3s-server"] = struct{}{}
		relevantServices["api-server"] = struct{}{}
		relevantServices["kubelet"] = struct{}{}
		relevantServices["kube-proxy"] = struct{}{}
		relevantServices["auth-proxy"] = struct{}{}
		relevantServices["controller-manager"] = struct{}{}
		relevantServices["scheduler"] = struct{}{}
		relevantServices["rke2-controller"] = struct{}{}
		relevantServices["k3s-controller"] = struct{}{}
		relevantServices["admin"] = struct{}{}
		relevantServices["cloud-controller"] = struct{}{}
	}

	if isEtcd(entry) {
		relevantServices["etcd"] = struct{}{}
		relevantServices["kubelet"] = struct{}{}
		relevantServices["k3s-server"] = struct{}{}
		relevantServices["rke2-server"] = struct{}{}
	}

	for i := range rotation.Services {
		if _, ok := relevantServices[rotation.Services[i]]; ok {
			return true
		}
	}

	return false
}
