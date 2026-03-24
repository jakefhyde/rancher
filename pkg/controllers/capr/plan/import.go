package plan

import (
	"strings"

	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	"github.com/rancher/rancher/pkg/image"
	namespaces "github.com/rancher/rancher/pkg/namespace"
	"github.com/rancher/rancher/pkg/settings"
	upgradev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

type importHandler struct {
}

const (
	Day2OpsEnabledAnnotation = "operations.cattle.io/ops-enabled"

	SystemAgentUpgrader     = "system-agent-upgrader"
	upgradeAPIVersion       = "upgrade.cattle.io/v1"
	upgradeDigestAnnotation = "upgrade.cattle.io/digest"
)

func (h *importHandler) ReconcileManagementCluster(_ string, cluster *apimgmtv3.Cluster) (*apimgmtv3.Cluster, error) {
	if cluster == nil {
		return nil, nil
	}

	if cluster.Name == "local" {
		return cluster, nil
	}

	// check the setting and annotation
	if cluster.Annotations[Day2OpsEnabledAnnotation] == "false" {
		return cluster, nil
	}

	if cluster.Annotations[Day2OpsEnabledAnnotation] == "" {
		if settings.ImportedClusterDay2OpsEnabledDefault.Get() == "true" {
			cluster.Annotations[Day2OpsEnabledAnnotation] = "true"
			// needs updating
		} else {
			return cluster, nil
		}
	}

	// if a cluster plan does not exist, create it

	return cluster, nil
}

func installer(cluster *apimgmtv3.Cluster, secretName string) []runtime.Object {
	upgradeImage := strings.SplitN(settings.SystemAgentUpgradeImage.Get(), ":", 2)
	version := SystemAgentUpgraderVersion()

	var env []corev1.EnvVar
	for _, e := range cluster.Spec.AgentEnvVars {
		env = append(env, corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		})
	}

	// Merge the env vars with the AgentTLSModeStrict
	found := false
	for _, ev := range env {
		if ev.Name == "STRICT_VERIFY" {
			found = true // The user has specified `STRICT_VERIFY`, we should not attempt to overwrite it.
		}
	}
	if !found {
		if settings.AgentTLSMode.Get() == settings.AgentTLSModeStrict {
			env = append(env, corev1.EnvVar{
				Name:  "STRICT_VERIFY",
				Value: "true",
			})
		} else {
			env = append(env, corev1.EnvVar{
				Name:  "STRICT_VERIFY",
				Value: "false",
			})
		}
	}

	//if len(cluster.Spec.RKEConfig.MachineSelectorConfig) == 0 {
	//	env = append(env, corev1.EnvVar{
	//		Name:  "CATTLE_ROLE_WORKER",
	//		Value: "true",
	//	})
	//}

	//if cluster.Spec.RKEConfig.DataDirectories.SystemAgent != "" {
	//	env = append(env, corev1.EnvVar{
	//		Name:  capr.SystemAgentDataDirEnvVar,
	//		Value: capr.GetSystemAgentDataDir(&cluster.Spec.RKEConfig.ClusterConfiguration),
	//	})
	//}
	var plans []runtime.Object

	plan := &upgradev1.Plan{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Plan",
			APIVersion: upgradeAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      SystemAgentUpgrader,
			Namespace: namespaces.System,
			Annotations: map[string]string{
				upgradeDigestAnnotation: "spec.upgrade.envs,spec.upgrade.envFrom",
			},
		},
		Spec: upgradev1.PlanSpec{
			Concurrency: 10,
			Version:     version,
			Tolerations: []corev1.Toleration{{
				Operator: corev1.TolerationOpExists,
			},
			},
			NodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      corev1.LabelOSStable,
						Operator: metav1.LabelSelectorOpIn,
						Values: []string{
							"linux",
						},
					},
				},
			},
			ServiceAccountName: SystemAgentUpgrader,
			// envFrom is still the source of CATTLE_ vars in plan, however secrets will trigger an update when changed.
			Secrets: []upgradev1.SecretSpec{
				{
					Name: "stv-aggregation",
				},
			},
			Upgrade: &upgradev1.ContainerSpec{
				Image: image.ResolveWithCluster(upgradeImage[0], cluster),
				Env:   env,
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
					},
				}},
			},
		},
	}
	plans = append(plans, plan)

	windowsPlan := winsUpgradePlan(cluster, env, secretName)
	//if cluster.Spec.RedeploySystemAgentGeneration != 0 {
	//	windowsPlan.Spec.Secrets = append(windowsPlan.Spec.Secrets, upgradev1.SecretSpec{
	//		Name: generationSecretName,
	//	})
	//}
	plans = append(plans, windowsPlan)

	objs := []runtime.Object{
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      SystemAgentUpgrader,
				Namespace: namespaces.System,
			},
		},
		&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: SystemAgentUpgrader,
			},
			Rules: []rbacv1.PolicyRule{{
				Verbs:     []string{"get"},
				APIGroups: []string{""},
				Resources: []string{"nodes"},
			}},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: SystemAgentUpgrader,
			},
			Subjects: []rbacv1.Subject{{
				Kind:      "ServiceAccount",
				Name:      SystemAgentUpgrader,
				Namespace: namespaces.System,
			}},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     SystemAgentUpgrader,
			},
		},
	}

	//// The stv-aggregation secret is managed separately, and SUC will trigger a plan upgrade automatically when the
	//// secret is updated. This prevents us from having to manually update the plan every time the secret changes
	//// (which is not often, and usually never).
	//if cluster.Spec.RedeploySystemAgentGeneration != 0 {
	//	plan.Spec.Secrets = append(plan.Spec.Secrets, upgradev1.SecretSpec{
	//		Name: generationSecretName,
	//	})
	//
	//	objs = append(objs, &corev1.Secret{
	//		ObjectMeta: metav1.ObjectMeta{
	//			Name:      generationSecretName,
	//			Namespace: namespaces.System,
	//		},
	//		StringData: map[string]string{
	//			"cluster-uid": string(cluster.UID),
	//			"generation":  strconv.Itoa(int(cluster.Spec.RedeploySystemAgentGeneration)),
	//		},
	//	})
	//}

	return append(plans, objs...)
}

