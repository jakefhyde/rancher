package planner

import (
	"encoding/base64"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/equality"
)

const (
	encryptionKeyRotationStageReencryptFinished = "reencrypt_finished"

	encryptionKeyRotationSecretsEncryptStatusCommand = "secrets-encrypt-status"

	encryptionKeyRotationBinPrefix = "capr/encryption-key-rotation/bin"

	encryptionKeyRotationWaitForSystemctlStatusPath      = "wait_for_systemctl_status.sh"
	encryptionKeyRotationWaitForSecretsEncryptStatusPath = "wait_for_secrets_encrypt_status.sh"
	encryptionKeyRotationSecretsEncryptStatusPath        = "secrets_encrypt_status.sh"

	encryptionKeyRotationWaitForSystemctlStatus = `
#!/bin/sh

runtimeServer=$1
i=0

while [ $i -lt 30 ]; do
	systemctl is-active $runtimeServer
	if [ $? -eq 0 ]; then
		exit 0
	fi
	sleep 10
	i=$((i + 1))
done
exit 1
`

	encryptionKeyRotationWaitForSecretsEncryptStatusScript = `
#!/bin/sh

runtime=$1
i=0

while [ $i -lt 10 ]; do
	$runtime secrets-encrypt status
	if [ $? -eq 0 ]; then
			exit 0
	fi
	sleep 10
	i=$((i + 1))
done
exit 1
`

	encryptionKeyRotationSecretsEncryptStatusScript = `
#!/bin/sh

runtime=$1
i=0

while [ $i -lt 10 ]; do
	output="$($runtime secrets-encrypt status)"
	if [ $? -eq 0 ]; then
		if [ -n "$2" ]; then
			echo $output | grep -q "$2"
				if [ $? -eq 0 ]; then
					exit 0
				fi
		else
			exit 0
		fi
	fi
	sleep 10
	i=$((i + 1))
done
exit 1
`

	encryptionKeyRotationEndpointEnv = "CONTAINER_RUNTIME_ENDPOINT=unix:///var/run/k3s/containerd/containerd.sock"
)

func (p *Planner) setEncryptionKeyRotateState(status rkev1.RKEControlPlaneStatus, rotate *rkev1.RotateEncryptionKeys, phase rkev1.RotateEncryptionKeysPhase) (rkev1.RKEControlPlaneStatus, error) {
	if equality.Semantic.DeepEqual(status.RotateEncryptionKeys, rotate) && equality.Semantic.DeepEqual(status.RotateEncryptionKeysPhase, phase) {
		return status, nil
	}
	status.RotateEncryptionKeys = rotate
	status.RotateEncryptionKeysPhase = phase
	return status, errWaiting("refreshing encryption key rotation state")
}

func (p *Planner) resetEncryptionKeyRotateState(status rkev1.RKEControlPlaneStatus) (rkev1.RKEControlPlaneStatus, error) {
	if status.RotateEncryptionKeys == nil && status.RotateEncryptionKeysPhase == "" {
		return status, nil
	}
	return p.setEncryptionKeyRotateState(status, nil, "")
}

