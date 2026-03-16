package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/wrangler/v3/pkg/name"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func (h *handler) OnNodePlanChange(_ string, plan *planv1alpha1.NodePlan) (*planv1alpha1.NodePlan, error) {
	if plan == nil || plan.DeletionTimestamp != nil {
		return nil, nil
	}

	// 1. Get the Secret associated with this NodePlan
	secretName := name.SafeConcatName(plan.Name, "machine", "plan")
	mps, err := h.secrets.Get(plan.Namespace, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return plan, nil // Wait for secret to be created
		}
		return nil, err
	}

	// 2. Calculate the checksum of the current Spec
	planData, err := json.Marshal(plan.Spec)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(planData)
	planChecksum := hex.EncodeToString(sum[:])

	// 3. Check if the node agent has reported success via appliedChecksum
	appliedChecksum := string(mps.Data["applied-checksum"])

	if appliedChecksum == planChecksum {
		if plan.Status.Phase != planv1alpha1.NodePlanPhaseSucceeded {
			content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(plan)
			obj := &unstructured.Unstructured{Object: content}

			funcs := h.getGenericFuncs(obj)

			updatedOutputs := map[string]planv1alpha1.OutputStatus{}

			// verify expect & create outputs
			for _, o := range plan.Spec.Outputs {
				var buf string
				val, err := h.executeTemplate(o.Source, &buf, funcs)
				if err != nil {
					logrus.Errorf("failed to render output %s: %v", o.Name, err)
					continue
				}

				updatedOutputs[o.Name] = planv1alpha1.OutputStatus{
					Value:      val,
					Persistent: o.Persistent,
				}
			}
			logrus.Infof("[nodeplan] plan %s/%s applied successfully (checksum matches)", plan.Namespace, plan.Name)
			plan = plan.DeepCopy()
			plan.Status.Phase = planv1alpha1.NodePlanPhaseSucceeded
			plan.Status.Outputs = updatedOutputs
			return h.nodePlan.UpdateStatus(plan)
		}
		return plan, nil
	}

	// 4. If not applied yet, ensure the Secret has the latest plan and the target checksum
	if string(mps.Data["plan"]) != string(planData) {
		mps = mps.DeepCopy()
		if mps.Data == nil {
			mps.Data = map[string][]byte{} // Ensure map is initialized
		}
		mps.Data["plan"] = planData

		logrus.Debugf("[nodeplan] updating secret %s/%s with new plan hash %s", plan.Namespace, secretName, planChecksum)
		_, err = h.secrets.Update(mps)
		if err != nil {
			return nil, err
		}

		// Reset phase to running/processing if the plan changed
		if plan.Status.Phase != planv1alpha1.NodePlanPhaseRunning {
			plan = plan.DeepCopy()
			plan.Status.Phase = planv1alpha1.NodePlanPhaseRunning
			return h.nodePlan.UpdateStatus(plan)
		}
	}

	return plan, nil
}
