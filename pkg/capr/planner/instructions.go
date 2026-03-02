package planner

import (
	"fmt"
	"path"
	"strings"

	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/rancher/pkg/provisioningv2/image"
	"github.com/rancher/rancher/pkg/settings"
	corev1 "k8s.io/api/core/v1"
)

const (
	captureAddressInstructionName = "capture-address"
	etcdNameInstructionName       = "etcd-name"
)

type DistroInfo struct {
	Runtime               func() string
	InstallerImage        func() string
	DataDirectory         func() string
	Version               func() string
	RuntimeSupervisorPort func() int

	AgentEnvVars func() []rkev1.EnvVar

	ProvisionGeneration func() int
	ConfigGeneration    func() int64

	ServerSystemdService func() string
	AgentSystemdService  func() string

	ProbesForEntry func(entry *planEntry) (map[string]plan.Probe, error)

	DisplayName func() string
}

func NewCAPRDistroInfo(controlPlane *rkev1.RKEControlPlane) DistroInfo {
	return DistroInfo{
		Runtime: func() string {
			return capr.GetRuntime(controlPlane.Spec.KubernetesVersion)
		},
		InstallerImage: func() string {
			return image.ResolveWithControlPlane(
				fmt.Sprintf("%s%s:%s",
					settings.SystemAgentInstallerImage.Get(),
					capr.GetRuntime(controlPlane.Spec.KubernetesVersion),
					strings.ReplaceAll(controlPlane.Spec.KubernetesVersion, "+", "-")),
				controlPlane)
		},
		DataDirectory: func() string {
			return capr.GetDistroDataDir(controlPlane)
		},
		Version: func() string {
			return controlPlane.Spec.KubernetesVersion
		},
		RuntimeSupervisorPort: func() int {
			return capr.GetRuntimeSupervisorPort(controlPlane.Spec.KubernetesVersion)
		},
		AgentEnvVars: func() []rkev1.EnvVar {
			return controlPlane.Spec.AgentEnvVars
		},
		ProvisionGeneration: func() int {
			return controlPlane.Spec.ProvisionGeneration
		},
		ConfigGeneration: func() int64 {
			return controlPlane.Status.ConfigGeneration
		},
		ServerSystemdService: func() string {
			return capr.GetRuntimeServerUnit(controlPlane.Spec.KubernetesVersion)
		},
		AgentSystemdService: func() string {
			return capr.GetRuntimeAgentUnit(controlPlane.Spec.KubernetesVersion)
		},
		ProbesForEntry: func(entry *planEntry) (map[string]plan.Probe, error) {
			// todo(jhyde): remove planner dependency on config
			return generateProbes(controlPlane, entry, nil)
		},
		DisplayName: func() string {
			return fmt.Sprintf("rkecontrolplane.rke.cattle.io=%s/%s", controlPlane.Namespace, controlPlane.Name)
		},
	}
}

// generateInstallInstruction generates the instruction necessary to install the desired tool.
func (p *Planner) generateInstallInstruction(info DistroInfo, entry *planEntry, env []string) plan.OneTimeInstruction {
	var instruction plan.OneTimeInstruction
	image := info.InstallerImage()
	cattleOS := entry.Metadata.Labels[capr.CattleOSLabel]
	for _, arg := range info.AgentEnvVars() {
		if arg.Value == "" {
			continue
		}
		switch cattleOS {
		case capr.WindowsMachineOS:
			env = append(env, capr.FormatWindowsEnvVar(corev1.EnvVar{
				Name:  arg.Name,
				Value: arg.Value,
			}, true))
		default:
			env = append(env, fmt.Sprintf("%s=%s", arg.Name, arg.Value))
		}
	}
	switch cattleOS {
	case capr.WindowsMachineOS:
		// TODO: Properly format the data dir when adding full support for Windows nodes
		env = append(env, fmt.Sprintf("$env:%s_DATA_DIR=\"c:%s\"", strings.ToUpper(info.Runtime()), info.DataDirectory()))
		env = append(env, capr.FormatWindowsEnvVar(corev1.EnvVar{
			Name:  "INSTALL_RKE2_VERSION",
			Value: info.Version(),
		}, true))
	default:
		env = append(env, fmt.Sprintf("%s_DATA_DIR=%s", strings.ToUpper(info.Runtime()), info.DataDirectory()))
	}

	switch cattleOS {
	case capr.WindowsMachineOS:
		instruction = plan.OneTimeInstruction{
			Name:    "install",
			Image:   image,
			Command: "powershell.exe",
			Args:    []string{"-File", "run.ps1"},
			Env:     env,
		}
	default:
		instruction = plan.OneTimeInstruction{
			Name:    "install",
			Image:   image,
			Command: "sh",
			Args:    []string{"-c", "run.sh"},
			Env:     env,
		}
	}

	if isOnlyWorker(entry) {
		switch cattleOS {
		case capr.WindowsMachineOS:
			instruction.Env = append(instruction.Env, capr.FormatWindowsEnvVar(corev1.EnvVar{
				Name:  fmt.Sprintf("INSTALL_%s_EXEC", strings.ToUpper(info.Runtime())),
				Value: "agent",
			}, true))
		default:
			instruction.Env = append(instruction.Env, fmt.Sprintf("INSTALL_%s_EXEC=agent", strings.ToUpper(info.Runtime())))
		}
	}

	return instruction
}

