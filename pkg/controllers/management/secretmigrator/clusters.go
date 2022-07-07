package secretmigrator

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/rancher/rancher/pkg/fleet"

	"github.com/rancher/norman/types/convert"
	v1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"

	"github.com/mitchellh/mapstructure"
	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	apiprjv3 "github.com/rancher/rancher/pkg/apis/project.cattle.io/v3"
	v3 "github.com/rancher/rancher/pkg/generated/norman/management.cattle.io/v3"
	pv3 "github.com/rancher/rancher/pkg/generated/norman/project.cattle.io/v3"
	"github.com/rancher/rancher/pkg/namespace"
	"github.com/rancher/rancher/pkg/pipeline/remote/model"
	pipelineutils "github.com/rancher/rancher/pkg/pipeline/utils"
	rketypes "github.com/rancher/rke/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/credentialprovider"
)

const (
	SecretNamespace             = namespace.GlobalNamespace
	SecretKey                   = "credential"
	S3BackupAnswersPath         = "rancherKubernetesEngineConfig.services.etcd.backupConfig.s3BackupConfig.secretKey"
	WeavePasswordAnswersPath    = "rancherKubernetesEngineConfig.network.weaveNetworkProvider.password"
	RegistryPasswordAnswersPath = "rancherKubernetesEngineConfig.privateRegistries[%d].password"
	VsphereGlobalAnswersPath    = "rancherKubernetesEngineConfig.cloudProvider.vsphereCloudProvider.global.password"
	VcenterAnswersPath          = "rancherKubernetesEngineConfig.cloudProvider.vsphereCloudProvider.virtualCenter[%s].password"
	OpenStackAnswersPath        = "rancherKubernetesEngineConfig.cloudProvider.openstackCloudProvider.global.password"
	AADClientAnswersPath        = "rancherKubernetesEngineConfig.cloudProvider.azureCloudProvider.aadClientSecret"
	AADCertAnswersPath          = "rancherKubernetesEngineConfig.cloudProvider.azureCloudProvider.aadClientCertPassword"
)

var PrivateRegistryQuestion = regexp.MustCompile(`rancherKubernetesEngineConfig.privateRegistries[[0-9]+].password`)
var VcenterQuestion = regexp.MustCompile(`rancherKubernetesEngineConfig.cloudProvider.vsphereCloudProvider.virtualCenter\[.+\].password`)

