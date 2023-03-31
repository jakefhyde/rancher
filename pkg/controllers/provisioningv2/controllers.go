package provisioningv2

import (
	"context"

	"github.com/rancher/rancher/pkg/controllers/provisioningv2/cluster"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/fleetcluster"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/fleetworkspace"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/managedchart"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/bootstrap"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/dynamicschema"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/machinedrain"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/machinenodelookup"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/machineprovision"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/managesystemagent"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/planner"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/plansecret"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/provisioningcluster"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/provisioninglog"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/rkecluster"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/rkecontrolplane"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/secret"
	"github.com/rancher/rancher/pkg/controllers/provisioningv2/rke2/unmanaged"
	"github.com/rancher/rancher/pkg/features"
	"github.com/rancher/rancher/pkg/provisioningv2/capi"
	"github.com/rancher/rancher/pkg/provisioningv2/capi/do"
	"github.com/rancher/rancher/pkg/provisioningv2/kubeconfig"
	planner2 "github.com/rancher/rancher/pkg/provisioningv2/rke2/planner"
	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/sirupsen/logrus"
)

func Register(ctx context.Context, clients *wrangler.Context) error {
	kubeconfigManager := kubeconfig.New(clients)
	cluster.Register(ctx, clients, kubeconfigManager)

	if features.Fleet.Enabled() {
		managedchart.Register(ctx, clients)
		fleetcluster.Register(ctx, clients)
		fleetworkspace.Register(ctx, clients)
	}

	if features.RKE2.Enabled() {
		rkePlanner := planner2.New(ctx, clients)
		if features.MCM.Enabled() {
			dynamicschema.Register(ctx, clients)
			machineprovision.Register(ctx, clients, kubeconfigManager)
		}
		rkecluster.Register(ctx, clients)
		provisioningcluster.Register(ctx, clients)
		provisioninglog.Register(ctx, clients)
		secret.Register(ctx, clients)
		bootstrap.Register(ctx, clients)
		machinenodelookup.Register(ctx, clients, kubeconfigManager)
		planner.Register(ctx, clients, rkePlanner)
		plansecret.Register(ctx, clients)
		unmanaged.Register(ctx, clients, kubeconfigManager)
		rkecontrolplane.Register(ctx, clients)
		managesystemagent.Register(ctx, clients)
		machinedrain.Register(ctx, clients)
	}

	if features.EmbeddedClusterAPIProviders.Enabled() {
		doStart, err := do.Register(ctx, clients)
		if err != nil {
			return err
		}
		clients.OnLeader(func(ctx context.Context) error {
			if err := doStart(ctx); err != nil {
				logrus.Fatal(err)
			}
			logrus.Info("Cluster API DO provider is started")
			return nil
		})
	}

	if features.EmbeddedClusterAPI.Enabled() {
		capiStart, err := capi.Register(ctx, clients)
		if err != nil {
			return err
		}
		clients.OnLeader(func(ctx context.Context) error {
			if err := capiStart(ctx); err != nil {
				logrus.Fatal(err)
			}
			logrus.Info("Cluster API is started")
			return nil
		})
	}

	return nil
}
