package do

import (
	"context"

	"github.com/rancher/rancher/pkg/provisioningv2/capi/logger"
	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/wrangler/pkg/schemes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	do "sigs.k8s.io/cluster-api-provider-digitalocean/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-digitalocean/controllers"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

func init() {
	_ = do.AddToScheme(schemes.All)
}

func Register(ctx context.Context, clients *wrangler.Context) (func(ctx context.Context) error, error) {
	mgr, err := ctrl.NewManager(clients.RESTConfig, ctrl.Options{
		MetricsBindAddress: "0",
		//NewCache: controllerruntime.NewNewCacheFunc(clients.SharedControllerFactory.SharedCacheFactory(),
		//	clients.Dynamic),
		Scheme: schemes.All,
		ClientDisableCacheFor: []client.Object{
			&corev1.ConfigMap{},
			&corev1.Secret{},
		},
		Logger: logger.New(2),
		// Work around a panic where the broadcaster is immediately closed
		EventBroadcaster: record.NewBroadcaster(),
	})
	if err != nil {
		return nil, err
	}

	reconcilers, err := reconcilers(mgr, clients)
	if err != nil {
		return nil, err
	}

	for _, reconciler := range reconcilers {
		if err := reconciler.SetupWithManager(ctx, mgr, concurrency(5)); err != nil {
			return nil, err
		}
	}

	return mgr.Start, nil
}

func reconcilers(mgr ctrl.Manager, _ *wrangler.Context) ([]reconciler, error) {
	return []reconciler{
		&controllers.DOClusterReconciler{
			Client:   mgr.GetClient(),
			Recorder: record.NewFakeRecorder(50),
		},
		&controllers.DOMachineReconciler{
			Client:   mgr.GetClient(),
			Recorder: record.NewFakeRecorder(50),
		},
	}, nil
}

func concurrency(c int) controller.Options {
	return controller.Options{MaxConcurrentReconciles: c}
}

type reconciler interface {
	SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error
}