func (h *handler) sync(key string, cluster *v3.Cluster) (runtime.Object, error) {
	if cluster == nil || cluster.DeletionTimestamp != nil {
		return cluster, nil
	}
	if apimgmtv3.ClusterConditionSecretsMigrated.IsTrue(cluster) && apimgmtv3.ClusterConditionServiceAccountSecretsMigrated.IsTrue(cluster) {
		logrus.Tracef("[secretmigrator] cluster %s already migrated", cluster.Name)
		return cluster, nil
	}
	obj, err := apimgmtv3.ClusterConditionSecretsMigrated.Do(cluster, func() (runtime.Object, error) {
		// privateRegistries
		if cluster.Status.PrivateRegistrySecret == "" {
			logrus.Tracef("[secretmigrator] migrating private registry secrets for cluster %s", cluster.Name)
			regSecret, err := h.migrator.CreateOrUpdatePrivateRegistrySecret(cluster.Status.PrivateRegistrySecret, cluster.Spec.RancherKubernetesEngineConfig, cluster)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate private registry secrets for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if regSecret != nil {
				logrus.Tracef("[secretmigrator] private registry secret found for cluster %s", cluster.Name)
				cluster.Status.PrivateRegistrySecret = regSecret.Name
				cluster.Spec.RancherKubernetesEngineConfig.PrivateRegistries = CleanRegistries(cluster.Spec.RancherKubernetesEngineConfig.PrivateRegistries)
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.PrivateRegistries = CleanRegistries(cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.PrivateRegistries)
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.PrivateRegistries = CleanRegistries(cluster.Status.FailedSpec.RancherKubernetesEngineConfig.PrivateRegistries)
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate private registry secrets for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, regSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}

		// s3 backup cred
		if cluster.Status.S3CredentialSecret == "" {
			logrus.Tracef("[secretmigrator] migrating S3 secrets for cluster %s", cluster.Name)
			s3Secret, err := h.migrator.CreateOrUpdateS3Secret("", cluster.Spec.RancherKubernetesEngineConfig, cluster)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate S3 secrets for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if s3Secret != nil {
				logrus.Tracef("[secretmigrator] S3 secret found for cluster %s", cluster.Name)
				cluster.Status.S3CredentialSecret = s3Secret.Name
				cluster.Spec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig.S3BackupConfig.SecretKey = ""
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig.S3BackupConfig != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig.S3BackupConfig.SecretKey = ""
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig.S3BackupConfig != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.Services.Etcd.BackupConfig.S3BackupConfig.SecretKey = ""
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate S3 secrets for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, s3Secret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}

		// weave CNI password
		if cluster.Status.WeavePasswordSecret == "" {
			logrus.Tracef("[secretmigrator] migrating weave CNI secrets for cluster %s", cluster.Name)
			weaveSecret, err := h.migrator.CreateOrUpdateWeaveSecret("", cluster.Spec.RancherKubernetesEngineConfig, cluster)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate weave CNI secrets for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if weaveSecret != nil {
				logrus.Tracef("[secretmigrator] weave secret found for cluster %s", cluster.Name)
				cluster.Status.WeavePasswordSecret = weaveSecret.Name
				cluster.Spec.RancherKubernetesEngineConfig.Network.WeaveNetworkProvider.Password = ""
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.Network.WeaveNetworkProvider != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.Network.WeaveNetworkProvider.Password = ""
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.Network.WeaveNetworkProvider != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.Network.WeaveNetworkProvider.Password = ""
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate weave CNI secrets for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, weaveSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}

		// cloud provider secrets

		// vsphere global
		if cluster.Status.VsphereSecret == "" {
			logrus.Tracef("[secretmigrator] migrating vsphere global secret for cluster %s", cluster.Name)
			vsphereSecret, err := h.migrator.CreateOrUpdateVsphereGlobalSecret("", cluster.Spec.RancherKubernetesEngineConfig, cluster)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate vsphere global secret for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if vsphereSecret != nil {
				logrus.Tracef("[secretmigrator] vsphere global secret found for cluster %s", cluster.Name)
				cluster.Status.VsphereSecret = vsphereSecret.Name
				cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.Global.Password = ""
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.Global.Password = ""
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.Global.Password = ""
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate vsphere global secret for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, vsphereSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}
		// vsphere virtual center
		if cluster.Status.VirtualCenterSecret == "" {
			logrus.Tracef("[secretmigrator] migrating vsphere virtualcenter secret for cluster %s", cluster.Name)
			vcenterSecret, err := h.migrator.CreateOrUpdateVsphereVirtualCenterSecret("", cluster.Spec.RancherKubernetesEngineConfig, cluster)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate vsphere virtualcenter secret for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if vcenterSecret != nil {
				logrus.Tracef("[secretmigrator] vsphere virtualcenter secret found for cluster %s", cluster.Name)
				cluster.Status.VirtualCenterSecret = vcenterSecret.Name
				for k, v := range cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter {
					v.Password = ""
					cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter[k] = v
				}
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider != nil {
					for k, v := range cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter {
						v.Password = ""
						cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter[k] = v
					}
				}

				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider != nil {
					for k, v := range cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter {
						v.Password = ""
						cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter[k] = v
					}
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate vsphere virtualcenter secret for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, vcenterSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}
		// openstack
		if cluster.Status.OpenStackSecret == "" {
			logrus.Tracef("[secretmigrator] migrating openstack secret for cluster %s", cluster.Name)
			openStackSecret, err := h.migrator.CreateOrUpdateOpenStackSecret("", cluster.Spec.RancherKubernetesEngineConfig, nil)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate openstack secret for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if openStackSecret != nil {
				logrus.Tracef("[secretmigrator] openstack secret found for cluster %s", cluster.Name)
				cluster.Status.OpenStackSecret = openStackSecret.Name
				cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.OpenstackCloudProvider.Global.Password = ""
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.OpenstackCloudProvider != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.OpenstackCloudProvider.Global.Password = ""
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.OpenstackCloudProvider != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.OpenstackCloudProvider.Global.Password = ""
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate openstack secret for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, openStackSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}
		// aad client secret
		if cluster.Status.AADClientSecret == "" {
			logrus.Tracef("[secretmigrator] migrating aad client secret for cluster %s", cluster.Name)
			aadClientSecret, err := h.migrator.CreateOrUpdateAADClientSecret("", cluster.Spec.RancherKubernetesEngineConfig, nil)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate aad client secret for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if aadClientSecret != nil {
				logrus.Tracef("[secretmigrator] aad client secret found for cluster %s", cluster.Name)
				cluster.Status.AADClientSecret = aadClientSecret.Name
				cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider.AADClientSecret = ""
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider.AADClientSecret = ""
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider.AADClientSecret = ""
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate aad client secret for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, aadClientSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}
		// aad cert password
		if cluster.Status.AADClientCertSecret == "" {
			logrus.Tracef("[secretmigrator] migrating aad cert secret for cluster %s", cluster.Name)
			aadCertSecret, err := h.migrator.CreateOrUpdateAADCertSecret("", cluster.Spec.RancherKubernetesEngineConfig, nil)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate aad cert secret for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if aadCertSecret != nil {
				logrus.Tracef("[secretmigrator] aad cert secret found for cluster %s", cluster.Name)
				cluster.Status.AADClientCertSecret = aadCertSecret.Name
				cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider.AADClientCertPassword = ""
				if cluster.Status.AppliedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider != nil {
					cluster.Status.AppliedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider.AADClientCertPassword = ""
				}
				if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig != nil && cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider != nil {
					cluster.Status.FailedSpec.RancherKubernetesEngineConfig.CloudProvider.AzureCloudProvider.AADClientCertPassword = ""
				}
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate aad cert secret for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, aadCertSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}

		// cluster template questions and answers
		logrus.Tracef("[secretmigrator] cleaning questions and answers from cluster %s", cluster.Name)
		cleanQuestions(cluster)

		// notifiers
		notifiers, err := h.notifierLister.List(cluster.Name, labels.NewSelector())
		if err != nil {
			logrus.Errorf("[secretmigrator] failed to get notifiers for cluster %s, will retry: %v", cluster.Name, err)
			return nil, err
		}
		for _, n := range notifiers {
			if n.Status.SMTPCredentialSecret == "" && n.Spec.SMTPConfig != nil {
				logrus.Tracef("[secretmigrator] migrating SMTP secrets for notifier %s in cluster %s", n.Name, cluster.Name)
				smtpSecret, err := h.migrator.CreateOrUpdateSMTPSecret("", n.Spec.SMTPConfig, cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate SMTP secrets for notifier %s in cluster %s, will retry: %v", n.Name, cluster.Name, err)
					return nil, err
				}
				if smtpSecret != nil {
					logrus.Tracef("[secretmigrator] SMTP secret found for notifier %s in cluster %s", n.Name, cluster.Name)
					n.Status.SMTPCredentialSecret = smtpSecret.Name
					n.Spec.SMTPConfig.Password = ""
					_, err = h.notifiers.Update(n)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate SMTP secrets for notifier %s in cluster %s, will retry: %v", n.Name, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, smtpSecret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
			if n.Status.WechatCredentialSecret == "" && n.Spec.WechatConfig != nil {
				logrus.Tracef("[secretmigrator] migrating Wechat secrets for notifier %s in cluster %s", n.Name, cluster.Name)
				wechatSecret, err := h.migrator.CreateOrUpdateWechatSecret("", n.Spec.WechatConfig, cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate Wechat secrets for notifier %s in cluster %s, will retry: %v", n.Name, cluster.Name, err)
					return nil, err
				}
				if wechatSecret != nil {
					logrus.Tracef("[secretmigrator] Wechat secret found for notifier %s in cluster %s", n.Name, cluster.Name)
					n.Status.WechatCredentialSecret = wechatSecret.Name
					n.Spec.WechatConfig.Secret = ""
					_, err = h.notifiers.Update(n)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate Wechat secrets for notifier %s in cluster %s, will retry: %v", n.Name, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, wechatSecret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
			if n.Status.DingtalkCredentialSecret == "" && n.Spec.DingtalkConfig != nil {
				logrus.Tracef("[secretmigrator] migrating Dingtalk secrets for notifier %s in cluster %s", n.Name, cluster.Name)
				dingtalkSecret, err := h.migrator.CreateOrUpdateDingtalkSecret(n.Status.DingtalkCredentialSecret, n.Spec.DingtalkConfig, cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate Dingtalk secrets for notifier %s in cluster %s, will retry: %v", n.Name, cluster.Name, err)
					return nil, err
				}
				if dingtalkSecret != nil {
					logrus.Tracef("[secretmigrator] Dingtalk secret found for notifier %s in cluster %s", n.Name, cluster.Name)
					n.Status.DingtalkCredentialSecret = dingtalkSecret.Name
					n.Spec.DingtalkConfig.Secret = ""
					_, err = h.notifiers.Update(n)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate Dingtalk secrets for notifier %s in cluster %s, will retry: %v", n.Name, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, dingtalkSecret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
		}

		// cluster catalogs
		clusterCatalogs, err := h.clusterCatalogLister.List(cluster.Name, labels.NewSelector())
		if err != nil {
			logrus.Errorf("[secretmigrator] failed to get cluster catalogs for cluster %s, will retry: %v", cluster.Name, err)
			return nil, err
		}
		for _, c := range clusterCatalogs {
			if c.Status.CredentialSecret == "" && c.Spec.Password != "" {
				logrus.Tracef("[secretmigrator] migrating secrets for cluster catalog %s in cluster %s", c.Name, cluster.Name)
				secret, err := h.migrator.CreateOrUpdateCatalogSecret(c.Status.CredentialSecret, c.Spec.Password, cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for cluster catalog %s in cluster %s, will retry: %v", c.Name, cluster.Name, err)
					return nil, err
				}
				if secret != nil {
					logrus.Tracef("[secretmigrator] secret found for cluster catalog %s in cluster %s", c.Name, cluster.Name)
					c.Status.CredentialSecret = secret.Name
					c.Spec.Password = ""
					_, err = h.clusterCatalogs.Update(c)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for cluster catalog %s in cluster %s, will retry: %v", c.Name, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
		}

		projects, err := h.projectLister.List(cluster.Name, labels.NewSelector())
		if err != nil {
			logrus.Errorf("[secretmigrator] failed to get projects for cluster %s, will retry: %v", cluster.Name, err)
			return nil, err
		}

		// project catalogs
		for _, p := range projects {
			projectCatalogs, err := h.projectCatalogLister.List(p.Name, labels.NewSelector())
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to get project catalogs for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			for _, c := range projectCatalogs {
				if c.Status.CredentialSecret == "" && c.Spec.Password != "" {
					logrus.Tracef("[secretmigrator] migrating secrets for project catalog %s in cluster %s", c.Name, cluster.Name)
					secret, err := h.migrator.CreateOrUpdateCatalogSecret(c.Status.CredentialSecret, c.Spec.Password, cluster)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for project catalog %s in cluster %s, will retry: %v", c.Name, cluster.Name, err)
						return nil, err
					}
					if secret != nil {
						logrus.Tracef("[secretmigrator] secret found for project catalog %s in cluster %s", c.Name, cluster.Name)
						c.Status.CredentialSecret = secret.Name
						c.Spec.Password = ""
						_, err = h.projectCatalogs.Update(c)
						if err != nil {
							logrus.Errorf("[secretmigrator] failed to migrate secrets for project catalog %s in cluster %s, will retry: %v", c.Name, cluster.Name, err)
							deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
							if deleteErr != nil {
								logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
							}
							return nil, err
						}
					}
				}
			}
		}

		// sourcecodeproviderconfigs
		for _, p := range projects {
			m, err := h.getUnstructuredPipelineConfig(p.Name, model.GithubType)
			if err != nil && !apierrors.IsNotFound(err) {
				logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GithubType, cluster.Name, err)
				return nil, err
			}
			if !apierrors.IsNotFound(err) {
				if credentialSecret, ok := m["credentialSecret"]; ok && credentialSecret != nil {
					continue
				}
				logrus.Tracef("[secretmigrator] migrating secrets for %s pipeline config in cluster %s", model.GithubType, cluster.Name)
				github := &apiprjv3.GithubPipelineConfig{}
				if err = mapstructure.Decode(m, github); err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GithubType, cluster.Name, err)
					return nil, err
				}
				secret, err := h.migrator.CreateOrUpdateSourceCodeProviderConfigSecret("", github.ClientSecret, cluster, model.GithubType)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GithubType, cluster.Name, err)
					return nil, err
				}
				if secret != nil {
					logrus.Tracef("[secretmigrator] secret found for %s pipeline config in cluster %s", model.GithubType, cluster.Name)
					github.CredentialSecret = secret.Name
					github.ClientSecret = ""
					github.ObjectMeta, github.APIVersion, github.Kind, err = setSourceCodeProviderConfigMetadata(m)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GithubType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
					if _, err = h.sourceCodeProviderConfigs.ObjectClient().Update(github.Name, github); err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GithubType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
			m, err = h.getUnstructuredPipelineConfig(p.Name, model.GitlabType)
			if err != nil && !apierrors.IsNotFound(err) {
				logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GitlabType, cluster.Name, err)
				return nil, err
			}
			if !apierrors.IsNotFound(err) {
				if credentialSecret, ok := m["credentialSecret"]; ok && credentialSecret != nil {
					continue
				}
				logrus.Tracef("[secretmigrator] migrating secrets for %s pipeline config in cluster %s", model.GitlabType, cluster.Name)
				gitlab := &apiprjv3.GitlabPipelineConfig{}
				if err = mapstructure.Decode(m, gitlab); err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GitlabType, cluster.Name, err)
					return nil, err
				}
				secret, err := h.migrator.CreateOrUpdateSourceCodeProviderConfigSecret("", gitlab.ClientSecret, cluster, model.GitlabType)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GitlabType, cluster.Name, err)
					return nil, err
				}
				if secret != nil {
					logrus.Tracef("[secretmigrator] secret found for %s pipeline config in cluster %s", model.GitlabType, cluster.Name)
					gitlab.CredentialSecret = secret.Name
					gitlab.ClientSecret = ""
					gitlab.ObjectMeta, gitlab.APIVersion, gitlab.Kind, err = setSourceCodeProviderConfigMetadata(m)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GitlabType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
					if _, err = h.sourceCodeProviderConfigs.ObjectClient().Update(gitlab.Name, gitlab); err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.GitlabType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
			m, err = h.getUnstructuredPipelineConfig(p.Name, model.BitbucketCloudType)
			if err != nil && !apierrors.IsNotFound(err) {
				logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketCloudType, cluster.Name, err)
				return nil, err
			}
			if !apierrors.IsNotFound(err) {
				if credentialSecret, ok := m["credentialSecret"]; ok && credentialSecret != nil {
					continue
				}
				logrus.Tracef("[secretmigrator] migrating secrets for %s pipeline config in cluster %s", model.BitbucketCloudType, cluster.Name)
				bbcloud := &apiprjv3.BitbucketCloudPipelineConfig{}
				if err = mapstructure.Decode(m, bbcloud); err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketCloudType, cluster.Name, err)
					return nil, err
				}
				secret, err := h.migrator.CreateOrUpdateSourceCodeProviderConfigSecret("", bbcloud.ClientSecret, cluster, model.BitbucketCloudType)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketCloudType, cluster.Name, err)
					return nil, err
				}
				if secret != nil {
					logrus.Tracef("[secretmigrator] secret found for %s pipeline config in cluster %s", model.BitbucketCloudType, cluster.Name)
					bbcloud.CredentialSecret = secret.Name
					bbcloud.ClientSecret = ""
					bbcloud.ObjectMeta, bbcloud.APIVersion, bbcloud.Kind, err = setSourceCodeProviderConfigMetadata(m)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketCloudType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
					if _, err = h.sourceCodeProviderConfigs.ObjectClient().Update(bbcloud.Name, bbcloud); err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketCloudType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
			m, err = h.getUnstructuredPipelineConfig(p.Name, model.BitbucketServerType)
			if err != nil && !apierrors.IsNotFound(err) {
				logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketServerType, cluster.Name, err)
				return nil, err
			}
			if !apierrors.IsNotFound(err) {
				if credentialSecret, ok := m["credentialSecret"]; ok && credentialSecret != nil {
					continue
				}
				logrus.Tracef("[secretmigrator] migrating secrets for %s pipeline config in cluster %s", model.BitbucketServerType, cluster.Name)
				bbserver := &apiprjv3.BitbucketServerPipelineConfig{}
				if err = mapstructure.Decode(m, bbserver); err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketServerType, cluster.Name, err)
					return nil, err
				}
				secret, err := h.migrator.CreateOrUpdateSourceCodeProviderConfigSecret("", bbserver.PrivateKey, cluster, model.BitbucketServerType)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketServerType, cluster.Name, err)
					return nil, err
				}
				if secret != nil {
					logrus.Tracef("[secretmigrator] secret found for %s pipeline config in cluster %s", model.BitbucketServerType, cluster.Name)
					bbserver.CredentialSecret = secret.Name
					bbserver.PrivateKey = ""
					bbserver.ObjectMeta, bbserver.APIVersion, bbserver.Kind, err = setSourceCodeProviderConfigMetadata(m)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketServerType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
					_, err = h.sourceCodeProviderConfigs.ObjectClient().Update(bbserver.Name, bbserver)
					if err != nil {
						logrus.Errorf("[secretmigrator] failed to migrate secrets for %s pipeline config in cluster %s, will retry: %v", model.BitbucketServerType, cluster.Name, err)
						deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, secret.Name, &metav1.DeleteOptions{})
						if deleteErr != nil {
							logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
						}
						return nil, err
					}
				}
			}
		}

		logrus.Tracef("[secretmigrator] setting cluster condition [%s] and updating cluster [%s]", apimgmtv3.ClusterConditionSecretsMigrated, cluster.Name)
		apimgmtv3.ClusterConditionSecretsMigrated.True(cluster)
		return h.clusters.Update(cluster)
	})
	if err != nil {
		return obj, err
	}
	obj, err = apimgmtv3.ClusterConditionServiceAccountSecretsMigrated.Do(obj, func() (runtime.Object, error) {
		// serviceAccountToken
		if cluster.Status.ServiceAccountTokenSecret == "" {
			logrus.Tracef("[secretmigrator] migrating service account token secret for cluster %s", cluster.Name)
			saSecret, err := h.migrator.CreateOrUpdateServiceAccountTokenSecret(cluster.Status.ServiceAccountTokenSecret, cluster.Status.ServiceAccountToken, cluster)
			if err != nil {
				logrus.Errorf("[secretmigrator] failed to migrate service account token secret for cluster %s, will retry: %v", cluster.Name, err)
				return nil, err
			}
			if saSecret != nil {
				logrus.Tracef("[secretmigrator] service account token secret found for cluster %s", cluster.Name)
				cluster.Status.ServiceAccountTokenSecret = saSecret.Name
				cluster.Status.ServiceAccountToken = ""
				clusterCopy, err := h.clusters.Update(cluster)
				if err != nil {
					logrus.Errorf("[secretmigrator] failed to migrate service account token secret for cluster %s, will retry: %v", cluster.Name, err)
					deleteErr := h.migrator.secrets.DeleteNamespaced(SecretNamespace, saSecret.Name, &metav1.DeleteOptions{})
					if deleteErr != nil {
						logrus.Errorf("[secretmigrator] encountered error while handling migration error: %v", deleteErr)
					}
					return nil, err
				}
				cluster = clusterCopy
			}
		}
		logrus.Tracef("[secretmigrator] setting cluster condition [%s] and updating cluster [%s]", apimgmtv3.ClusterConditionServiceAccountSecretsMigrated, cluster.Name)
		apimgmtv3.ClusterConditionServiceAccountSecretsMigrated.True(cluster)
		return h.clusters.Update(cluster)
	})
	return obj, err
}

func (h *handler) getUnstructuredPipelineConfig(namespace, pType string) (map[string]interface{}, error) {
	obj, err := h.sourceCodeProviderConfigs.ObjectClient().UnstructuredClient().GetNamespaced(namespace, pType, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	u, ok := obj.(runtime.Unstructured)
	if !ok {
		return nil, fmt.Errorf("could not get github source code provider")
	}
	return u.UnstructuredContent(), nil
}

// CreateOrUpdatePrivateRegistrySecret accepts an optional secret name and a RancherKubernetesEngineConfig object and creates a dockerconfigjson Secret
// containing the login credentials for every registry in the array, if there are any.
// If an owner is passed, the owner is set as an owner reference on the Secret. If no owner is passed,
// the caller is responsible for calling UpdateSecretOwnerReference once the owner is known.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdatePrivateRegistrySecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil {
		return nil, nil
	}
	rkeConfig = rkeConfig.DeepCopy()
	privateRegistries := rkeConfig.PrivateRegistries
	if len(privateRegistries) == 0 {
		return nil, nil
	}
	var existing *corev1.Secret
	var err error
	if secretName != "" {
		var err error
		existing, err = m.secretLister.Get(SecretNamespace, secretName)
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	registry := credentialprovider.DockerConfigJSON{
		Auths: map[string]credentialprovider.DockerConfigEntry{},
	}
	active := make(map[string]struct{})
	registrySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:         secretName, // if empty, the secret will be created with a generated name
			GenerateName: "cluster-registry-",
			Namespace:    SecretNamespace,
		},
		Data: map[string][]byte{},
		Type: corev1.SecretTypeDockerConfigJson,
	}
	if owner != nil {
		gvk := owner.GetObjectKind().GroupVersionKind()
		accessor, err := meta.Accessor(owner)
		if err != nil {
			return nil, err
		}
		registrySecret.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: gvk.Group + "/" + gvk.Version,
				Kind:       gvk.Kind,
				Name:       accessor.GetName(),
				UID:        accessor.GetUID(),
			},
		}
	}
	if existing != nil {
		err = json.Unmarshal(existing.Data[corev1.DockerConfigJsonKey], &registry)
		if err != nil {
			return nil, err
		}
	}
	for _, privateRegistry := range privateRegistries {
		active[privateRegistry.URL] = struct{}{}
		if privateRegistry.Password == "" {
			continue
		}
		// limitation: if a URL is repeated in the privateRegistries list, it will be overwritten in the registry secret
		registry.Auths[privateRegistry.URL] = credentialprovider.DockerConfigEntry{
			Username: privateRegistry.User,
			Password: privateRegistry.Password,
		}
	}
	registryJSON, err := json.Marshal(registry)
	if err != nil {
		return nil, err
	}
	registrySecret.Data = map[string][]byte{
		corev1.DockerConfigJsonKey: registryJSON,
	}
	if existing == nil {
		registrySecret, err = m.secrets.Create(registrySecret)
		if err != nil {
			return nil, err
		}
	} else {
		for url := range registry.Auths {
			if _, ok := active[url]; !ok {
				delete(registry.Auths, url)
			}
		}
		registrySecret.Data[corev1.DockerConfigJsonKey], err = json.Marshal(registry)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(existing.Data, registrySecret.Data) {
			return m.secrets.Update(registrySecret)
		}
	}
	return registrySecret, nil
}

