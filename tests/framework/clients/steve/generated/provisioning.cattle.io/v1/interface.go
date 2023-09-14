
package v1

import (
	"github.com/rancher/lasso/pkg/controller"
	v1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	controllers "github.com/rancher/rancher/pkg/generated/controllers/provisioning.cattle.io/v1"
	stevev1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	"github.com/rancher/rancher/tests/framework/pkg/session"
	"github.com/rancher/rancher/tests/framework/pkg/steve/generic"
	"github.com/rancher/wrangler/pkg/schemes"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	schemes.Register(v1.AddToScheme)
}

type ClusterController interface {
	controllers.ClusterController
}

type Interface interface { 
	Cluster() ClusterController
}

func New(controllerFactory controller.SharedControllerFactory, client *stevev1.Client, session *session.Session) Interface {
	return &version{
		controllerFactory: controllerFactory,
		client:            client,
		session:					 session,
	}
}

type version struct {
	controllerFactory controller.SharedControllerFactory
	client            *stevev1.Client
	session					 	*session.Session
}


func (v *version) Cluster() ClusterController {
	return generic.NewController[*v1.Cluster, *v1.ClusterList](v.client, v.session, schema.GroupVersionKind{Group: "provisioning.cattle.io", Version: "v1", Kind: "Cluster"}, "clusters", true, v.controllerFactory)
}

