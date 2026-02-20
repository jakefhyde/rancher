package capr

import (
	"context"

	"github.com/rancher/rancher/pkg/capr"
	"github.com/rancher/rancher/pkg/capr/planner"
	rkebootstrapcontrollers "github.com/rancher/rancher/pkg/cluster-api-provider-rancher/bootstrap/controllers"
	rkeinfracontrollers "github.com/rancher/rancher/pkg/cluster-api-provider-rancher/controllers"
	rkecontrolplanecontroller "github.com/rancher/rancher/pkg/cluster-api-provider-rancher/controlplane/controllers"
	"github.com/rancher/rancher/pkg/controllers/capr/autoscaler"
	"github.com/rancher/rancher/pkg/controllers/capr/dynamicschema"
	"github.com/rancher/rancher/pkg/controllers/capr/machinedrain"
	"github.com/rancher/rancher/pkg/controllers/capr/machinenodelookup"
	"github.com/rancher/rancher/pkg/controllers/capr/managesystemagent"
	"github.com/rancher/rancher/pkg/controllers/capr/plansecret"
	"github.com/rancher/rancher/pkg/controllers/capr/unmanaged"
	"github.com/rancher/rancher/pkg/features"
	"github.com/rancher/rancher/pkg/provisioningv2/image"
	"github.com/rancher/rancher/pkg/provisioningv2/kubeconfig"
	"github.com/rancher/rancher/pkg/provisioningv2/prebootstrap"
	"github.com/rancher/rancher/pkg/provisioningv2/systeminfo"
	"github.com/rancher/rancher/pkg/settings"
	"github.com/rancher/rancher/pkg/wrangler"
)

func EarlyRegister(ctx context.Context, clients *wrangler.Context) error {
	if features.MCM.Enabled() {
		if err := dynamicschema.Register(ctx, clients); err != nil {
			return err
		}
	}
	return nil
}

func Register(ctx context.Context, clients *wrangler.CAPIContext, kubeconfigManager *kubeconfig.Manager) error {
	rkePlanner := planner.New(ctx, clients, planner.InfoFunctions{
		ImageResolver:           image.ResolveWithControlPlane,
		ReleaseData:             capr.GetKDMReleaseData,
		SystemAgentImage:        settings.SystemAgentInstallerImage.Get,
		SystemPodLabelSelectors: systeminfo.NewRetriever(clients).GetSystemPodLabelSelectors,
		GetBootstrapManifests:   prebootstrap.NewRetriever(clients).GeneratePreBootstrapClusterAgentManifest,
	})
	if features.MCM.Enabled() {
		rkeinfracontrollers.Register(ctx, clients, kubeconfigManager)
		autoscaler.Register(ctx, clients)
	}
	machinenodelookup.Register(ctx, clients, kubeconfigManager)
	plansecret.Register(ctx, clients)
	unmanaged.Register(ctx, clients, kubeconfigManager)
	managesystemagent.Register(ctx, clients)
	machinedrain.Register(ctx, clients)

	rkebootstrapcontrollers.Register(ctx, clients)
	rkecontrolplanecontroller.Register(ctx, clients, rkePlanner)
	rkeinfracontrollers.RegisterCluster(ctx, clients)

	return nil
}