// CleanRegistries unsets the password of every private registry in the list.
// Must be called after passwords have been migrated.
func CleanRegistries(privateRegistries []rketypes.PrivateRegistry) []rketypes.PrivateRegistry {
	for i := range privateRegistries {
		privateRegistries[i].Password = ""
	}
	return privateRegistries
}

// UpdateSecretOwnerReference sets an object as owner of a given Secret and updates the Secret.
// The object must be a non-namespaced resource.
func (m *Migrator) UpdateSecretOwnerReference(secret *corev1.Secret, owner metav1.OwnerReference) error {
	secret.OwnerReferences = []metav1.OwnerReference{owner}
	_, err := m.secrets.Update(secret)
	return err
}

// createOrUpdateSecret accepts an optional secret name and tries to update it with the provided data if it exists, or creates it.
// If an owner is provided, it sets it as an owner reference before creating it. If annotations are provided, they are added
// before the secret is created.
func (m *Migrator) createOrUpdateSecret(secretName, secretNamespace string, data, annotations map[string]string, owner runtime.Object, kind, field string) (*corev1.Secret, error) {
	var existing *corev1.Secret
	var err error
	if secretName != "" {
		existing, err = m.secretLister.Get(secretNamespace, secretName)
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:         secretName,
			GenerateName: fmt.Sprintf("%s-%s-", kind, field),
			Namespace:    secretNamespace,
		},
		StringData: data,
		Type:       corev1.SecretTypeOpaque,
	}
	if owner != nil {
		gvk := owner.GetObjectKind().GroupVersionKind()
		accessor, err := meta.Accessor(owner)
		if err != nil {
			return nil, err
		}
		secret.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: gvk.Group + "/" + gvk.Version,
				Kind:       gvk.Kind,
				Name:       accessor.GetName(),
				UID:        accessor.GetUID(),
			},
		}
	}
	if annotations != nil {
		secret.Annotations = annotations
	}
	if existing == nil {
		return m.secrets.Create(secret)
	} else if !reflect.DeepEqual(existing.StringData, secret.StringData) {
		existing.StringData = data
		return m.secrets.Update(existing)
	}
	return secret, nil
}

