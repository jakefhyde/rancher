package plan

import (
	"context"

	"github.com/rancher/lasso/pkg/dynamic"
	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	plancontrollers "github.com/rancher/rancher/pkg/generated/controllers/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/wrangler"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/v3/pkg/name"
	"github.com/rancher/wrangler/v3/pkg/relatedresource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type handler struct {
	dynamic *dynamic.Controller

	clusterPlan      plancontrollers.ClusterPlanController
	clusterPlanCache plancontrollers.ClusterPlanCache
	nodePlan         plancontrollers.NodePlanController
	nodePlanCache    plancontrollers.NodePlanCache

	secrets     corecontrollers.SecretController
	secretCache corecontrollers.SecretCache
}

func Register(ctx context.Context, clients *wrangler.CAPIContext) {
	h := &handler{
		dynamic:          clients.Dynamic,
		clusterPlan:      clients.Plan.ClusterPlan(),
		clusterPlanCache: clients.Plan.ClusterPlan().Cache(),
		nodePlan:         clients.Plan.NodePlan(),
		nodePlanCache:    clients.Plan.NodePlan().Cache(),
		secrets:          clients.Core.Secret(),
		secretCache:      clients.Core.Secret().Cache(),
	}

	clients.Plan.ClusterPlan().OnChange(ctx, "cluster-plan", h.reconcileClusterPlan)
	clients.Plan.NodePlan().OnChange(ctx, "node-plan", h.OnNodePlanChange)

	relatedresource.Watch(ctx, "machine-plan-node-plan", func(_, _ string, obj runtime.Object) ([]relatedresource.Key, error) {
		if secret, ok := obj.(*corev1.Secret); ok {
			return []relatedresource.Key{
				{
					Namespace: secret.Namespace,
					Name:      name.SafeConcatName(secret.Name, "machine", "plan"),
				},
			}, nil
		}
		return nil, nil
	}, clients.Plan.NodePlan(), clients.Core.Secret())
}