// rotateEncryptionKeys first verifies that the control plane is in a state where the next step can be derived. If encryption key rotation is required, the corresponding phase and status fields will be set.
// The function is expected to be called multiple times throughout encryption key rotation, and will set the next corresponding phase based on previous output.
func (p *Planner) rotateEncryptionKeys(info DistroInfo, input *rkev1.RotateEncryptionKeys, phase rkev1.RotateEncryptionKeysPhase, clusterPlan *plan.Plan, initNode, leader *planEntry) (rkev1.RotateEncryptionKeysPhase, error) {
	// todo(jhyde): make precondition
	//if !canRotateEncryptionKeys(controlPlane) {
	//	return status, nil
	//}
	//
	//if !ptr.Deref(status.Initialization.ControlPlaneInitialized, false) {
	//	// cluster is not yet initialized, so return nil for now.
	//	logrus.Warnf("[planner] rkecluster %s/%s: skipping encryption key rotation as cluster was not initialized", controlPlane.Namespace, controlPlane.Name)
	//	return status, nil
	//}

	// todo(jhyde): compute leader for each plane
	//found, joinServer, initNode, err := p.findInitNode(controlPlane, clusterPlan)
	//if err != nil {
	//	logrus.Errorf("[planner] rkecluster %s/%s: error encountered while searching for init node during encryption key rotation: %v", controlPlane.Namespace, controlPlane.Name, err)
	//	return status, err
	//}
	//if !found || joinServer == "" {
	//	logrus.Warnf("[planner] rkecluster %s/%s: skipping encryption key rotation as cluster does not have an init node", controlPlane.Namespace, controlPlane.Name)
	//	return status, nil
	//}

	logrus.Debugf("[planner] rkecluster %s/%s: current encryption key rotation phase: [%s]", controlPlane.Namespace, controlPlane.Spec.ClusterName, controlPlane.Status.RotateEncryptionKeysPhase)

	switch phase {
	case rkev1.RotateEncryptionKeysPhaseRotateKeys:
		//todo(jhyde): pause CAPI cluster before operation starts
		//if err := p.pauseCAPICluster(controlPlane, true); err != nil {
		//	return status, errWaiting("pausing CAPI cluster")
		//}
		err := p.encryptionKeyRotationLeaderPhaseReconcile(info, input, leader)
		if err != nil {
			return "", err
		}
		return rkev1.RotateEncryptionKeysPhaseRotateKeysRestart, nil
	case rkev1.RotateEncryptionKeysPhaseRotateKeysRestart:
		status, err = p.encryptionKeyRotationRestartNodes(controlPlane, status, clusterPlan, leader, initNode)
		if err != nil {
			return status, err
		}
		// todo(jhyde): unpause afterwards
		//if err = p.pauseCAPICluster(controlPlane, false); err != nil {
		//	return status, errWaiting("unpausing CAPI cluster")
		//}
		return "", nil
	default:
		return rkev1.RotateEncryptionKeysPhaseRotateKeys, nil
	}
}

//// canRotateEncryptionKeys returns false if the controlplane does not have a Ready: True condition and encryption key rotation is not already in progress, if the spec for
//// encryption key rotation is nil, or if the spec has been reconciled but the phase is done or failed.
//func canRotateEncryptionKeys(controlPlane *rkev1.RKEControlPlane) bool {
//	if (!capr.Ready.IsTrue(controlPlane) && !rotateEncryptionKeyInProgress(controlPlane)) ||
//		controlPlane.Spec.RotateEncryptionKeys == nil ||
//		(controlPlane.Status.RotateEncryptionKeys != nil && controlPlane.Status.RotateEncryptionKeys.Generation == controlPlane.Spec.RotateEncryptionKeys.Generation &&
//			(controlPlane.Status.RotateEncryptionKeysPhase == rkev1.RotateEncryptionKeysPhaseDone || controlPlane.Status.RotateEncryptionKeysPhase == rkev1.RotateEncryptionKeysPhaseFailed)) {
//		return false
//	}
//
//	return true
//}

// shouldRestartEncryptionKeyRotation returns `true` if encryption key rotation is necessary and the fields on the status object are not valid for an encryption key rotation procedure.
func shouldRestartEncryptionKeyRotation(controlPlane *rkev1.RKEControlPlane) bool {
	if !capr.Ready.IsTrue(controlPlane) {
		return false
	}
	if controlPlane.Spec.RotateEncryptionKeys.Generation > 0 && controlPlane.Status.RotateEncryptionKeys == nil {
		return true
	}
	if (controlPlane.Status.RotateEncryptionKeysPhase == rkev1.RotateEncryptionKeysPhaseDone ||
		controlPlane.Status.RotateEncryptionKeysPhase == rkev1.RotateEncryptionKeysPhaseFailed ||
		controlPlane.Status.RotateEncryptionKeysPhase == "") &&
		controlPlane.Spec.RotateEncryptionKeys.Generation != controlPlane.Status.RotateEncryptionKeys.Generation {
		return true
	}
	if !hasValidNonFailedRotateEncryptionKeyStatus(controlPlane) {
		return true
	}
	return false
}

