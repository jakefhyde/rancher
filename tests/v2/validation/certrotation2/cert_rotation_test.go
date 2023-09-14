package certrotation

import (
	"context"
	"testing"

	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/rancher/tests/framework/clients/rancher"
	"github.com/rancher/rancher/tests/framework/extensions/kubeconfig"
	"github.com/rancher/rancher/tests/framework/extensions/provisioninginput"
	"github.com/rancher/rancher/tests/framework/pkg/config"
	"github.com/rancher/rancher/tests/framework/pkg/session"
	"github.com/rancher/rancher/tests/framework/pkg/steve"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type V2ProvCertRotationTestSuite struct {
	suite.Suite
	session        *session.Session
	client         *rancher.Client
	clustersConfig *provisioninginput.Config
}

func (r *V2ProvCertRotationTestSuite) TearDownSuite() {
	r.session.Cleanup()
}

func (r *V2ProvCertRotationTestSuite) SetupSuite() {
	testSession := session.NewSession()
	r.session = testSession

	r.clustersConfig = new(provisioninginput.Config)
	config.LoadConfig(provisioninginput.ConfigurationFileKey, r.clustersConfig)

	client, err := rancher.NewClient("", testSession)
	require.NoError(r.T(), err)

	r.client = client
}

func (r *V2ProvCertRotationTestSuite) TestCertRotation() {
	r.Run("test-cert-rotation", func() {
		adminClient, err := rancher.NewClient(r.client.RancherConfig.AdminToken, r.client.Session)
		require.NoError(r.T(), err)

		clusterName := r.client.RancherConfig.ClusterName

		provClient, err := adminClient.GetKubeAPIProvisioningClient()
		require.NoError(r.T(), err)

		cluster, err := provClient.Clusters(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
		require.NoError(r.T(), err)

		kubeConfig, err := kubeconfig.GetKubeconfig(r.client, cluster.Status.ClusterName)
		require.NoError(r.T(), err)

		wranglerCtx, err := wrangler.NewContext(context.TODO(), *kubeConfig, r.client.RestConfig)
		require.NoError(r.T(), err)

		steveCtx, err := steve.NewContext(wranglerCtx, adminClient.Steve)
		require.NoError(r.T(), err)

		require.NoError(r.T(), RotateCerts(clusterName, steveCtx.Provisioning.Cluster(), steveCtx.RKE.RKEControlPlane()))
		require.NoError(r.T(), RotateCerts(clusterName, steveCtx.Provisioning.Cluster(), steveCtx.RKE.RKEControlPlane()))
	})
}

func (r *V2ProvCertRotationTestSuite) a() {

}

//// Option a, has the neatest interface
//func (r *V2ProvCertRotationTestSuite) a() {
//	wranglerCtx, err := wrangler.NewContext(context.TODO(), *kubeConfig, r.client.RestConfig)
//	require.NoError(r.T(), err)
//
//	steveCtx, err := steve.NewContext(adminClient.Steve, wranglerCtx)
//	require.NoError(r.T(), err)
//
//	require.NoError(r.T(), RotateCerts(clusterName, steveCtx.Provisioning.Cluster(), steveCtx.RKE.RKEControlPlane()))
//	require.NoError(r.T(), RotateCerts(clusterName, steveCtx.Provisioning.Cluster(), steveCtx.RKE.RKEControlPlane()))
//}
//
//// Option b, has the neatest interface
//func (r *V2ProvCertRotationTestSuite) a() {
//	wranglerCtx, err := wrangler.NewContext(context.TODO(), *kubeConfig, r.client.RestConfig)
//	require.NoError(r.T(), err)
//
//	clusterClient := steve.ClientFactory[*provv1.Cluster, *provv1.ClusterList](adminClient.Steve, wranglerCtx.Provisioning.Cluster(), steve.NewGeneratorForType(provv1.NewCluster), &provv1.Cluster{})
//	rkeClient := steve.ClientFactory[*rkev1.RKEControlPlane, *rkev1.RKEControlPlaneList](adminClient.Steve, wranglerCtx.RKE.RKEControlPlane(), steve.NewGeneratorForType(rkev1.NewRKEControlPlane), &rkev1.RKEControlPlane{})
//
//	steveCtx, err := steve.NewContext(adminClient.Steve, wranglerCtx)
//	require.NoError(r.T(), err)
//
//	require.NoError(r.T(), RotateCerts(clusterName, steveCtx.ClusterClient, rkeClient))
//	require.NoError(r.T(), RotateCerts(clusterName, clusterClient, rkeClient))
//}

func TestCertRotation(t *testing.T) {
	suite.Run(t, new(V2ProvCertRotationTestSuite))
}
