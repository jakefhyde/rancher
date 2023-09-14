
package v3

import (
	"github.com/rancher/lasso/pkg/controller"
	v3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	controllers "github.com/rancher/rancher/pkg/generated/controllers/management.cattle.io/v3"
	stevev1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	"github.com/rancher/rancher/tests/framework/pkg/steve/generic"
	"github.com/rancher/wrangler/pkg/schemes"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	schemes.Register(v3.AddToScheme)
}

type APIServiceController interface {
	controllers.APIServiceController
}

type ActiveDirectoryProviderController interface {
	controllers.ActiveDirectoryProviderController
}

type AuthConfigController interface {
	controllers.AuthConfigController
}

type AuthProviderController interface {
	controllers.AuthProviderController
}

type AuthTokenController interface {
	controllers.AuthTokenController
}

type AzureADProviderController interface {
	controllers.AzureADProviderController
}

type CatalogController interface {
	controllers.CatalogController
}

type CatalogTemplateController interface {
	controllers.CatalogTemplateController
}

type CatalogTemplateVersionController interface {
	controllers.CatalogTemplateVersionController
}

type CloudCredentialController interface {
	controllers.CloudCredentialController
}

type ClusterController interface {
	controllers.ClusterController
}

type ClusterAlertController interface {
	controllers.ClusterAlertController
}

type ClusterAlertGroupController interface {
	controllers.ClusterAlertGroupController
}

type ClusterAlertRuleController interface {
	controllers.ClusterAlertRuleController
}

type ClusterLoggingController interface {
	controllers.ClusterLoggingController
}

type ClusterMonitorGraphController interface {
	controllers.ClusterMonitorGraphController
}

type ClusterRegistrationTokenController interface {
	controllers.ClusterRegistrationTokenController
}

type ClusterRoleTemplateBindingController interface {
	controllers.ClusterRoleTemplateBindingController
}

type ClusterTemplateController interface {
	controllers.ClusterTemplateController
}

type ClusterTemplateRevisionController interface {
	controllers.ClusterTemplateRevisionController
}

type ComposeConfigController interface {
	controllers.ComposeConfigController
}

type DynamicSchemaController interface {
	controllers.DynamicSchemaController
}

type EtcdBackupController interface {
	controllers.EtcdBackupController
}

type FeatureController interface {
	controllers.FeatureController
}

type FleetWorkspaceController interface {
	controllers.FleetWorkspaceController
}

type FreeIpaProviderController interface {
	controllers.FreeIpaProviderController
}

type GithubProviderController interface {
	controllers.GithubProviderController
}

type GlobalDnsController interface {
	controllers.GlobalDnsController
}

type GlobalDnsProviderController interface {
	controllers.GlobalDnsProviderController
}

type GlobalRoleController interface {
	controllers.GlobalRoleController
}

type GlobalRoleBindingController interface {
	controllers.GlobalRoleBindingController
}

type GoogleOAuthProviderController interface {
	controllers.GoogleOAuthProviderController
}

type GroupController interface {
	controllers.GroupController
}

type GroupMemberController interface {
	controllers.GroupMemberController
}

type KontainerDriverController interface {
	controllers.KontainerDriverController
}

type LocalProviderController interface {
	controllers.LocalProviderController
}

type ManagedChartController interface {
	controllers.ManagedChartController
}

type MonitorMetricController interface {
	controllers.MonitorMetricController
}

type MultiClusterAppController interface {
	controllers.MultiClusterAppController
}

type MultiClusterAppRevisionController interface {
	controllers.MultiClusterAppRevisionController
}

type NodeController interface {
	controllers.NodeController
}

type NodeDriverController interface {
	controllers.NodeDriverController
}

type NodePoolController interface {
	controllers.NodePoolController
}

type NodeTemplateController interface {
	controllers.NodeTemplateController
}

type NotifierController interface {
	controllers.NotifierController
}

type OIDCProviderController interface {
	controllers.OIDCProviderController
}

