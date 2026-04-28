// Package etcdsnapshot reconciles plan.cattle.io/v1alpha1.ETCDSnapshotCreate
// objects: it picks the correct per-cluster-type ClusterPlanTemplate and
// materialises a ClusterPlan that the rest of the framework drives to
// completion.
//
// Cluster-type knowledge lives entirely in the per-type
// ClusterPlanTemplate selection (see pkg/clusterplan/builtin) — this
// reconciler is itself cluster-type-agnostic. It only knows:
//
//   1. How to read req.Spec.ClusterRef and turn it into a cluster type
//      via DetermineClusterType (a GVK lookup).
//   2. How to compute the namespace where the ClusterPlan should live
//      (req.Namespace by default; for cluster-scoped imported clusters
//      the request is created directly in the management namespace, so
//      this works uniformly).
//   3. How to compose the ClusterPlan from the template + the request's
//      inputs.
package etcdsnapshot

import (
	"fmt"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/clusterplan/adapter"
	"github.com/rancher/rancher/pkg/clusterplan/builtin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OperationName is the canonical operation discriminator. The
// reconciler computes the per-cluster-type ClusterPlanTemplate name as
// `<OperationName>-<clusterType>`.
const OperationName = "etcd-snapshot-create"

// ClusterPlanClient is the namespaced create/get surface this
// reconciler needs. Production code passes the wrangler-generated
// ClusterPlanClient; tests pass a fake.
type ClusterPlanClient interface {
	Get(namespace, name string, opts metav1.GetOptions) (*v1alpha1.ClusterPlan, error)
	Create(*v1alpha1.ClusterPlan) (*v1alpha1.ClusterPlan, error)
}

// ETCDSnapshotCreateClient is the status-update surface.
type ETCDSnapshotCreateClient interface {
	UpdateStatus(*v1alpha1.ETCDSnapshotCreate) (*v1alpha1.ETCDSnapshotCreate, error)
}

// TemplateCache is the read API the reconciler needs over
// ClusterPlanTemplate. Production code passes the wrangler-generated
// ClusterPlanTemplateCache; tests pass a fake (or
// builtin.ClusterPlanTemplateByName-backed shim for round-trip tests).
type TemplateCache interface {
	Get(name string) (*v1alpha1.ClusterPlanTemplate, error)
}

// Deps bundles the dependencies the reconciler operates on.
type Deps struct {
	Templates           TemplateCache
	ClusterPlans        ClusterPlanClient
	Requests            ETCDSnapshotCreateClient

	// Now overrides time.Now for deterministic test output.
	Now func() time.Time
}

// New returns an initialised Handler.
func New(deps Deps) *Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Handler{deps: deps}
}

// Handler is the reconciler. Its OnChange method matches the wrangler
// controller signature.
type Handler struct {
	deps Deps
}

// OnChange materialises a ClusterPlan for the supplied request when one
// does not yet exist, and reflects the materialisation in
// req.Status.ClusterPlanRef. Idempotent: re-invocations after the
// ClusterPlan exists are no-ops.
func (h *Handler) OnChange(_ string, req *v1alpha1.ETCDSnapshotCreate) (*v1alpha1.ETCDSnapshotCreate, error) {
	if req == nil || req.DeletionTimestamp != nil {
		return req, nil
	}
	// Already materialised? Nothing to do.
	if req.Status.ClusterPlanRef != nil {
		return req, nil
	}

	clusterType := adapter.DetermineClusterType(req.Spec.ClusterRef)
	if clusterType == "" {
		return h.fail(req, "UnsupportedClusterType",
			fmt.Sprintf("clusterRef %s/%s is not a supported cluster surface (got apiVersion=%q kind=%q)",
				req.Spec.ClusterRef.Namespace, req.Spec.ClusterRef.Name,
				req.Spec.ClusterRef.APIVersion, req.Spec.ClusterRef.Kind))
	}

	templateName := builtin.TemplateNameFor(OperationName, clusterType)
	tmpl, err := h.deps.Templates.Get(templateName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return h.fail(req, "TemplateMissing",
				fmt.Sprintf("ClusterPlanTemplate %q for cluster type %q is not installed", templateName, clusterType))
		}
		return req, fmt.Errorf("etcdsnapshot: get template %q: %w", templateName, err)
	}

	plan := composeClusterPlan(req, tmpl)

	// Idempotency: re-create the same plan name is a no-op.
	created, err := h.deps.ClusterPlans.Create(plan)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return req, fmt.Errorf("etcdsnapshot: create ClusterPlan: %w", err)
	}
	if apierrors.IsAlreadyExists(err) {
		// Fetch the existing one to record its UID in our status.
		created, err = h.deps.ClusterPlans.Get(plan.Namespace, plan.Name, metav1.GetOptions{})
		if err != nil {
			return req, fmt.Errorf("etcdsnapshot: get existing ClusterPlan: %w", err)
		}
	}

	out := req.DeepCopy()
	out.Status.ObservedGeneration = req.Generation
	out.Status.ClusterPlanRef = &v1alpha1.LocalObjectReference{Name: created.Name}
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "PlanCreated",
		Message:            fmt.Sprintf("ClusterPlan %q created from template %q", created.Name, tmpl.Name),
		LastTransitionTime: metav1.NewTime(h.deps.Now()),
	})
	return h.deps.Requests.UpdateStatus(out)
}

