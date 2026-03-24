package rkecontrolplane

import (
	"context"

	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	plancontrollers "github.com/rancher/rancher/pkg/generated/controllers/plan.cattle.io/v1alpha1"
	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/wrangler/v3/pkg/name"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type handler2 struct {
	clusterPlanCache plancontrollers.ClusterPlanCache
}

func Register2(ctx context.Context, clients *wrangler.CAPIContext) {

}

func (h *handler2) OnChange(obj *rkev1.RKEControlPlane) error {
	if obj == nil {
		return nil
	}

	//if obj.DeletionTimestamp != nil {
	//	return nil
	//}
	//
	//// get existing cluster plan
	//plan, err := h.clusterPlanCache.Get(obj.Namespace, name.SafeConcatName(obj.Name, "cluster", "plan"))
	//if !apierrors.IsNotFound(err) {
	//	// get desired plan
	//
	//	// create
	//} else if err != nil {
	//	return err
	//}
	//
	//// get desired cluster plan
	//// if existing == desired, update status and exit
	//
	return nil
}

func ensureTokenSecret() {

}

func (h *handler2) desiredPlan(obj *rkev1.RKEControlPlane) (*planv1alpha1.ClusterPlan, error) {
	if obj == nil {
		return nil, nil
	}

	plan := &planv1alpha1.ClusterPlan{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: obj.Namespace,
			Name:      name.SafeConcatName(obj.Name, "cluster", "plan"),
		},
		Spec: planv1alpha1.ClusterPlanSpec{
			Plan: []planv1alpha1.NodePoolPlan{},
		},
	}

	return plan, nil
}