// hasValidNonFailedRotateEncryptionKeyStatus verifies that the control plane encryption key status is an expected value and is not failed.
func hasValidNonFailedRotateEncryptionKeyStatus(controlPlane *rkev1.RKEControlPlane) bool {
	return rotateEncryptionKeyInProgress(controlPlane) ||
		controlPlane.Status.RotateEncryptionKeysPhase == rkev1.RotateEncryptionKeysPhaseDone
}

// rotateEncryptionKeyInProgress returns true if the phase of the encryption key rotation indicates that rotation is in progress.
func rotateEncryptionKeyInProgress(controlPlane *rkev1.RKEControlPlane) bool {
	return controlPlane.Status.RotateEncryptionKeysPhase == rkev1.RotateEncryptionKeysPhaseRotateKeys
}

// encryptionKeyRotationFindLeader returns the current encryption rotation leader if it is valid, otherwise, if the
// phase is "prepare", it will re-elect a new leader. It will look for the init node, and if the init node is not valid
// (etcd-only), it will elect the first suitable control plane node. If the phase is not in "prepare" and a re-election
// of the leader is necessary, the phase will be set to failed as this is unexpected.
func (p *Planner) encryptionKeyRotationFindLeader(status rkev1.RKEControlPlaneStatus, clusterPlan *plan.Plan, init *planEntry) (*planEntry, error) {
	machineName := status.RotateEncryptionKeysLeader
	if machine, ok := clusterPlan.Machines[machineName]; ok {
		entry := &planEntry{
			Machine:  machine,
			Plan:     clusterPlan.Nodes[machineName],
			Metadata: clusterPlan.Metadata[machineName],
		}
		if encryptionKeyRotationIsSuitableControlPlane(entry) {
			return entry, nil
		}
	}

	if status.RotateEncryptionKeysPhase != rkev1.RotateEncryptionKeysPhaseRotateKeys {
		// if we are electing a leader and are not in the "rotate-keys" phase, something is wrong.
		return nil, fmt.Errorf("cannot elect control plane leader in phase %s", status.RotateEncryptionKeysPhase)
	}

	leader := init
	if !isControlPlane(init) {
		machines := collect(clusterPlan, encryptionKeyRotationIsSuitableControlPlane)
		if len(machines) == 0 {
			return nil, fmt.Errorf("no suitable control plane nodes for encryption key rotation")
		}
		leader = machines[0]
	}

	return leader, nil
}

// encryptionKeyRotationIsSuitableControlPlane ensures that a control plane node has not been deleted and has a valid
// node associated with it.
func encryptionKeyRotationIsSuitableControlPlane(entry *planEntry) bool {
	return isControlPlane(entry) && isNotDeleting(entry) && entry.Machine.Status.NodeRef.IsDefined() && capr.Ready.IsTrue(entry.Machine)
}

// encryptionKeyRotationIsControlPlaneAndNotLeaderAndInit allows us to filter cluster plans to restart healthy follower nodes.
func encryptionKeyRotationIsControlPlaneAndNotLeaderAndInit(leader *planEntry) roleFilter {
	return func(entry *planEntry) bool {
		return isControlPlaneAndNotInitNode(entry) &&
			leader.Machine.Name != entry.Machine.Name
	}
}

// encryptionKeyRotationIsEtcdAndNotControlPlaneAndNotLeaderAndInit allows us to filter cluster plans to restart healthy follower nodes.
func encryptionKeyRotationIsEtcdAndNotControlPlaneAndNotLeaderAndInit(leader *planEntry) roleFilter {
	return func(entry *planEntry) bool {
		return isEtcd(entry) && !isControlPlane(entry) &&
			leader.Machine.Name != entry.Machine.Name &&
			!isInitNode(entry)
	}
}