// composeClusterPlan builds the ClusterPlan that will get created from
// req + the resolved template. Critically, the structural metadata
// (Operation, ClusterRef, EntrypointRef, Lifecycle, TemplateRef,
// Inputs) is derived from req+template here — the template body
// rendered by the renderer fills in only Plan / Files / Probes /
// Instructions on top of this.
func composeClusterPlan(req *v1alpha1.ETCDSnapshotCreate, tmpl *v1alpha1.ClusterPlanTemplate) *v1alpha1.ClusterPlan {
	planName := req.Name + "-plan"
	inputs := map[string]string{}
	if req.Spec.Name != "" {
		inputs["snapshotName"] = req.Spec.Name
	}
	var entrypoint *v1alpha1.ObjectReference
	if len(tmpl.Spec.Entrypoints) > 0 {
		// First entrypoint wins — the templates we ship list exactly
		// one entry, so this is unambiguous in practice.
		ep := tmpl.Spec.Entrypoints[0]
		entrypoint = &v1alpha1.ObjectReference{
			APIVersion: ep.APIVersion,
			Kind:       ep.Kind,
			Namespace:  req.Spec.ClusterRef.Namespace,
			Name:       req.Spec.ClusterRef.Name,
		}
	}
	return &v1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: req.Namespace,
			Labels: map[string]string{
				v1alpha1.ClusterPlanNameLabel: planName,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.SchemeGroupVersion.String(),
				Kind:       "ETCDSnapshotCreate",
				Name:       req.Name,
				UID:        req.UID,
				Controller: ptrBool(true),
			}},
		},
		Spec: v1alpha1.ClusterPlanSpec{
			TemplateRef:   &v1alpha1.LocalClusterScopedRef{Name: tmpl.Name},
			Operation:     tmpl.Spec.Operation,
			ClusterRef:    req.Spec.ClusterRef,
			EntrypointRef: entrypoint,
			Inputs:        inputs,
			Lifecycle:     tmpl.Spec.Lifecycle,
			// Plan / Files / Probes / Instructions are filled in by
			// the renderer controller once the Beacon has been
			// acquired (see pkg/controllers/plan/renderer/, future
			// task).
		},
	}
}

// fail records a terminal failure on the request and returns it for
// the framework to persist.
func (h *Handler) fail(req *v1alpha1.ETCDSnapshotCreate, reason, message string) (*v1alpha1.ETCDSnapshotCreate, error) {
	out := req.DeepCopy()
	out.Status.ObservedGeneration = req.Generation
	setCondition(&out.Status.Conditions, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(h.deps.Now()),
	})
	return h.deps.Requests.UpdateStatus(out)
}

// DetermineClusterType is re-exported from pkg/clusterplan/adapter for
// callers that already depend on this package. New callers should use
// adapter.DetermineClusterType directly.
func DetermineClusterType(ref v1alpha1.ClusterReference) string {
	return adapter.DetermineClusterType(ref)
}

// --- helpers ----------------------------------------------------------------

func setCondition(conds *[]metav1.Condition, c metav1.Condition) {
	for i := range *conds {
		if (*conds)[i].Type == c.Type {
			(*conds)[i] = c
			return
		}
	}
	*conds = append(*conds, c)
}

func ptrBool(b bool) *bool { return &b }