type OpenLdapProviderController interface {
	controllers.OpenLdapProviderController
}

type PodSecurityAdmissionConfigurationTemplateController interface {
	controllers.PodSecurityAdmissionConfigurationTemplateController
}

type PodSecurityPolicyTemplateController interface {
	controllers.PodSecurityPolicyTemplateController
}

type PodSecurityPolicyTemplateProjectBindingController interface {
	controllers.PodSecurityPolicyTemplateProjectBindingController
}

type PreferenceController interface {
	controllers.PreferenceController
}

type PrincipalController interface {
	controllers.PrincipalController
}

type ProjectController interface {
	controllers.ProjectController
}

type ProjectAlertController interface {
	controllers.ProjectAlertController
}

type ProjectAlertGroupController interface {
	controllers.ProjectAlertGroupController
}

type ProjectAlertRuleController interface {
	controllers.ProjectAlertRuleController
}

type ProjectLoggingController interface {
	controllers.ProjectLoggingController
}

type ProjectMonitorGraphController interface {
	controllers.ProjectMonitorGraphController
}

type ProjectNetworkPolicyController interface {
	controllers.ProjectNetworkPolicyController
}

type ProjectRoleTemplateBindingController interface {
	controllers.ProjectRoleTemplateBindingController
}

type RancherUserNotificationController interface {
	controllers.RancherUserNotificationController
}

type RkeAddonController interface {
	controllers.RkeAddonController
}

type RkeK8sServiceOptionController interface {
	controllers.RkeK8sServiceOptionController
}

type RkeK8sSystemImageController interface {
	controllers.RkeK8sSystemImageController
}

type RoleTemplateController interface {
	controllers.RoleTemplateController
}

type SamlProviderController interface {
	controllers.SamlProviderController
}

type SamlTokenController interface {
	controllers.SamlTokenController
}

type SettingController interface {
	controllers.SettingController
}

type TemplateController interface {
	controllers.TemplateController
}

type TemplateContentController interface {
	controllers.TemplateContentController
}

type TemplateVersionController interface {
	controllers.TemplateVersionController
}

type TokenController interface {
	controllers.TokenController
}

type UserController interface {
	controllers.UserController
}

type UserAttributeController interface {
	controllers.UserAttributeController
}