// encryptionKeyRotationRestartNodes restarts the leader's server service, extracting the current stage afterwards.
// The followers (if any exist) are subsequently restarted. Notably, if the encryption key rotation leader is not the init node,
// it will restart the init node, then restart the encryption key rotation leader,
// then finalize walking through etcd nodes (that are not controlplane), then finally controlplane nodes.
func (p *Planner) encryptionKeyRotationRestartNodes(info DistroInfo, input *rkev1.RotateEncryptionKeys, clusterPlan *plan.Plan, leader *planEntry, initNode *planEntry) error {
	// in certain cases with multi-node setups, we must restart the init node before we can proceed to restarting the leader.
	if !isInitNode(leader) {
		logrus.Debugf("[planner] rkecluster %s/%s: leader %s was not the init node, finding and restarting etcd nodes", controlPlane.Namespace, controlPlane.Name, leader.Machine.Name)

		_, err := p.encryptionKeyRotationRestartService(info, input, initNode, false, "")
		if err != nil {
			return err
		}
		logrus.Debugf("[planner] rkecluster %s/%s: collecting etcd and not control plane", controlPlane.Namespace, controlPlane.Name)
		for _, entry := range collect(clusterPlan, encryptionKeyRotationIsEtcdAndNotControlPlaneAndNotLeaderAndInit(leader)) {
			_, err = p.encryptionKeyRotationRestartService(info, input, entry, false, "")
			if err != nil {
				return err
			}
		}
	}

	leaderStage, err := p.encryptionKeyRotationRestartService(info, input, leader, true, "")
	if err != nil {
		return err
	}

	logrus.Debugf("[planner] rkecluster %s/%s: collecting control plane and not leader and init nodes", controlPlane.Namespace, controlPlane.Name)
	for _, entry := range collect(clusterPlan, encryptionKeyRotationIsControlPlaneAndNotLeaderAndInit(leader)) {
		var stage string
		stage, err = p.encryptionKeyRotationRestartService(info, input, entry, true, leaderStage)
		if err != nil {
			return err
		}

		if stage != leaderStage {
			// secrets-encrypt command was run on another node. this is considered a failure, but might be a bit too sensitive. to be tested.
			return fmt.Errorf("leader [%s] with %s stage and follower [%s] with %s stage", leader.Machine.Status.NodeRef.Name, leaderStage, entry.Machine.Status.NodeRef.Name, stage))
		}
	}

	return nil
}