// createOrUpdateSecretForCredential accepts an optional secret name and a value containing the data that needs to be sanitized,
// and creates a secret to hold the sanitized data. If an owner is passed, the owner is set as an owner reference on the secret.
func (m *Migrator) createOrUpdateSecretForCredential(secretName, secretNamespace, secretValue string, annotations map[string]string, owner runtime.Object, kind, field string) (*corev1.Secret, error) {
	if secretValue == "" {
		logrus.Warnf("Refusing to create empty secret %s:%s", secretNamespace, secretName)
		return nil, nil
	}
	data := map[string]string{
		SecretKey: secretValue,
	}
	secret, err := m.createOrUpdateSecret(secretName, secretNamespace, data, annotations, owner, kind, field)
	if err != nil {
		return nil, fmt.Errorf("error creating secret for credential: %w", err)
	}
	return secret, nil
}

// CreateOrUpdateS3Secret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the S3BackupConfig credentials if there are any.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateS3Secret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.Services.Etcd.BackupConfig == nil || rkeConfig.Services.Etcd.BackupConfig.S3BackupConfig == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, rkeConfig.Services.Etcd.BackupConfig.S3BackupConfig.SecretKey, nil, owner, "cluster", "s3backup")
}

// CreateOrUpdateWeaveSecret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the Weave CNI password if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateWeaveSecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.Network.WeaveNetworkProvider == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, rkeConfig.Network.WeaveNetworkProvider.Password, nil, owner, "cluster", "weave")
}

