// Package plan is the umbrella registration entry-point for the new
// ClusterPlan-based day-2 ops framework. It wires every controller in
// pkg/controllers/plan/<sub>/ to its wrangler-backed dependencies and
// to the cluster-type adapter Registry.
//
// Register is invoked exactly once at Rancher startup, gated by
// features.ImportedDay2Ops.Enabled() — see
// pkg/controllers/management/controller.go.
//
// The wiring deliberately uses the small dependency interfaces each
// controller declares rather than reaching directly for wrangler
// types: the wrangler-generated typed clients satisfy them
// structurally, and the unit tests use the same interfaces with
// in-memory fakes.
package plan

import (
	"context"
	"errors"

	"github.com/rancher/rancher/pkg/clustermanager"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/adapter/capr"
	"github.com/rancher/rancher/pkg/clusterplan/adapter/caprke2"
	"github.com/rancher/rancher/pkg/clusterplan/adapter/imported"
	"github.com/rancher/rancher/pkg/clusterplan/render"
	planbeacon "github.com/rancher/rancher/pkg/controllers/plan/beacon"
	planbuiltin "github.com/rancher/rancher/pkg/controllers/plan/builtin"
	planelection "github.com/rancher/rancher/pkg/controllers/plan/election"
	planetcdsnap "github.com/rancher/rancher/pkg/controllers/plan/etcdsnapshot"
	plannodeplan "github.com/rancher/rancher/pkg/controllers/plan/nodeplan"
	planrenderer "github.com/rancher/rancher/pkg/controllers/plan/renderer"
	planstage "github.com/rancher/rancher/pkg/controllers/plan/stage"
	plantrigger "github.com/rancher/rancher/pkg/controllers/plan/trigger"
	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/wrangler/v3/pkg/apply"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// Register wires every controller in the framework into the supplied
// wrangler context. It is safe to call only once per process; the
// caller in pkg/controllers/management/controller.go gates the call on
// features.ImportedDay2Ops.Enabled().
//
// The CAPI-shaped caches (CAPI Machines, RKEBootstrap) live behind the
// deferred CAPI registration in wrangler.Context. We register the
// CAPR adapter inside that deferred callback so we don't fail
// startup on clusters that don't yet have CAPI installed.
func Register(ctx context.Context, w *wrangler.Context, manager *clustermanager.Manager) error {
	registry := adapter.NewRegistry()

	// Imported adapter is wired immediately — it only needs management
	// caches that are always available.
	registry.Register(imported.New(imported.Deps{
		Clusters:   w.Mgmt.Cluster().Cache(),
		Nodes:      w.Mgmt.Node().Cache(),
		NodeClient: w.Mgmt.Node(),
		Downstream: newClusterManagerDownstream(manager),
	}))

	// CAPRKE2 ships as a scaffold; safe to register either way.
	registry.Register(caprke2.New())

	// Render engine. The sandboxed RenderClient is intentionally
	// no-op for PR1 — the etcd-snapshot-create templates we ship
	// only call cluster-type-agnostic funcs (runtimeCommand,
	// kubeVersion, semverGTE, hasRole) that don't need k8s reads. A
	// production-quality client backed by typed informers + a GVK
	// allow-list lands as a follow-up alongside richer operations.
	engine := render.New(noopRenderClient{}, nil)

	// Resolve Rancher's externally-reachable URL for the registration
	// endpoint. Empty here → renderer leaves
	// Beacon.Status.RegistrationEndpoint blank, which is benign for
	// PR1 (the agent transport remains the legacy machine-plan secret
	// until PR6 swaps it for kubeconfigs minted by the registration
	// endpoint).
	registrationURL := ""

	// builtin controller — apply embedded ClusterPlanTemplates at
	// startup. Uses wrangler.Apply.
	bh := planbuiltin.New(applyShim{apply: w.Apply})
	if err := bh.Register(ctx); err != nil {
		return err
	}

	// etcdsnapshot reconciler — turns ETCDSnapshotCreate CRs into
	// ClusterPlans.
	esh := planetcdsnap.New(planetcdsnap.Deps{
		Templates:    w.Plan.ClusterPlanTemplate().Cache(),
		ClusterPlans: w.Plan.ClusterPlan(),
		Requests:     w.Plan.ETCDSnapshotCreate(),
	})
	w.Plan.ETCDSnapshotCreate().OnChange(ctx, "plan-etcdsnapshot", esh.OnChange)

	// beacon controller — owns the per-cluster lock state machine.
	beaconHandler := planbeacon.New(planbeacon.Deps{
		Beacons:      w.Plan.Beacon(),
		ClusterPlans: w.Plan.ClusterPlan().Cache(),
		PlansClient:  w.Plan.ClusterPlan(),
	})
	w.Plan.Beacon().OnChange(ctx, "plan-beacon", beaconHandler.OnChange)
	// ClusterPlan terminal-phase transitions are eventually picked up
	// by the beacon controller via the wrangler default resync — for
	// PR1 we accept that latency rather than wire a cross-resource
	// enqueue. PR2 (etcd-restore) introduces real cancellation flows
	// that benefit from prompt enqueues; that's where we'll add it.

	// renderer controller — render-on-acquire.
	rh := planrenderer.New(planrenderer.Deps{
		Templates:       w.Plan.ClusterPlanTemplate().Cache(),
		ClusterPlans:    w.Plan.ClusterPlan(),
		Beacons:         w.Plan.Beacon().Cache(),
		BeaconsClient:   w.Plan.Beacon(),
		Adapters:        registry,
		Engine:          engine,
		RegistrationURL: registrationURL,
	})
	w.Plan.ClusterPlan().OnChange(ctx, "plan-renderer", rh.OnChange)

	// stage controller — drives pool execution.
	sh := planstage.New(planstage.Deps{
		ClusterPlans:    w.Plan.ClusterPlan(),
		NodePlans:       w.Plan.NodePlan().Cache(),
		NodePlansClient: w.Plan.NodePlan(),
		Adapters:        registry,
	})
	w.Plan.ClusterPlan().OnChange(ctx, "plan-stage", sh.OnChange)

	// nodeplan controller — deliver and read status per NodePlan.
	nph := plannodeplan.New(plannodeplan.Deps{
		NodePlans:    w.Plan.NodePlan(),
		ClusterPlans: w.Plan.ClusterPlan().Cache(),
		Adapters:     registry,
		Engine:       engine,
	})
	w.Plan.NodePlan().OnChange(ctx, "plan-nodeplan", nph.OnChange)

	// election controller — picks the elected node per pool.
	eh := planelection.New(planelection.Deps{
		Adapters: registry,
		Engine:   engine,
	})
	w.Plan.ClusterPlan().OnChange(ctx, "plan-election", eh.OnChange)

	// CAPR adapter + its trigger needs CAPI Machine caches. Wire
	// inside the deferred CAPI initializer so we don't crash on
	// non-CAPI startups.
	w.DeferredCAPIRegistration.DeferRegistration(func(ctx context.Context, capiCtx *wrangler.CAPIContext) error {
		registry.Register(capr.New(capr.Deps{
			Machines:             capiCtx.CAPI.Machine().Cache(),
			MachinesClient:       capiCtx.CAPI.Machine(),
			Secrets:              w.Core.Secret(),
			RKEControlPlanes:     w.RKE.RKEControlPlane().Cache(),
			RKEBootstraps:        w.RKE.RKEBootstrap().Cache(),
			ProvisioningClusters: w.Provisioning.Cluster().Cache(),
		}))
		// trigger controller — RKEControlPlane gen-bumps for opted-in
		// clusters become ETCDSnapshotCreate CRs.
		th := plantrigger.NewRKEControlPlane(plantrigger.Deps{
			ProvisioningClusters: w.Provisioning.Cluster().Cache(),
			Requests:             w.Plan.ETCDSnapshotCreate(),
		})
		w.RKE.RKEControlPlane().OnChange(ctx, "plan-trigger-rkecp", th.OnChange)
		return nil
	})

	// importlabels controller intentionally not registered in PR1 —
	// the cross-cluster downstream-Node watch needed to mirror
	// node-role labels onto v3.Node lives behind the same
	// clustermanager surface as the imported adapter's downstream
	// secret factory; wiring it requires either per-cluster watch
	// goroutines or a periodic resync. Tracked as a follow-up.
	return nil
}

// --- shims between wrangler types and the controllers' interfaces ---------

// applyShim adapts wrangler/v3/pkg/apply.Apply to the
// planbuiltin.Applier signature, which takes setID as a function arg
// rather than via the WithSetID builder.
type applyShim struct {
	apply apply.Apply
}

func (a applyShim) ApplyObjects(setID string, objects ...runtime.Object) error {
	return a.apply.WithSetID(setID).WithDynamicLookup().ApplyObjects(objects...)
}

// --- noop render client ----------------------------------------------------

// noopRenderClient returns adapter.ErrNotAllowed for every read. The
// shipped etcd-snapshot-create templates don't exercise the secret /
// owner / get / list functions, so this is sufficient for PR1.
// PR2-PR5 operations that do call them must replace this with a real
// sandboxed client backed by typed informers.
type noopRenderClient struct{}

func (noopRenderClient) GetSecret(_, _ string) (*corev1.Secret, error) {
	return nil, render.ErrNotAllowed
}
func (noopRenderClient) Get(_, _, _, _ string) (runtime.Object, error) {
	return nil, render.ErrNotAllowed
}
func (noopRenderClient) List(_, _, _ string, _ labels.Selector) ([]runtime.Object, error) {
	return nil, render.ErrNotAllowed
}

// --- clustermanager-backed downstream client -------------------------------

// newClusterManagerDownstream wraps a clustermanager.Manager into the
// imported.DownstreamClient interface. For PR1 the downstream secret
// surface is a thin shim around UserContextNoControllers — production
// installations of the imported-day-2-ops feature already exercise
// this same path.
func newClusterManagerDownstream(manager *clustermanager.Manager) imported.DownstreamClient {
	return &clustermanagerDownstream{manager: manager}
}

type clustermanagerDownstream struct {
	manager *clustermanager.Manager
}

func (c *clustermanagerDownstream) Secrets(clusterName string) (imported.DownstreamSecrets, error) {
	if c.manager == nil {
		return nil, errClusterManagerUnavailable
	}
	uc, err := c.manager.UserContextNoControllers(clusterName)
	if err != nil {
		return nil, err
	}
	if uc == nil {
		return nil, errClusterManagerUnavailable
	}
	return &downstreamSecrets{
		client: uc.K8sClient.CoreV1().Secrets(downstreamSecretNamespace),
	}, nil
}

const downstreamSecretNamespace = "cattle-system"

var errClusterManagerUnavailable = errors.New("plan: clustermanager unavailable")

// secretInterface is the subset of corev1.SecretInterface this package
// touches; it's defined to keep the imports tidy without dragging in
// the full client-go typed API surface here.
type secretInterface interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error)
	Create(ctx context.Context, secret *corev1.Secret, opts metav1.CreateOptions) (*corev1.Secret, error)
	Update(ctx context.Context, secret *corev1.Secret, opts metav1.UpdateOptions) (*corev1.Secret, error)
}

type downstreamSecrets struct {
	client secretInterface
}

func (d *downstreamSecrets) Get(name string) (*corev1.Secret, error) {
	return d.client.Get(context.Background(), name, metav1.GetOptions{})
}
func (d *downstreamSecrets) Create(s *corev1.Secret) (*corev1.Secret, error) {
	return d.client.Create(context.Background(), s, metav1.CreateOptions{})
}
func (d *downstreamSecrets) Update(s *corev1.Secret) (*corev1.Secret, error) {
	return d.client.Update(context.Background(), s, metav1.UpdateOptions{})
}