func winsUpgradePlan(cluster *apimgmtv3.Cluster, env []corev1.EnvVar, secretName string) *upgradev1.Plan {
	winsUpgradeImage := strings.SplitN(settings.WinsAgentUpgradeImage.Get(), ":", 2)
	winsVersion := "latest"
	if len(winsUpgradeImage) == 2 {
		winsVersion = winsUpgradeImage[1]
	}

	return &upgradev1.Plan{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Plan",
			APIVersion: upgradeAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "system-agent-upgrader-windows",
			Namespace: namespaces.System,
			Annotations: map[string]string{
				upgradeDigestAnnotation: "spec.upgrade.envs,spec.upgrade.envFrom",
			},
		},
		Spec: upgradev1.PlanSpec{
			Concurrency: 10,
			Version:     winsVersion,
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
			NodeSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      corev1.LabelOSStable,
						Operator: metav1.LabelSelectorOpIn,
						Values: []string{
							"windows",
						},
					},
				},
			},
			ServiceAccountName: SystemAgentUpgrader,
			Upgrade: &upgradev1.ContainerSpec{
				Image: image.ResolveWithCluster(winsUpgradeImage[0], cluster),
				Env:   env,
				SecurityContext: &corev1.SecurityContext{
					WindowsOptions: &corev1.WindowsSecurityContextOptions{
						HostProcess:   ptr.To(true),
						RunAsUserName: ptr.To("NT AUTHORITY\\SYSTEM"),
					},
				},
				EnvFrom: []corev1.EnvFromSource{{
					SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
					},
				}},
			},
		},
	}
}

// SystemAgentUpgraderVersion returns the version of the system-agent-upgrader,
// which is determined by the image tag or defaults to "latest" if unspecified.
func SystemAgentUpgraderVersion() string {
	upgradeImage := strings.SplitN(settings.SystemAgentUpgradeImage.Get(), ":", 2)
	version := "latest"
	if len(upgradeImage) == 2 {
		version = upgradeImage[1]
	}
	return version
}