// CreateOrUpdateVsphereGlobalSecret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the Vsphere global password if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateVsphereGlobalSecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.CloudProvider.VsphereCloudProvider == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, rkeConfig.CloudProvider.VsphereCloudProvider.Global.Password, nil, owner, "cluster", "vsphereglobal")
}

// CreateOrUpdateVsphereVirtualCenterSecret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the Vsphere VirtualCenter password if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateVsphereVirtualCenterSecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.CloudProvider.VsphereCloudProvider == nil {
		return nil, nil
	}
	data := map[string]string{}
	for k, v := range rkeConfig.CloudProvider.VsphereCloudProvider.VirtualCenter {
		if v.Password != "" {
			data[k] = v.Password
		}
	}
	if len(data) == 0 {
		return nil, nil
	}
	return m.createOrUpdateSecret(secretName, SecretNamespace, data, nil, owner, "cluster", "vspherevcenter")
}

// CreateOrUpdateOpenStackSecret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the OpenStack password if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateOpenStackSecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.CloudProvider.OpenstackCloudProvider == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, rkeConfig.CloudProvider.OpenstackCloudProvider.Global.Password, nil, owner, "cluster", "openstack")
}

// CreateOrUpdateAADClientSecret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the AAD client secret if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateAADClientSecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.CloudProvider.AzureCloudProvider == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, rkeConfig.CloudProvider.AzureCloudProvider.AADClientSecret, nil, owner, "cluster", "aadclientsecret")
}