//func (h *handler) OnClusterPlanChange(_ string, plan *planv1alpha1.ClusterPlan) (*planv1alpha1.ClusterPlan, error) {
	//if plan == nil {
	//	return nil, nil
	//}
	//defer func() {
	//	logrus.Debugf("[clusterplan] requeue cluster plan %s/%s", plan.Namespace, plan.Name)
	//	h.clusterPlan.EnqueueAfter(plan.Namespace, plan.Name, 5*time.Second)
	//}()
	//
	//if plan.Status.Phase == planv1alpha1.ClusterPlanPhaseSucceeded {
	//	logrus.Debugf("[clusterplan] skipping processing for successful clusterplan %s/%s", plan.Namespace, plan.Name)
	//	return plan, nil
	//} else if plan.Status.Phase == planv1alpha1.ClusterPlanPhaseFailed {
	//	logrus.Debugf("[clusterplan] skipping processing for failed clusterplan %s/%s", plan.Namespace, plan.Name)
	//	return plan, nil
	//}
	//
	//logrus.Debugf("[clusterplan] processing clusterplan %s/%s", plan.Namespace, plan.Name)
	//
	//b, err := json.Marshal(plan)
	//if err != nil {
	//	return nil, err
	//}
	//rawHash := sha256.Sum256(b)
	//hash := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawHash[:])
	//
	//logrus.Debugf("[clusterplan] processing clusterplan %s/%s with hash %s", plan.Namespace, plan.Name, hash)
	//
	//// get the current step
	//currentStep := plan.Status.CurrentStep
	//if len(plan.Spec.Plan) <= currentStep {
	//	// error
	//	return nil, errors.New("invalid current step")
	//}
	//
	//npp := plan.Spec.Plan[currentStep]
	//
	//sel, err := metav1.LabelSelectorAsSelector(npp.Selector)
	//if err != nil {
	//	return nil, err
	//}
	//
	//// todo(jhyde): extract GVK from object
	//objs, err := h.dynamic.List(schema.FromAPIVersionAndKind("cluster.x-k8s.io/v1beta2", "Machine"), plan.Namespace, sel)
	//if err != nil {
	//	return nil, err
	//}
	//
	//// sort machines by name
	//slices.SortFunc(objs, func(i, j runtime.Object) int {
	//	i1, err := meta.Accessor(i)
	//	if err != nil {
	//		return 0
	//	}
	//	j1, err := meta.Accessor(j)
	//	if err != nil {
	//		return 0
	//	}
	//	if i1.GetName() < j1.GetName() {
	//		return -1
	//	} else if i1.GetName() > j1.GetName() {
	//		return 1
	//	}
	//	return 0
	//})
	//
	//// todo(jhyde): calculate concurrency
	////concurrency := npp.Concurrency
	////if concurrency <= 0 || concurrency > len(objs) {
	////	concurrency = len(objs)
	////}
	//concurrency := 1
	//
	//logrus.Debugf("[clusterplan] processing %d machines out of %d", concurrency, len(objs))
	//
	//if concurrency == 0 {
	//	logrus.Debugf("[clusterplan] skipping step for clusterplan %s/%s as no machines match selector", plan.Namespace, plan.Name)
	//}
	//
	//// render plan for machines according to the cluster plan
	//for _, o := range objs {
	//	if concurrency <= 0 {
	//		logrus.Debugf("[clusterplan] halting processing for clusterplan %s/%s due to concurrency limit", plan.Namespace, plan.Name)
	//		// hit concurrency limit
	//		break
	//	}
	//
	//	// todo(jhyde): go template node spec
	//
	//	m, err := NewMachineInfo(o)
	//	if err != nil {
	//		return nil, err
	//	}
	//
	//	logrus.Debugf("[clusterplan] rendering nodeplan %s/%s for clusterplan %s/%s", m.Namespace(), m.PlanName(), plan.Namespace, plan.Name)
	//
	//	np, err := h.nodePlanCache.Get(m.Namespace(), m.PlanName())
	//	if apierrors.IsNotFound(err) {
	//		// create
	//		np = &planv1alpha1.NodePlan{
	//			ObjectMeta: metav1.ObjectMeta{
	//				Namespace:   m.Namespace(),
	//				Name:        m.PlanName(),
	//				Annotations: map[string]string{},
	//				Labels: map[string]string{
	//					planv1alpha1.ClusterPlanHashLabel: hash,
	//				},
	//				OwnerReferences: []metav1.OwnerReference{},
	//			},
	//			Spec: npp.NodePlanSpec,
	//		}
	//		logrus.Debugf("[clusterplan] creating nodeplan %s/%s", m.Namespace(), m.PlanName())
	//		_, err = h.nodePlan.Create(np)
	//		if err != nil {
	//			return nil, err
	//		}
	//		concurrency--
	//		continue
	//	} else if err != nil {
	//		return nil, err
	//	}
	//	if np.Labels != nil && np.Labels[planv1alpha1.ClusterPlanHashLabel] == hash {
	//		// check if plan in sync
	//		if np.Status.Phase == planv1alpha1.NodePlanPhaseFailed {
	//			logrus.Debugf("[clusterplan] marking plan %s/%s failed due to failed nodeplan %s/%s", plan.Namespace, plan.Name, m.Namespace(), m.PlanName())
	//			// update clusterplan
	//			plan = plan.DeepCopy()
	//			plan.Status.Phase = planv1alpha1.ClusterPlanPhaseFailed
	//			plan, err = h.clusterPlan.UpdateStatus(plan)
	//			if err != nil {
	//				return nil, err
	//			}
	//		} else if np.Status.Phase == planv1alpha1.NodePlanPhaseSucceeded {
	//			continue
	//		}
	//		concurrency--
	//		continue
	//	}
	//
	//	// either delete or update
	//
	//	logrus.Debugf("[clusterplan] deleting nodeplan %s/%s", m.Namespace(), m.PlanName())
	//	// assign plan, check concurrency
	//	err = h.nodePlan.Delete(m.Namespace(), m.PlanName(), &metav1.DeleteOptions{})
	//	if err != nil {
	//		return nil, err
	//	}
	//	concurrency--
	//}
	//
	//if concurrency != len(objs) {
	//	logrus.Debugf("[clusterplan] marking clusterplan %s/%s as running", plan.Namespace, plan.Name)
	//	plan = plan.DeepCopy()
	//	// update conditions
	//	plan.Status.Phase = planv1alpha1.ClusterPlanPhaseRunning
	//	return h.clusterPlan.UpdateStatus(plan)
	//}
	//
	//logrus.Debugf("[clusterplan] advancing current step for clusterplan %s/%s", plan.Namespace, plan.Name)
	//// all plans in sync
	//plan = plan.DeepCopy()
	//plan.Status.CurrentStep++
	//if len(plan.Spec.Plan) <= plan.Status.CurrentStep {
	//	logrus.Debugf("[clusterplan] marking clusterplan %s/%s as successful", plan.Namespace, plan.Name)
	//	plan.Status.Phase = planv1alpha1.ClusterPlanPhaseSucceeded
	//}
	//return h.clusterPlan.UpdateStatus(plan)
//}

func (h *handler) reconcileClusterPlan(_ string, plan *planv1alpha1.ClusterPlan) (*planv1alpha1.ClusterPlan, error) {
	if plan == nil {
		return nil, nil
	}

	switch plan.Status.Phase {
	case planv1alpha1.ClusterPlanPhasePending:
		plan := plan.DeepCopy()
		plan.Status.Phase = planv1alpha1.ClusterPlanPhaseRunning
		return h.clusterPlan.UpdateStatus(plan)
	case planv1alpha1.ClusterPlanPhaseRunning:
		return h.reconcileRunning(plan)
	case planv1alpha1.ClusterPlanPhaseSucceeded:
		return plan, nil
	case planv1alpha1.ClusterPlanPhaseFailed:
		return plan, nil
	default:
		plan := plan.DeepCopy()
		plan.Status.Phase = planv1alpha1.ClusterPlanPhasePending
		return h.clusterPlan.UpdateStatus(plan)
	}
}