// encryptionKeyRotationRestartService restarts the server unit on the downstream node, waits until secrets-encrypt
// status can be successfully queried, and then gets the status. leaderStage is allowed to be empty if entry is the
// leader.
func (p *Planner) encryptionKeyRotationRestartService(info DistroInfo, input *rkev1.RotateEncryptionKeys, entry *planEntry, scrapeStage bool, leaderStage string) (string, error) {
	nodePlan := plan.NodePlan{}

	nodePlan.Files = append(nodePlan.Files, plan.File{
		Content: base64.StdEncoding.EncodeToString([]byte(encryptionKeyRotationWaitForSystemctlStatus)),
		Path:    encryptionKeyRotationScriptPath(info, encryptionKeyRotationWaitForSystemctlStatusPath),
	})

	nodePlan.Instructions = []plan.OneTimeInstruction{}

	runtime := info.Runtime()
	if runtime == capr.RuntimeRKE2 {
		if generated, instruction := generateManifestRemovalInstruction(info, entry); generated {
			nodePlan.Instructions = append(nodePlan.Instructions, convertToIdempotentInstruction(
				"encryption-key-rotation/manifest-cleanup",
				strconv.FormatInt(input.Generation, 10),
				instruction))
		}
	}

	nodePlan.Instructions = append(nodePlan.Instructions, idempotentRestartInstructions(
		"encryption-key-rotation/restart",
		strconv.FormatInt(input.Generation, 10),
		info.ServerSystemdService())...)

	nodePlan.Instructions = append(nodePlan.Instructions, encryptionKeyRotationWaitForSystemctlStatusInstruction(info, input))

	if isControlPlane(entry) {
		nodePlan.Files = append(nodePlan.Files,
			plan.File{
				Content: base64.StdEncoding.EncodeToString([]byte(encryptionKeyRotationSecretsEncryptStatusScript)),
				Path:    encryptionKeyRotationScriptPath(info, encryptionKeyRotationSecretsEncryptStatusPath),
			},
			plan.File{
				Content: base64.StdEncoding.EncodeToString([]byte(encryptionKeyRotationWaitForSecretsEncryptStatusScript)),
				Path:    encryptionKeyRotationScriptPath(info, encryptionKeyRotationWaitForSecretsEncryptStatusPath),
			},
		)
		nodePlan.Instructions = append(nodePlan.Instructions,
			encryptionKeyRotationWaitForSecretsEncryptStatus(info, input),
			encryptionKeyRotationSecretsEncryptStatusScriptOneTimeInstruction(info, input, leaderStage),
			encryptionKeyRotationSecretsEncryptStatusOneTimeInstruction(info, input),
		)
	}

	probes, err := info.ProbesForEntry(entry)
	if err != nil {
		return "", err
	}
	nodePlan.Probes = probes

	// retry is important here because without it, we always seem to run into some sort of issue such as:
	// - the follower node reporting the wrong status after a restart
	// - the plan failing with the k3s/rke2-server services crashing the first, and resuming subsequent times
	// It's not necessarily ideal if encryption key rotation can never complete, especially since we don't have access to
	// the downstream k3s/rke2-server service logs, but it has to be done in order for encryption key rotation to succeed
	err = assignAndCheckPlan(p.store, fmt.Sprintf("encryption key rotation [%s] for machine [%s]", controlPlane.Status.RotateEncryptionKeysPhase, entry.Machine.Name), entry, nodePlan, "", 5, 5)
	if err != nil {
		if IsErrWaiting(err) {
			if planAppliedButWaitingForProbes(entry) {
				return "", errWaitingf("%s: %s", err.Error(), probesMessage(entry.Plan))
			}
			return "", err
		}
		return "", err
	}

	if !scrapeStage || !isControlPlane(entry) {
		return "", nil
	}

	stage, err := encryptionKeyRotationSecretsEncryptStageFromOneTimeStatus(entry)
	if err != nil {
		return "", err
	}
	return stage, nil
}

// encryptionKeyRotationLeaderPhaseReconcile will run the secrets-encrypt command that corresponds to the phase, and scrape output to ensure that it was
// successful. If the secrets-encrypt command does not exist on the plan, that means this is the first reconciliation, and
// it must be added, otherwise reenqueue until the plan is in sync.
func (p *Planner) encryptionKeyRotationLeaderPhaseReconcile(info DistroInfo, input *rkev1.RotateEncryptionKeys, leader *planEntry) error {
	nodePlan := plan.NodePlan{}

	apply, err := encryptionKeyRotationSecretsEncryptInstruction(info, input)
	if err != nil {
		return err
	}

	nodePlan.Instructions = []plan.OneTimeInstruction{
		apply,
	}
	nodePlan.PeriodicInstructions = []plan.PeriodicInstruction{
		encryptionKeyRotationSecretsEncryptStatusPeriodicInstruction(info),
	}
	err = assignAndCheckPlan(p.store, fmt.Sprintf("encryption key rotation [%s] for machine [%s]", controlPlane.Status.RotateEncryptionKeysPhase, leader.Machine.Name), leader, nodePlan, "", 1, 1)
	if err != nil {
		if IsErrWaiting(err) {
			if strings.HasPrefix(err.Error(), "starting") {
				logrus.Infof("[planner] rkecluster %s/%s: applying encryption key rotation stage command: [%s]", controlPlane.Namespace, controlPlane.Spec.ClusterName, apply.Args[1])
			}
		}
		return err
	}
	periodic, err := encryptionKeyRotationSecretsEncryptStageFromPeriodic(leader)
	if err != nil {
		return err
	}

	if periodic != encryptionKeyRotationStageReencryptFinished {
		return errWaitingf("waiting for encryption key rotation stage to be finished")
	}

	// successful restart, complete same phases for rotate & reencrypt
	logrus.Infof("[planner] rkecluster %s/%s: successfully applied encryption key rotation stage command: [%s]", controlPlane.Namespace, controlPlane.Spec.ClusterName, leader.Plan.Plan.Instructions[0].Args[1])
	return nil
}