// CreateOrUpdateAADCertSecret accepts an optional secret name and a RancherKubernetesEngineConfig object
// and creates a Secret for the AAD client cert password if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateAADCertSecret(secretName string, rkeConfig *rketypes.RancherKubernetesEngineConfig, owner runtime.Object) (*corev1.Secret, error) {
	if rkeConfig == nil || rkeConfig.CloudProvider.AzureCloudProvider == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, rkeConfig.CloudProvider.AzureCloudProvider.AADClientCertPassword, nil, owner, "cluster", "aadcert")
}

// CreateOrUpdateSMTPSecret accepts an optional secret name and an SMTPConfig object
// and creates a Secret for the SMTP server password if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateSMTPSecret(secretName string, smtpConfig *apimgmtv3.SMTPConfig, owner runtime.Object) (*corev1.Secret, error) {
	if smtpConfig == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, smtpConfig.Password, nil, owner, "notifier", "smtpconfig")
}

// CreateOrUpdateWechatSecret accepts an optional secret name and a WechatConfig object
// and creates a Secret for the Wechat credential if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateWechatSecret(secretName string, wechatConfig *apimgmtv3.WechatConfig, owner runtime.Object) (*corev1.Secret, error) {
	if wechatConfig == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, wechatConfig.Secret, nil, owner, "notifier", "wechatconfig")
}

