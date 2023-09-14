package steve

import (
	"github.com/rancher/rancher/pkg/wrangler"
	v1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	managementv3 "github.com/rancher/rancher/tests/framework/clients/steve/generated/management.cattle.io/v3"
	provisioningv1 "github.com/rancher/rancher/tests/framework/clients/steve/generated/provisioning.cattle.io/v1"
	rkev1 "github.com/rancher/rancher/tests/framework/clients/steve/generated/rke.cattle.io/v1"
	"github.com/rancher/rancher/tests/framework/pkg/session"
)

type Context struct {
	Management   managementv3.Interface
	Provisioning provisioningv1.Interface
	RKE          rkev1.Interface
}

func NewContext(wranglerCtx *wrangler.Context, steveClient *v1.Client, session *session.Session) (*Context, error) {
	return &Context{
		Management:   managementv3.New(wranglerCtx.SharedControllerFactory, steveClient, session),
		Provisioning: provisioningv1.New(wranglerCtx.SharedControllerFactory, steveClient, session),
		RKE:          rkev1.New(wranglerCtx.SharedControllerFactory, steveClient, session),
	}, nil
}
