
package v1

import (
	"github.com/rancher/lasso/pkg/controller"
	v1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	controllers "github.com/rancher/rancher/pkg/generated/controllers/rke.cattle.io/v1"
	stevev1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	"github.com/rancher/rancher/tests/framework/pkg/steve/generic"
	"github.com/rancher/wrangler/pkg/schemes"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	schemes.Register(v1.AddToScheme)
}

type CustomMachineController interface {
	controllers.CustomMachineController
}

type ETCDSnapshotController interface {
	controllers.ETCDSnapshotController
}

type RKEBootstrapController interface {
	controllers.RKEBootstrapController
}

type RKEBootstrapTemplateController interface {
	controllers.RKEBootstrapTemplateController
}

type RKEClusterController interface {
	controllers.RKEClusterController
}

type RKEControlPlaneController interface {
	controllers.RKEControlPlaneController
}

type Interface interface { 
	CustomMachine() CustomMachineController
	ETCDSnapshot() ETCDSnapshotController
	RKEBootstrap() RKEBootstrapController
	RKEBootstrapTemplate() RKEBootstrapTemplateController
	RKECluster() RKEClusterController
	RKEControlPlane() RKEControlPlaneController
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


func (v *version) CustomMachine() CustomMachineController {
	return generic.NewController[*v1.CustomMachine, *v1.CustomMachineList](v.client, schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "CustomMachine"}, "custommachines", true, v.controllerFactory)
}

func (v *version) ETCDSnapshot() ETCDSnapshotController {
	return generic.NewController[*v1.ETCDSnapshot, *v1.ETCDSnapshotList](v.client, schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "ETCDSnapshot"}, "etcdsnapshots", true, v.controllerFactory)
}

func (v *version) RKEBootstrap() RKEBootstrapController {
	return generic.NewController[*v1.RKEBootstrap, *v1.RKEBootstrapList](v.client, schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "RKEBootstrap"}, "rkebootstraps", true, v.controllerFactory)
}

func (v *version) RKEBootstrapTemplate() RKEBootstrapTemplateController {
	return generic.NewController[*v1.RKEBootstrapTemplate, *v1.RKEBootstrapTemplateList](v.client, schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "RKEBootstrapTemplate"}, "rkebootstraptemplates", true, v.controllerFactory)
}

func (v *version) RKECluster() RKEClusterController {
	return generic.NewController[*v1.RKECluster, *v1.RKEClusterList](v.client, schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "RKECluster"}, "rkeclusters", true, v.controllerFactory)
}

func (v *version) RKEControlPlane() RKEControlPlaneController {
	return generic.NewController[*v1.RKEControlPlane, *v1.RKEControlPlaneList](v.client, schema.GroupVersionKind{Group: "rke.cattle.io", Version: "v1", Kind: "RKEControlPlane"}, "rkecontrolplanes", true, v.controllerFactory)
}