// addInstallInstructionWithRestartStamp will generate an instruction and append it to the node plan that executes the `run.sh` or `run.ps1`
// from the installer image based on the control plane configuration. It will generate a restart stamp based on the
// passed in configuration to determine whether it needs to start/restart the service being managed.
// It will also add a separate environment variable containing the hash of the drainable config.
func (p *Planner) addInstallInstructionWithRestartStamp(info DistroInfo, nodePlan plan.NodePlan, entry *planEntry) (plan.NodePlan, error) {
	env := make([]string, 0, 2)
	image := info.InstallerImage()
	stamp := restartStamp(info, nodePlan, image)
	drainHash := drainHash(info, nodePlan, image)
	switch entry.Metadata.Labels[capr.CattleOSLabel] {
	case capr.WindowsMachineOS:
		env = append(env,
			capr.FormatWindowsEnvVar(corev1.EnvVar{
				Name:  "WINS_RESTART_STAMP",
				Value: stamp,
			}, true),
			capr.FormatWindowsEnvVar(corev1.EnvVar{
				Name:  "WINS_DRAIN_HASH",
				Value: drainHash,
			}, true))
	default:
		env = append(env, "RESTART_STAMP="+stamp, "DRAIN_HASH="+drainHash)
	}
	nodePlan.Instructions = append(nodePlan.Instructions, p.generateInstallInstruction(info, entry, env))
	return nodePlan, nil
}

// generateInstallInstructionWithSkipStart will generate an instruction that executes the `run.sh` or `run.ps1`
// from the installer image based on the control plane configuration. It will add a `SKIP_START` environment variable to prevent
// the service from being started/restarted.
func (p *Planner) generateInstallInstructionWithSkipStart(info DistroInfo, entry *planEntry) plan.OneTimeInstruction {
	var skipStartEnv string
	switch entry.Metadata.Labels[capr.CattleOSLabel] {
	case capr.WindowsMachineOS:
		skipStartEnv = capr.FormatWindowsEnvVar(corev1.EnvVar{
			Name:  fmt.Sprintf("INSTALL_%s_SKIP_START", strings.ToUpper(info.Runtime())),
			Value: "true",
		}, true)
	default:
		skipStartEnv = fmt.Sprintf("INSTALL_%s_SKIP_START=true", strings.ToUpper(info.Runtime()))
	}
	instEnv := []string{skipStartEnv}
	return p.generateInstallInstruction(info, entry, instEnv)
}

func (p *Planner) addInitNodePeriodicInstruction(info DistroInfo, nodePlan plan.NodePlan) (plan.NodePlan, error) {
	nodePlan.PeriodicInstructions = append(nodePlan.PeriodicInstructions, []plan.PeriodicInstruction{
		{
			Name:    captureAddressInstructionName,
			Command: "sh",
			Args: []string{
				"-c",
				// the grep here is to make the command fail if we don't get the output we expect, like empty string.
				fmt.Sprintf("curl -f --retry 100 --retry-delay 5 --cacert %s https://localhost:%d/db/info | grep 'clientURLs'",
					path.Join(info.DataDirectory(), "server/tls/server-ca.crt"),
					info.RuntimeSupervisorPort(),
				),
			},
			PeriodSeconds: 600,
		},
		{
			Name:    etcdNameInstructionName,
			Command: "sh",
			Args: []string{
				"-c",
				fmt.Sprintf("cat %s", path.Join(info.DataDirectory(), "server/db/etcd/name")),
			},
			PeriodSeconds: 600,
		},
	}...)
	return nodePlan, nil
}

// generateManifestRemovalInstruction generates a rm -rf command for the manifests of a server. This was created in response to https://github.com/rancher/rancher/issues/41174
func generateManifestRemovalInstruction(info DistroInfo, entry *planEntry) (bool, plan.OneTimeInstruction) {
	runtime := info.Runtime()
	if runtime == "" || entry == nil || roleNot(roleOr(isEtcd, isControlPlane))(entry) {
		return false, plan.OneTimeInstruction{}
	}
	return true, plan.OneTimeInstruction{
		Name:    "remove server manifests",
		Command: "/bin/sh",
		Args: []string{
			"-c",
			fmt.Sprintf("rm -rf %s/%s-*.yaml", path.Join(info.DataDirectory(), "server/manifests"), runtime),
		},
	}
}