// CreateOrUpdateDingtalkSecret accepts an optional secret name and a DingtalkConfig object
// and creates a Secret for the Dingtalk credential if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateDingtalkSecret(secretName string, dingtalkConfig *apimgmtv3.DingtalkConfig, owner runtime.Object) (*corev1.Secret, error) {
	if dingtalkConfig == nil {
		return nil, nil
	}
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, dingtalkConfig.Secret, nil, owner, "notifier", "dingtalkconfig")
}

// CreateOrUpdateSourceCodeProviderConfigSecret accepts an optional secret name and a client secret or
// private key for a SourceCodeProviderConfig and creates a Secret for the credential if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateSourceCodeProviderConfigSecret(secretName string, credential string, owner runtime.Object, provider string) (*corev1.Secret, error) {
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, credential, nil, owner, "sourcecodeproviderconfig", provider)
}

// CreateOrUpdateHarvesterCloudConfigSecret accepts an optional secret name and a client secret or
// harvester cloud-provider-config and creates a Secret for the credential if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateHarvesterCloudConfigSecret(secretName string, credential string, annotations map[string]string, owner runtime.Object, provider string) (*corev1.Secret, error) {
	return m.createOrUpdateSecretForCredential(secretName, fleet.ClustersDefaultNamespace, credential, annotations, owner, "harvester", provider)
}

// Cleanup deletes a secret if provided a secret name, otherwise does nothing.
func (m *Migrator) Cleanup(secretName string) error {
	if secretName == "" {
		return nil
	}
	_, err := m.secretLister.Get(namespace.GlobalNamespace, secretName)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	err = m.secrets.DeleteNamespaced(namespace.GlobalNamespace, secretName, &metav1.DeleteOptions{})
	return err
}

// CleanupKnownSecrets deletes a slice of secrets and logs any encountered errors at a WARNING level.
func (m *Migrator) CleanupKnownSecrets(secrets []*corev1.Secret) {
	for _, secret := range secrets {
		cleanUpErr := m.secrets.DeleteNamespaced(secret.Namespace, secret.Name, &metav1.DeleteOptions{})
		if cleanUpErr != nil {
			logrus.Warnf("[secretmigrator] error encountered while handling secrets cleanup for migration error; secret %s:%s may not have been cleaned up: %s", secret.Namespace, secret.Name, cleanUpErr)
		}
	}
}

