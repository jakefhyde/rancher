package certrotation

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	provv1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/rancher/tests/framework/clients/rancher"
	v1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	"github.com/rancher/rancher/tests/framework/extensions/kubeconfig"
	"github.com/rancher/rancher/tests/framework/extensions/provisioninginput"
	"github.com/rancher/rancher/tests/framework/pkg/config"
	"github.com/rancher/rancher/tests/framework/pkg/session"
	"github.com/rancher/wrangler/pkg/generic"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
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

		clusterClient := SteveClientFactory[*provv1.Cluster, *provv1.ClusterList](adminClient.Steve, wranglerCtx.Provisioning.Cluster(), NewGeneratorForType(provv1.NewCluster), &provv1.Cluster{})
		rkeClient := SteveClientFactory[*rkev1.RKEControlPlane, *rkev1.RKEControlPlaneList](adminClient.Steve, wranglerCtx.RKE.RKEControlPlane(), NewGeneratorForType(rkev1.NewRKEControlPlane), &rkev1.RKEControlPlane{})

		require.NoError(r.T(), RotateCerts(clusterName, clusterClient, rkeClient))
		require.NoError(r.T(), RotateCerts(clusterName, clusterClient, rkeClient))
	})
}

type GeneratorFunc[T comparable] func(string, string, T) T

func NewGeneratorForType[T any](f func(string, string, T) *T) GeneratorFunc[*T] {
	return func(s string, s2 string, t *T) *T {
		return f(s, s2, *t)
	}
}

func TestCertRotation(t *testing.T) {
	suite.Run(t, new(V2ProvCertRotationTestSuite))
}

type SteveClient[T generic.RuntimeMetaObject, TList runtime.Object] struct {
	generic.ClientInterface[T, TList]
	client  generic.ClientInterface[T, TList]
	steve   *v1.Client
	objType reflect.Type
	gen     GeneratorFunc[T]
}

func (s *SteveClient[T, TList]) Watch(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return s.client.Watch(namespace, opts)
}

func (s *SteveClient[T, TList]) Get(namespace, name string, opts metav1.GetOptions) (T, error) {
	result := reflect.New(s.objType).Interface().(T)
	result = s.gen(namespace, name, result)

	gvk := result.GetObjectKind().GroupVersionKind()
	steveType := fmt.Sprintf("%s.%s", gvk.Group, strings.ToLower(gvk.Kind))

	apiObj, err := s.steve.SteveType(steveType).ByID(namespace + "/" + name)
	if err != nil {
		return result, err
	}

	err = v1.ConvertToK8sType(apiObj.JSONResp, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func (s *SteveClient[T, TList]) Create(t T) (T, error) {
	result := reflect.New(s.objType).Interface().(T)
	result = s.gen(t.GetNamespace(), t.GetName(), result)

	gvk := result.GetObjectKind().GroupVersionKind()
	steveType := fmt.Sprintf("%s.%s", gvk.Group, strings.ToLower(gvk.Kind))

	apiObj, err := s.steve.SteveType(steveType).Create(t)
	if err != nil {
		return result, err
	}

	err = v1.ConvertToK8sType(apiObj.JSONResp, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func (s *SteveClient[T, TList]) Update(t T) (T, error) {
	result := reflect.New(s.objType).Interface().(T)
	result = s.gen(t.GetNamespace(), t.GetName(), result)

	gvk := result.GetObjectKind().GroupVersionKind()
	steveType := fmt.Sprintf("%s.%s", gvk.Group, strings.ToLower(gvk.Kind))

	apiObj, err := s.steve.SteveType(steveType).ByID(t.GetNamespace() + "/" + t.GetName())
	if err != nil {
		return result, err
	}

	apiObj, err = s.steve.SteveType(steveType).Update(apiObj, t)
	if err != nil {
		return result, err
	}

	err = v1.ConvertToK8sType(apiObj.JSONResp, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func SteveClientFactory[T generic.RuntimeMetaObject, TList runtime.Object](steve *v1.Client, c generic.ClientInterface[T, TList], g GeneratorFunc[T], t T) *SteveClient[T, TList] {
	return &SteveClient[T, TList]{
		client:  c,
		steve:   steve,
		gen:     g,
		objType: reflect.TypeOf(t).Elem(),
	}
}