// encryptionKeyRotationSecretsEncryptStageFromPeriodic will attempt to extract the current stage (secrets-encrypt status) from the
// plan by parsing the periodic output.
func encryptionKeyRotationSecretsEncryptStageFromPeriodic(plan *planEntry) (string, error) {
	output, ok := plan.Plan.PeriodicOutput[encryptionKeyRotationSecretsEncryptStatusCommand]
	if !ok {
		for _, pi := range plan.Plan.Plan.PeriodicInstructions {
			if pi.Name == encryptionKeyRotationSecretsEncryptStatusCommand {
				return "", errWaitingf("could not extract current status from plan for [%s]: no output for status", plan.Machine.Name)
			}
		}
		return "", fmt.Errorf("could not extract current status from plan for [%s]: status command not present in plan", plan.Machine.Name)
	}
	periodic, err := encryptionKeyRotationStageFromOutput(plan, string(output.Stdout))
	return periodic, err
}

// encryptionKeyRotationSecretsEncryptStageFromOneTimeStatus will attempt to extract the current stage (secrets-encrypt status) from the
// plan by parsing the one-time output.
func encryptionKeyRotationSecretsEncryptStageFromOneTimeStatus(plan *planEntry) (string, error) {
	output, ok := plan.Plan.Output[encryptionKeyRotationSecretsEncryptStatusCommand]
	if !ok {
		return "", errWaitingf("could not extract current status from plan for [%s]: no output for status", plan.Machine.Name)
	}
	status, err := encryptionKeyRotationStageFromOutput(plan, string(output))
	return status, err
}

// encryptionKeyRotationStageFromOutput parses the output of a secrets-encrypt status command.
func encryptionKeyRotationStageFromOutput(plan *planEntry, output string) (string, error) {
	a := strings.Split(output, "\n")
	if len(a) < 2 {
		return "", errWaitingf("could not extract current stage from plan for [%s]: status output is incomplete", plan.Machine.Name)
	}
	for _, v := range a {
		a = strings.Split(v, ": ")
		if a[0] != "Current Rotation Stage" {
			continue
		}
		status := a[1]
		return status, nil
	}
	return "", errWaitingf("unable to parse rotation stage from output")
}

// encryptionKeyRotationSecretsEncryptInstruction generates a secrets-encrypt command to run on the leader node given
// the current secrets-encrypt phase.
func encryptionKeyRotationSecretsEncryptInstruction(info DistroInfo, input *rkev1.RotateEncryptionKeys) (plan.OneTimeInstruction, error) {
	return idempotentInstruction(
		"encryption-key-rotation/rotate-keys",
		strconv.FormatInt(input.Generation, 10),
		info.Runtime(),
		[]string{
			"secrets-encrypt",
			"rotate-keys",
		},
		[]string{},
	), nil
}

// encryptionKeyRotationGenerationEnv returns an environment variable in order to force followers to rerun their plans
// on subsequent generations, in the event that encryption key rotation is restarting and failed during prepare.
func encryptionKeyRotationGenerationEnv(input *rkev1.RotateEncryptionKeys) string {
	return fmt.Sprintf("ENCRYPTION_KEY_ROTATION_GENERATION=%d", input.Generation)
}

// encryptionKeyRotationSecretsEncryptStatusOneTimeInstruction generates a one-time instruction which will scrape the secrets-encrypt
// status.
func encryptionKeyRotationSecretsEncryptStatusScriptOneTimeInstruction(info DistroInfo, input *rkev1.RotateEncryptionKeys, expected string) plan.OneTimeInstruction {
	i := plan.OneTimeInstruction{
		Name:    "secrets-encrypt-status-script",
		Command: "sh",
		Args: []string{
			"-x",
			encryptionKeyRotationScriptPath(info, encryptionKeyRotationSecretsEncryptStatusPath),
			info.Runtime(),
		},
		Env: []string{
			encryptionKeyRotationGenerationEnv(input),
		},
	}
	if expected != "" {
		i.Args = append(i.Args, expected)
	}
	return i
}