type Interface interface { 
	APIService() APIServiceController
	ActiveDirectoryProvider() ActiveDirectoryProviderController
	AuthConfig() AuthConfigController
	AuthProvider() AuthProviderController
	AuthToken() AuthTokenController
	AzureADProvider() AzureADProviderController
	Catalog() CatalogController
	CatalogTemplate() CatalogTemplateController
	CatalogTemplateVersion() CatalogTemplateVersionController
	CloudCredential() CloudCredentialController
	Cluster() ClusterController
	ClusterAlert() ClusterAlertController
	ClusterAlertGroup() ClusterAlertGroupController
	ClusterAlertRule() ClusterAlertRuleController
	ClusterLogging() ClusterLoggingController
	ClusterMonitorGraph() ClusterMonitorGraphController
	ClusterRegistrationToken() ClusterRegistrationTokenController
	ClusterRoleTemplateBinding() ClusterRoleTemplateBindingController
	ClusterTemplate() ClusterTemplateController
	ClusterTemplateRevision() ClusterTemplateRevisionController
	ComposeConfig() ComposeConfigController
	DynamicSchema() DynamicSchemaController
	EtcdBackup() EtcdBackupController
	Feature() FeatureController
	FleetWorkspace() FleetWorkspaceController
	FreeIpaProvider() FreeIpaProviderController
	GithubProvider() GithubProviderController
	GlobalDns() GlobalDnsController
	GlobalDnsProvider() GlobalDnsProviderController
	GlobalRole() GlobalRoleController
	GlobalRoleBinding() GlobalRoleBindingController
	GoogleOAuthProvider() GoogleOAuthProviderController
	Group() GroupController
	GroupMember() GroupMemberController
	KontainerDriver() KontainerDriverController
	LocalProvider() LocalProviderController
	ManagedChart() ManagedChartController
	MonitorMetric() MonitorMetricController
	MultiClusterApp() MultiClusterAppController
	MultiClusterAppRevision() MultiClusterAppRevisionController
	Node() NodeController
	NodeDriver() NodeDriverController
	NodePool() NodePoolController
	NodeTemplate() NodeTemplateController
	Notifier() NotifierController
	OIDCProvider() OIDCProviderController
	OpenLdapProvider() OpenLdapProviderController
	PodSecurityAdmissionConfigurationTemplate() PodSecurityAdmissionConfigurationTemplateController
	PodSecurityPolicyTemplate() PodSecurityPolicyTemplateController
	PodSecurityPolicyTemplateProjectBinding() PodSecurityPolicyTemplateProjectBindingController
	Preference() PreferenceController
	Principal() PrincipalController
	Project() ProjectController
	ProjectAlert() ProjectAlertController
	ProjectAlertGroup() ProjectAlertGroupController
	ProjectAlertRule() ProjectAlertRuleController
	ProjectLogging() ProjectLoggingController
	ProjectMonitorGraph() ProjectMonitorGraphController
	ProjectNetworkPolicy() ProjectNetworkPolicyController
	ProjectRoleTemplateBinding() ProjectRoleTemplateBindingController
	RancherUserNotification() RancherUserNotificationController
	RkeAddon() RkeAddonController
	RkeK8sServiceOption() RkeK8sServiceOptionController
	RkeK8sSystemImage() RkeK8sSystemImageController
	RoleTemplate() RoleTemplateController
	SamlProvider() SamlProviderController
	SamlToken() SamlTokenController
	Setting() SettingController
	Template() TemplateController
	TemplateContent() TemplateContentController
	TemplateVersion() TemplateVersionController
	Token() TokenController
	User() UserController
	UserAttribute() UserAttributeController
}

func New(controllerFactory controller.SharedControllerFactory, client *stevev1.Client) Interface {
	return &version{
		controllerFactory: controllerFactory,
		client:            client,
	}
}

type version struct {
	controllerFactory controller.SharedControllerFactory
	client            *stevev1.Client
}


func (v *version) APIService() APIServiceController {
	return generic.NewController[*v1.APIService, *v1.APIServiceList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "APIService"}, "apiservices", true, v.controllerFactory)
}

func (v *version) ActiveDirectoryProvider() ActiveDirectoryProviderController {
	return generic.NewController[*v1.ActiveDirectoryProvider, *v1.ActiveDirectoryProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ActiveDirectoryProvider"}, "activedirectoryproviders", true, v.controllerFactory)
}

func (v *version) AuthConfig() AuthConfigController {
	return generic.NewController[*v1.AuthConfig, *v1.AuthConfigList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "AuthConfig"}, "authconfigs", true, v.controllerFactory)
}

func (v *version) AuthProvider() AuthProviderController {
	return generic.NewController[*v1.AuthProvider, *v1.AuthProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "AuthProvider"}, "authproviders", true, v.controllerFactory)
}

func (v *version) AuthToken() AuthTokenController {
	return generic.NewController[*v1.AuthToken, *v1.AuthTokenList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "AuthToken"}, "authtokens", true, v.controllerFactory)
}

func (v *version) AzureADProvider() AzureADProviderController {
	return generic.NewController[*v1.AzureADProvider, *v1.AzureADProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "AzureADProvider"}, "azureadproviders", true, v.controllerFactory)
}

func (v *version) Catalog() CatalogController {
	return generic.NewController[*v1.Catalog, *v1.CatalogList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Catalog"}, "catalogs", true, v.controllerFactory)
}

func (v *version) CatalogTemplate() CatalogTemplateController {
	return generic.NewController[*v1.CatalogTemplate, *v1.CatalogTemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "CatalogTemplate"}, "catalogtemplates", true, v.controllerFactory)
}