// isHarvesterCluster determines if a v1.Cluster represents a harvester cluster
func (m *Migrator) isHarvesterCluster(cluster *v1.Cluster) bool {
	if cluster == nil || cluster.Spec.RKEConfig == nil {
		return false
	}

	for _, selectorConfig := range cluster.Spec.RKEConfig.MachineSelectorConfig {
		if strings.ToLower(convert.ToString(selectorConfig.Config.Data["cloud-provider-name"])) == "harvester" {
			return true
		}
	}

	return false
}

// CreateOrUpdateServiceAccountTokenSecret accepts an optional secret name and a token string
// and creates a Secret for the cluster service account token if there is one.
// If an owner is passed, the owner is set as an owner reference on the Secret.
// It returns a reference to the Secret if one was created. If the returned Secret is not nil and there is no error,
// the caller is responsible for un-setting the secret data, setting a reference to the Secret, and
// updating the Cluster object, if applicable.
func (m *Migrator) CreateOrUpdateServiceAccountTokenSecret(secretName string, credential string, owner runtime.Object) (*corev1.Secret, error) {
	return m.createOrUpdateSecretForCredential(secretName, SecretNamespace, credential, nil, owner, "cluster", "serviceaccounttoken")
}

// MatchesQuestionPath checks whether the given string matches the question-formatted path of the
// s3 secret, weave password, or registry password.
func MatchesQuestionPath(variable string) bool {
	return variable == S3BackupAnswersPath ||
		variable == WeavePasswordAnswersPath ||
		PrivateRegistryQuestion.MatchString(variable) ||
		variable == VsphereGlobalAnswersPath ||
		VcenterQuestion.MatchString(variable) ||
		variable == OpenStackAnswersPath ||
		variable == AADClientAnswersPath ||
		variable == AADCertAnswersPath
}

// cleanQuestions removes credentials from the questions and answers sections of the cluster object.
// Answers are already substituted into the spec in norman, so they can be deleted without migration.
func cleanQuestions(cluster *v3.Cluster) {
	cleanQuestions := func(questions []apimgmtv3.Question) {
		for i, q := range questions {
			if MatchesQuestionPath(q.Variable) {
				questions[i].Default = ""
			}
		}
	}
	if len(cluster.Spec.ClusterTemplateQuestions) > 0 {
		cleanQuestions(cluster.Spec.ClusterTemplateQuestions)
	}
	if len(cluster.Status.AppliedSpec.ClusterTemplateQuestions) > 0 {
		cleanQuestions(cluster.Status.AppliedSpec.ClusterTemplateQuestions)
	}
	if cluster.Status.FailedSpec != nil && len(cluster.Status.FailedSpec.ClusterTemplateQuestions) > 0 {
		cleanQuestions(cluster.Status.FailedSpec.ClusterTemplateQuestions)
	}
	cleanAnswers := func(answers *apimgmtv3.Answer) {
		for i := 0; ; i++ {
			key := fmt.Sprintf(RegistryPasswordAnswersPath, i)
			if _, ok := answers.Values[key]; !ok {
				break
			}
			delete(answers.Values, key)
		}
		if cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider != nil {
			vcenters := cluster.Spec.RancherKubernetesEngineConfig.CloudProvider.VsphereCloudProvider.VirtualCenter
			for k := range vcenters {
				key := fmt.Sprintf(VcenterAnswersPath, k)
				delete(answers.Values, key)
			}
		}
		delete(answers.Values, S3BackupAnswersPath)
		delete(answers.Values, WeavePasswordAnswersPath)
		delete(answers.Values, VsphereGlobalAnswersPath)
		delete(answers.Values, OpenStackAnswersPath)
		delete(answers.Values, AADClientAnswersPath)
		delete(answers.Values, AADCertAnswersPath)
	}
	if cluster.Spec.ClusterTemplateAnswers.Values != nil {
		cleanAnswers(&cluster.Spec.ClusterTemplateAnswers)
	}
	if cluster.Status.AppliedSpec.ClusterTemplateAnswers.Values != nil {
		cleanAnswers(&cluster.Status.AppliedSpec.ClusterTemplateAnswers)
	}
	if cluster.Status.FailedSpec != nil && cluster.Status.FailedSpec.ClusterTemplateAnswers.Values != nil {
		cleanAnswers(&cluster.Status.FailedSpec.ClusterTemplateAnswers)
	}
}

func setSourceCodeProviderConfigMetadata(m map[string]interface{}) (metav1.ObjectMeta, string, string, error) {
	objectMeta, err := pipelineutils.ObjectMetaFromUnstructureContent(m)
	if err != nil {
		return metav1.ObjectMeta{}, "", "", err
	}
	if objectMeta == nil {
		return metav1.ObjectMeta{}, "", "", fmt.Errorf("could not get ObjectMeta from sourcecodeproviderconfig")
	}
	return *objectMeta, "project.cattle.io/v3", pv3.SourceCodeProviderConfigGroupVersionKind.Kind, nil
}