// encryptionKeyRotationSecretsEncryptStatusOneTimeInstruction generates a one time instruction which will scrape the secrets-encrypt
// status.
func encryptionKeyRotationSecretsEncryptStatusOneTimeInstruction(info DistroInfo, input *rkev1.RotateEncryptionKeys) plan.OneTimeInstruction {
	return plan.OneTimeInstruction{
		Name:    encryptionKeyRotationSecretsEncryptStatusCommand,
		Command: info.Runtime(),
		Args: []string{
			"secrets-encrypt",
			"status",
		},
		Env: []string{
			encryptionKeyRotationGenerationEnv(input),
		},
		SaveOutput: true,
	}
}

// encryptionKeyRotationSecretsEncryptStatusPeriodicInstruction generates a periodic instruction which will scrape the secrets-encrypt
// status from the node every 5 seconds.
func encryptionKeyRotationSecretsEncryptStatusPeriodicInstruction(info DistroInfo) plan.PeriodicInstruction {
	return plan.PeriodicInstruction{
		Name:    encryptionKeyRotationSecretsEncryptStatusCommand,
		Command: info.Runtime(),
		Args: []string{
			"secrets-encrypt",
			"status",
		},
		PeriodSeconds: 5,
	}
}

// encryptionKeyRotationWaitForSystemctlStatusInstruction is intended to run after a node is restart, and wait until the
// node is online and able to provide systemctl status, ensuring that the server service is able to be restarted. If the
// service never comes active, the plan advances anyway in order to restart the service. If restarting the service
// fails, then the plan will fail.
func encryptionKeyRotationWaitForSystemctlStatusInstruction(info DistroInfo, input *rkev1.RotateEncryptionKeys) plan.OneTimeInstruction {
	return plan.OneTimeInstruction{
		Name:    "wait-for-systemctl-status",
		Command: "sh",
		Args: []string{
			"-x", encryptionKeyRotationScriptPath(info, encryptionKeyRotationWaitForSystemctlStatusPath), info.ServerSystemdService(),
		},
		Env: []string{
			encryptionKeyRotationEndpointEnv,
			encryptionKeyRotationGenerationEnv(input),
		},
		SaveOutput: false,
	}
}

// encryptionKeyRotationWaitForSecretsEncryptStatus is intended to run after a node is restart, and wait until the node
// is online and able to provide secrets-encrypt status, ensuring that subsequent status commands from the system-agent
// will be successful.
func encryptionKeyRotationWaitForSecretsEncryptStatus(info DistroInfo, input *rkev1.RotateEncryptionKeys) plan.OneTimeInstruction {
	return plan.OneTimeInstruction{
		Name:    "wait-for-secrets-encrypt-status",
		Command: "sh",
		Args: []string{
			"-x", encryptionKeyRotationScriptPath(info, encryptionKeyRotationWaitForSecretsEncryptStatusPath), info.Runtime(),
		},
		Env: []string{
			encryptionKeyRotationEndpointEnv,
			encryptionKeyRotationGenerationEnv(input),
		},
		SaveOutput: true,
	}
}

// encryptionKeyRotationFailed updates the various status objects on the control plane, allowing the cluster to
// continue the reconciliation loop. Encryption key rotation will not be restarted again until requested.
func (p *Planner) encryptionKeyRotationFailed(status rkev1.RKEControlPlaneStatus, err error) (rkev1.RKEControlPlaneStatus, error) {
	status.RotateEncryptionKeysPhase = rkev1.RotateEncryptionKeysPhaseFailed
	return status, errors.Wrap(err, "encryption key rotation failed, please perform an etcd restore")
}

func encryptionKeyRotationScriptPath(info DistroInfo, file string) string {
	return path.Join(info.DataDirectory(), encryptionKeyRotationBinPrefix, file)
}
