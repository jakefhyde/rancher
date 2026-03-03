package plan

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"

	"github.com/rancher/lasso/pkg/dynamic"
	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	plancontrollers "github.com/rancher/rancher/pkg/generated/controllers/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/wrangler"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/v3/pkg/name"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type handler struct {
	dynamic *dynamic.Controller

	clusterPlan      plancontrollers.ClusterPlanController
	clusterPlanCache plancontrollers.ClusterPlanCache
	nodePlan         plancontrollers.NodePlanController
	nodePlanCache    plancontrollers.NodePlanCache

	secrets corecontrollers.SecretController
}

func Register(ctx context.Context, clients *wrangler.CAPIContext) {
	h := &handler{
		dynamic:          clients.Dynamic,
		clusterPlan:      clients.Plan.ClusterPlan(),
		clusterPlanCache: clients.Plan.ClusterPlan().Cache(),
		nodePlan:         clients.Plan.NodePlan(),
		nodePlanCache:    clients.Plan.NodePlan().Cache(),
		secrets:          clients.Core.Secret(),
	}

	clients.Plan.ClusterPlan().OnChange(ctx, "cluster-plan", h.OnClusterPlanChange)
	clients.Plan.NodePlan().OnChange(ctx, "node-plan", h.OnNodePlanChange)
}

func (h *handler) OnClusterPlanChange(_ string, plan *planv1alpha1.ClusterPlan) (*planv1alpha1.ClusterPlan, error) {
	if plan == nil {
		return nil, nil
	}

	b, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(b)

	// get the current step
	currentStep := plan.Status.CurrentStep
	if len(plan.Spec.Plan) <= currentStep {
		// error
		return nil, errors.New("invalid current step")
	}

	npp := plan.Spec.Plan[currentStep]

	sel, err := metav1.LabelSelectorAsSelector(&npp.Selector)
	if err != nil {
		return nil, err
	}

	// todo(jhyde): extract GVK from object
	objs, err := h.dynamic.List(schema.FromAPIVersionAndKind("cluster.x-k8s.io/v1beta2", "Machine"), plan.Namespace, sel)
	if err != nil {
		return nil, err
	}

	// sort machines by name
	slices.SortFunc(objs, func(i, j runtime.Object) int {
		i1, err := meta.Accessor(i)
		if err != nil {
			return 0
		}
		j1, err := meta.Accessor(j)
		if err != nil {
			return 0
		}
		if i1.GetName() < j1.GetName() {
			return -1
		} else if i1.GetName() > j1.GetName() {
			return 1
		}
		return 0
	})

	concurrency := npp.Concurrency
	if concurrency <= 0 || concurrency > len(objs) {
		concurrency = len(objs)
	}

	// render plan for machines according to the cluster plan
	for _, o := range objs {
		if concurrency <= 0 {
			// hit concurrency limit
			break
		}
		// todo(jhyde): go template node spec

		m, err := NewMachineInfo(o)
		if err != nil {
			return nil, err
		}

		np, err := h.nodePlanCache.Get(m.Namespace(), m.PlanName())
		if apierrors.IsNotFound(err) {
			// create
			_ = planv1alpha1.NodePlan{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   m.Namespace(),
					Name:        m.PlanName(),
					Annotations: map[string]string{},
					Labels: map[string]string{
						planv1alpha1.ClusterPlanHashLabel: string(hash[:]),
					},
					OwnerReferences: []metav1.OwnerReference{},
				},
				Spec: npp.NodePlanSpec,
			}
			concurrency--
			continue
		} else if err != nil {
			return nil, err
		}
		if np.Labels != nil && np.Labels[planv1alpha1.ClusterPlanHashLabel] == string(hash[:]) {
			// check if plan in sync
			if np.Status.Phase == planv1alpha1.NodePlanPhaseFailed {
				// update clusterplan
				plan = plan.DeepCopy()
				plan.Status.Phase = planv1alpha1.ClusterPlanPhaseFailed
				plan, err = h.clusterPlan.UpdateStatus(plan)
				if err != nil {
					return nil, err
				}
			} else if np.Status.Phase == planv1alpha1.NodePlanPhaseSucceeded {
				continue
			}
			concurrency--
			continue
		}

		// either delete or update

		// assign plan, check concurrency
		err = h.clusterPlan.Delete(m.Namespace(), m.PlanName(), &metav1.DeleteOptions{})
		if err != nil {
			return nil, err
		}
		concurrency--
	}

	if concurrency != len(objs) {
		plan = plan.DeepCopy()
		// update conditions
		plan.Status.Phase = planv1alpha1.ClusterPlanPhaseRunning
		return h.clusterPlan.UpdateStatus(plan)
	}

	// all plans in sync
	plan = plan.DeepCopy()
	plan.Status.CurrentStep++
	if len(plan.Spec.Plan) <= plan.Status.CurrentStep {
		plan.Status.Phase = planv1alpha1.ClusterPlanPhaseSucceeded
		return h.clusterPlan.UpdateStatus(plan)
	}

	return plan, nil
}

func (h *handler) OnNodePlanChange(_ string, plan *planv1alpha1.NodePlan) (*planv1alpha1.NodePlan, error) {
	if plan == nil {
		return nil, nil
	}

	// todo(jhyde): remove all this
	mps, err := h.secrets.Get(plan.Namespace, name.SafeConcatName(plan.Name, "machine", "plan"), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	b, err := json.Marshal(plan.Spec)
	if err != nil {
		return nil, err
	}
	mps = mps.DeepCopy()
	mps.Data["plan"] = b

	_, err = h.secrets.Update(mps)
	if err != nil {
		return nil, err
	}

	return plan, nil
}