func (v *version) CatalogTemplateVersion() CatalogTemplateVersionController {
	return generic.NewController[*v1.CatalogTemplateVersion, *v1.CatalogTemplateVersionList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "CatalogTemplateVersion"}, "catalogtemplateversions", true, v.controllerFactory)
}

func (v *version) CloudCredential() CloudCredentialController {
	return generic.NewController[*v1.CloudCredential, *v1.CloudCredentialList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "CloudCredential"}, "cloudcredentials", true, v.controllerFactory)
}

func (v *version) Cluster() ClusterController {
	return generic.NewController[*v1.Cluster, *v1.ClusterList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Cluster"}, "clusters", true, v.controllerFactory)
}

func (v *version) ClusterAlert() ClusterAlertController {
	return generic.NewController[*v1.ClusterAlert, *v1.ClusterAlertList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterAlert"}, "clusteralerts", true, v.controllerFactory)
}

func (v *version) ClusterAlertGroup() ClusterAlertGroupController {
	return generic.NewController[*v1.ClusterAlertGroup, *v1.ClusterAlertGroupList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterAlertGroup"}, "clusteralertgroups", true, v.controllerFactory)
}

func (v *version) ClusterAlertRule() ClusterAlertRuleController {
	return generic.NewController[*v1.ClusterAlertRule, *v1.ClusterAlertRuleList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterAlertRule"}, "clusteralertrules", true, v.controllerFactory)
}

func (v *version) ClusterLogging() ClusterLoggingController {
	return generic.NewController[*v1.ClusterLogging, *v1.ClusterLoggingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterLogging"}, "clusterloggings", true, v.controllerFactory)
}

func (v *version) ClusterMonitorGraph() ClusterMonitorGraphController {
	return generic.NewController[*v1.ClusterMonitorGraph, *v1.ClusterMonitorGraphList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterMonitorGraph"}, "clustermonitorgraphs", true, v.controllerFactory)
}

func (v *version) ClusterRegistrationToken() ClusterRegistrationTokenController {
	return generic.NewController[*v1.ClusterRegistrationToken, *v1.ClusterRegistrationTokenList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterRegistrationToken"}, "clusterregistrationtokens", true, v.controllerFactory)
}

func (v *version) ClusterRoleTemplateBinding() ClusterRoleTemplateBindingController {
	return generic.NewController[*v1.ClusterRoleTemplateBinding, *v1.ClusterRoleTemplateBindingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterRoleTemplateBinding"}, "clusterroletemplatebindings", true, v.controllerFactory)
}

func (v *version) ClusterTemplate() ClusterTemplateController {
	return generic.NewController[*v1.ClusterTemplate, *v1.ClusterTemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterTemplate"}, "clustertemplates", true, v.controllerFactory)
}

func (v *version) ClusterTemplateRevision() ClusterTemplateRevisionController {
	return generic.NewController[*v1.ClusterTemplateRevision, *v1.ClusterTemplateRevisionList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ClusterTemplateRevision"}, "clustertemplaterevisions", true, v.controllerFactory)
}

func (v *version) ComposeConfig() ComposeConfigController {
	return generic.NewController[*v1.ComposeConfig, *v1.ComposeConfigList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ComposeConfig"}, "composeconfigs", true, v.controllerFactory)
}

func (v *version) DynamicSchema() DynamicSchemaController {
	return generic.NewController[*v1.DynamicSchema, *v1.DynamicSchemaList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "DynamicSchema"}, "dynamicschemas", true, v.controllerFactory)
}

func (v *version) EtcdBackup() EtcdBackupController {
	return generic.NewController[*v1.EtcdBackup, *v1.EtcdBackupList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "EtcdBackup"}, "etcdbackups", true, v.controllerFactory)
}

func (v *version) Feature() FeatureController {
	return generic.NewController[*v1.Feature, *v1.FeatureList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Feature"}, "features", true, v.controllerFactory)
}

func (v *version) FleetWorkspace() FleetWorkspaceController {
	return generic.NewController[*v1.FleetWorkspace, *v1.FleetWorkspaceList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "FleetWorkspace"}, "fleetworkspaces", true, v.controllerFactory)
}

func (v *version) FreeIpaProvider() FreeIpaProviderController {
	return generic.NewController[*v1.FreeIpaProvider, *v1.FreeIpaProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "FreeIpaProvider"}, "freeipaproviders", true, v.controllerFactory)
}

func (v *version) GithubProvider() GithubProviderController {
	return generic.NewController[*v1.GithubProvider, *v1.GithubProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GithubProvider"}, "githubproviders", true, v.controllerFactory)
}

func (v *version) GlobalDns() GlobalDnsController {
	return generic.NewController[*v1.GlobalDns, *v1.GlobalDnsList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GlobalDns"}, "globaldnss", true, v.controllerFactory)
}

func (v *version) GlobalDnsProvider() GlobalDnsProviderController {
	return generic.NewController[*v1.GlobalDnsProvider, *v1.GlobalDnsProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GlobalDnsProvider"}, "globaldnsproviders", true, v.controllerFactory)
}

func (v *version) GlobalRole() GlobalRoleController {
	return generic.NewController[*v1.GlobalRole, *v1.GlobalRoleList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GlobalRole"}, "globalroles", true, v.controllerFactory)
}

func (v *version) GlobalRoleBinding() GlobalRoleBindingController {
	return generic.NewController[*v1.GlobalRoleBinding, *v1.GlobalRoleBindingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GlobalRoleBinding"}, "globalrolebindings", true, v.controllerFactory)
}

func (v *version) GoogleOAuthProvider() GoogleOAuthProviderController {
	return generic.NewController[*v1.GoogleOAuthProvider, *v1.GoogleOAuthProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GoogleOAuthProvider"}, "googleoauthproviders", true, v.controllerFactory)
}

func (v *version) Group() GroupController {
	return generic.NewController[*v1.Group, *v1.GroupList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Group"}, "groups", true, v.controllerFactory)
}

func (v *version) GroupMember() GroupMemberController {
	return generic.NewController[*v1.GroupMember, *v1.GroupMemberList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "GroupMember"}, "groupmembers", true, v.controllerFactory)
}

func (v *version) KontainerDriver() KontainerDriverController {
	return generic.NewController[*v1.KontainerDriver, *v1.KontainerDriverList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "KontainerDriver"}, "kontainerdrivers", true, v.controllerFactory)
}

func (v *version) LocalProvider() LocalProviderController {
	return generic.NewController[*v1.LocalProvider, *v1.LocalProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "LocalProvider"}, "localproviders", true, v.controllerFactory)
}

func (v *version) ManagedChart() ManagedChartController {
	return generic.NewController[*v1.ManagedChart, *v1.ManagedChartList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ManagedChart"}, "managedcharts", true, v.controllerFactory)
}

func (v *version) MonitorMetric() MonitorMetricController {
	return generic.NewController[*v1.MonitorMetric, *v1.MonitorMetricList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "MonitorMetric"}, "monitormetrics", true, v.controllerFactory)
}

func (v *version) MultiClusterApp() MultiClusterAppController {
	return generic.NewController[*v1.MultiClusterApp, *v1.MultiClusterAppList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "MultiClusterApp"}, "multiclusterapps", true, v.controllerFactory)
}

func (v *version) MultiClusterAppRevision() MultiClusterAppRevisionController {
	return generic.NewController[*v1.MultiClusterAppRevision, *v1.MultiClusterAppRevisionList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "MultiClusterAppRevision"}, "multiclusterapprevisions", true, v.controllerFactory)
}

func (v *version) Node() NodeController {
	return generic.NewController[*v1.Node, *v1.NodeList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Node"}, "nodes", true, v.controllerFactory)
}

func (v *version) NodeDriver() NodeDriverController {
	return generic.NewController[*v1.NodeDriver, *v1.NodeDriverList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "NodeDriver"}, "nodedrivers", true, v.controllerFactory)
}

func (v *version) NodePool() NodePoolController {
	return generic.NewController[*v1.NodePool, *v1.NodePoolList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "NodePool"}, "nodepools", true, v.controllerFactory)
}

func (v *version) NodeTemplate() NodeTemplateController {
	return generic.NewController[*v1.NodeTemplate, *v1.NodeTemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "NodeTemplate"}, "nodetemplates", true, v.controllerFactory)
}

func (v *version) Notifier() NotifierController {
	return generic.NewController[*v1.Notifier, *v1.NotifierList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Notifier"}, "notifiers", true, v.controllerFactory)
}

func (v *version) OIDCProvider() OIDCProviderController {
	return generic.NewController[*v1.OIDCProvider, *v1.OIDCProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "OIDCProvider"}, "oidcproviders", true, v.controllerFactory)
}

func (v *version) OpenLdapProvider() OpenLdapProviderController {
	return generic.NewController[*v1.OpenLdapProvider, *v1.OpenLdapProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "OpenLdapProvider"}, "openldapproviders", true, v.controllerFactory)
}

func (v *version) PodSecurityAdmissionConfigurationTemplate() PodSecurityAdmissionConfigurationTemplateController {
	return generic.NewController[*v1.PodSecurityAdmissionConfigurationTemplate, *v1.PodSecurityAdmissionConfigurationTemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "PodSecurityAdmissionConfigurationTemplate"}, "podsecurityadmissionconfigurationtemplates", true, v.controllerFactory)
}

func (v *version) PodSecurityPolicyTemplate() PodSecurityPolicyTemplateController {
	return generic.NewController[*v1.PodSecurityPolicyTemplate, *v1.PodSecurityPolicyTemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "PodSecurityPolicyTemplate"}, "podsecuritypolicytemplates", true, v.controllerFactory)
}

func (v *version) PodSecurityPolicyTemplateProjectBinding() PodSecurityPolicyTemplateProjectBindingController {
	return generic.NewController[*v1.PodSecurityPolicyTemplateProjectBinding, *v1.PodSecurityPolicyTemplateProjectBindingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "PodSecurityPolicyTemplateProjectBinding"}, "podsecuritypolicytemplateprojectbindings", true, v.controllerFactory)
}

func (v *version) Preference() PreferenceController {
	return generic.NewController[*v1.Preference, *v1.PreferenceList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Preference"}, "preferences", true, v.controllerFactory)
}

func (v *version) Principal() PrincipalController {
	return generic.NewController[*v1.Principal, *v1.PrincipalList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Principal"}, "principals", true, v.controllerFactory)
}

func (v *version) Project() ProjectController {
	return generic.NewController[*v1.Project, *v1.ProjectList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Project"}, "projects", true, v.controllerFactory)
}

func (v *version) ProjectAlert() ProjectAlertController {
	return generic.NewController[*v1.ProjectAlert, *v1.ProjectAlertList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectAlert"}, "projectalerts", true, v.controllerFactory)
}

func (v *version) ProjectAlertGroup() ProjectAlertGroupController {
	return generic.NewController[*v1.ProjectAlertGroup, *v1.ProjectAlertGroupList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectAlertGroup"}, "projectalertgroups", true, v.controllerFactory)
}

func (v *version) ProjectAlertRule() ProjectAlertRuleController {
	return generic.NewController[*v1.ProjectAlertRule, *v1.ProjectAlertRuleList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectAlertRule"}, "projectalertrules", true, v.controllerFactory)
}

func (v *version) ProjectLogging() ProjectLoggingController {
	return generic.NewController[*v1.ProjectLogging, *v1.ProjectLoggingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectLogging"}, "projectloggings", true, v.controllerFactory)
}

func (v *version) ProjectMonitorGraph() ProjectMonitorGraphController {
	return generic.NewController[*v1.ProjectMonitorGraph, *v1.ProjectMonitorGraphList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectMonitorGraph"}, "projectmonitorgraphs", true, v.controllerFactory)
}

func (v *version) ProjectNetworkPolicy() ProjectNetworkPolicyController {
	return generic.NewController[*v1.ProjectNetworkPolicy, *v1.ProjectNetworkPolicyList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectNetworkPolicy"}, "projectnetworkpolicys", true, v.controllerFactory)
}

func (v *version) ProjectRoleTemplateBinding() ProjectRoleTemplateBindingController {
	return generic.NewController[*v1.ProjectRoleTemplateBinding, *v1.ProjectRoleTemplateBindingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "ProjectRoleTemplateBinding"}, "projectroletemplatebindings", true, v.controllerFactory)
}

func (v *version) RancherUserNotification() RancherUserNotificationController {
	return generic.NewController[*v1.RancherUserNotification, *v1.RancherUserNotificationList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "RancherUserNotification"}, "rancherusernotifications", true, v.controllerFactory)
}

func (v *version) RkeAddon() RkeAddonController {
	return generic.NewController[*v1.RkeAddon, *v1.RkeAddonList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "RkeAddon"}, "rkeaddons", true, v.controllerFactory)
}

func (v *version) RkeK8sServiceOption() RkeK8sServiceOptionController {
	return generic.NewController[*v1.RkeK8sServiceOption, *v1.RkeK8sServiceOptionList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "RkeK8sServiceOption"}, "rkek8sserviceoptions", true, v.controllerFactory)
}

func (v *version) RkeK8sSystemImage() RkeK8sSystemImageController {
	return generic.NewController[*v1.RkeK8sSystemImage, *v1.RkeK8sSystemImageList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "RkeK8sSystemImage"}, "rkek8ssystemimages", true, v.controllerFactory)
}

func (v *version) RoleTemplate() RoleTemplateController {
	return generic.NewController[*v1.RoleTemplate, *v1.RoleTemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "RoleTemplate"}, "roletemplates", true, v.controllerFactory)
}

func (v *version) SamlProvider() SamlProviderController {
	return generic.NewController[*v1.SamlProvider, *v1.SamlProviderList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "SamlProvider"}, "samlproviders", true, v.controllerFactory)
}

func (v *version) SamlToken() SamlTokenController {
	return generic.NewController[*v1.SamlToken, *v1.SamlTokenList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "SamlToken"}, "samltokens", true, v.controllerFactory)
}

func (v *version) Setting() SettingController {
	return generic.NewController[*v1.Setting, *v1.SettingList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Setting"}, "settings", true, v.controllerFactory)
}

func (v *version) Template() TemplateController {
	return generic.NewController[*v1.Template, *v1.TemplateList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Template"}, "templates", true, v.controllerFactory)
}

func (v *version) TemplateContent() TemplateContentController {
	return generic.NewController[*v1.TemplateContent, *v1.TemplateContentList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "TemplateContent"}, "templatecontents", true, v.controllerFactory)
}

func (v *version) TemplateVersion() TemplateVersionController {
	return generic.NewController[*v1.TemplateVersion, *v1.TemplateVersionList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "TemplateVersion"}, "templateversions", true, v.controllerFactory)
}

func (v *version) Token() TokenController {
	return generic.NewController[*v1.Token, *v1.TokenList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "Token"}, "tokens", true, v.controllerFactory)
}

func (v *version) User() UserController {
	return generic.NewController[*v1.User, *v1.UserList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "User"}, "users", true, v.controllerFactory)
}

func (v *version) UserAttribute() UserAttributeController {
	return generic.NewController[*v1.UserAttribute, *v1.UserAttributeList](v.client, schema.GroupVersionKind{Group: "management.cattle.io", Version: "v3", Kind: "UserAttribute"}, "userattributes", true, v.controllerFactory)
}

