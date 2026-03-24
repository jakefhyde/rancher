package plan

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"text/template"

	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (h *handler) reconcileRunning(plan *planv1alpha1.ClusterPlan) (*planv1alpha1.ClusterPlan, error) {
	i := plan.Status.CurrentStep
	if i > len(plan.Spec.Plan) || i < 0 {
		return nil, fmt.Errorf("invalid current step %d", i)
	}

	logrus.Debugf("[clusterplan] reconciling clusterplan %s/%s", plan.Namespace, plan.Name)

	step := plan.Spec.Plan[i]

	var err error
	var nodePlans []*planv1alpha1.NodePlan

	if step.Selector != nil {
		nodePlans, err = h.nodePlanCache.List(plan.Namespace, labels.SelectorFromSet(step.Selector.MatchLabels))
		if err != nil {
			return nil, err
		}
	} else if step.Election != nil {
		leader, err := h.reconcileNodeElection(plan, step.Election)
		if err != nil {
			return nil, err
		}
		nodePlans = append(nodePlans, leader)
	}

	// apply before patches

	// todo(jhyde): concurrency
	for _, np := range nodePlans {
		// compute desired plan
		n := step.NodePlanSpec.DeepCopy()
		for _, f := range plan.Spec.Files {
			if matchesSelectors(np, f.Selector) {
				// 3. Render the file (path and content) using the current NodePlan as context
				renderedFile, err := h.renderFile(f.File, np, plan)
				if err != nil {
					logrus.Errorf("[clusterplan] failed to render file %s for %s: %v", f.Path, np.Name, err)
					continue
				}
				n.Files = append(n.Files, *renderedFile)
			}
		}
		// if desired plan is different, update nodeplan
		if !reflect.DeepEqual(np.Spec, *n) {
			np = np.DeepCopy()
			np.Spec = *n
			_, err = h.nodePlan.Update(np)
			if err != nil {
				return nil, err
			}
			logrus.Debugf("[clusterplan] updated nodeplan %s/%s", np.Namespace, np.Name)
			return plan, nil
		}

		// if desired plan is the same, continue
		if np.Status.Phase == planv1alpha1.NodePlanPhaseSucceeded {
			logrus.Debugf("[clusterplan] nodeplan %s/%s is already in desired state", np.Namespace, np.Name)
			continue
		} else if np.Status.Phase == planv1alpha1.NodePlanPhaseFailed {
			logrus.Debugf("[clusterplan] marking plan %s/%s failed due to failed nodeplan %s/%s", plan.Namespace, plan.Name, np.Namespace, np.Name)
			// update clusterplan
			plan = plan.DeepCopy()
			plan.Status.Phase = planv1alpha1.ClusterPlanPhaseFailed
			return h.clusterPlan.UpdateStatus(plan)
		}
	}

	// apply after patches
	for _, ap := range step.Patches.After {
		logrus.Debugf("[clusterplan] applying patch \"%s\" for step %d in clusterplan %s/%s", ap.Name, i, plan.Namespace, plan.Name)
		for _, d := range ap.Definitions {
			gvk := schema.FromAPIVersionAndKind(d.Selector.APIVersion, d.Selector.Kind)
			o, err := h.dynamic.Get(gvk, d.Selector.Namespace, d.Selector.Name)
			if err != nil {
				return nil, err
			}

		}
	}

	logrus.Debugf("[clusterplan] advancing current step for clusterplan %s/%s", plan.Namespace, plan.Name)
	// all plans in sync
	plan = plan.DeepCopy()
	plan.Status.CurrentStep++
	if len(plan.Spec.Plan) <= plan.Status.CurrentStep {
		logrus.Debugf("[clusterplan] marking clusterplan %s/%s as successful", plan.Namespace, plan.Name)
		plan.Status.Phase = planv1alpha1.ClusterPlanPhaseSucceeded
	}
	return h.clusterPlan.UpdateStatus(plan)
}

func matchesSelectors(np *planv1alpha1.NodePlan, selectors []metav1.LabelSelector) bool {
	if len(selectors) == 0 {
		return true
	}

	for _, s := range selectors {
		sel, err := metav1.LabelSelectorAsSelector(&s)
		if err != nil {
			logrus.Errorf("[clusterplan] invalid selector %v: %v", s, err)
			continue
		}

		// Match against the NodePlan's own labels
		if sel.Matches(labels.Set(np.Labels)) {
			return true
		}
	}
	return false
}

func (h *handler) renderFile(spec planv1alpha1.File, np *planv1alpha1.NodePlan, plan *planv1alpha1.ClusterPlan) (*planv1alpha1.File, error) {
	// Convert NodePlan to Unstructured for the generic template helpers
	content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(np)
	obj := &unstructured.Unstructured{Object: content}

	// Use the FuncMap we built (owner, fetch, jsonPath, getCondition, shard, etc.)
	funcs := h.getGenericFuncs(obj)

	// Render Path (in case it's dynamic)
	path, err := h.executeTemplate(spec.Path, obj, funcs)
	if err != nil {
		return nil, err
	}

	// Render Content
	body, err := h.executeTemplate(spec.Content, obj, funcs)
	if err != nil {
		return nil, err
	}

	return &planv1alpha1.File{
		Path:        path,
		Content:     body,
		Permissions: spec.Permissions,
		//UID:         spec.UID,
		//GID:         spec.GID,
	}, nil
}

func (h *handler) executeTemplate(templateStr string, data interface{}, funcs template.FuncMap) (string, error) {
	if templateStr == "" {
		return "", nil
	}

	// 1. Create a new template with a unique name
	// We use a hash or a static name because these are short-lived
	tmpl, err := template.New("plan-template").
		Funcs(funcs). // Inject your generic helpers (shard, fetch, etc.)
		Option("missingkey=error"). // Stop execution if a variable is missing
		Parse(templateStr)

	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// 2. Execute the template against the provided data (e.g., the NodePlan)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

func (h *handler) reconcileNodeElection(plan *planv1alpha1.ClusterPlan, election *planv1alpha1.NodeElection) (*planv1alpha1.NodePlan, error) {
	label := election.TargetLabel

	// 1. Check for existing leader
	plans, err := h.nodePlanCache.List(plan.Namespace, labels.SelectorFromSet(labels.Set{label: "true"}))
	if err != nil {
		return nil, err
	}

	// Found exactly one leader - verify it still meets criteria
	if len(plans) == 1 {
		eligible, err := h.isEligible(plans[0], election)
		if err != nil {
			return nil, err
		}
		if eligible {
			return plans[0], nil
		}
		// If no longer eligible, strip the label and proceed to re-elect
		logrus.Infof("[election] current leader %s is no longer eligible, re-electing", plans[0].Name)
		p := plans[0].DeepCopy()
		delete(p.Labels, label)
		return h.nodePlan.Update(p)
	}

	// Multiple leaders? Clear them to reset state
	if len(plans) > 1 {
		for _, p := range plans {
			p := p.DeepCopy()
			delete(p.Labels, label)
			if _, err := h.nodePlan.Update(p); err != nil {
				return nil, err
			}
		}
		return nil, nil // Requeue to elect a fresh leader
	}

	// 2. Perform Election
	sel, err := metav1.LabelSelectorAsSelector(&election.Selector)
	if err != nil {
		return nil, err
	}

	matching, err := h.nodePlanCache.List(plan.Namespace, sel)
	if err != nil {
		return nil, err
	}

	// Sort for determinism
	slices.SortFunc(matching, func(i, j *planv1alpha1.NodePlan) int {
		return strings.Compare(i.Name, j.Name)
	})

	// 3. Evaluate Criteria for each candidate
	for _, p := range matching {
		eligible, err := h.isEligible(p, election)
		if err != nil {
			logrus.Errorf("[election] error checking eligibility for %s: %v", p.Name, err)
			continue
		}

		if eligible {
			logrus.Infof("[election] node %s elected as %s", p.Name, label)
			p := p.DeepCopy()
			if p.Labels == nil {
				p.Labels = make(map[string]string)
			}
			p.Labels[label] = "true"
			return h.nodePlan.Update(p)
		}
	}

	return nil, errors.New("no candidates found")
}

// isEligible evaluates the boolean template criteria against a specific NodePlan
func (h *handler) isEligible(p *planv1alpha1.NodePlan, election *planv1alpha1.NodeElection) (bool, error) {
	// Build the context for this specific NodePlan
	// We wrap the NodePlan in Unstructured so our generic helpers can read it
	content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(p)
	obj := &unstructured.Unstructured{Object: content}

	funcs := h.getGenericFuncs(obj)

	for _, c := range election.Criteria {
		tmpl, err := template.New("criteria").Funcs(funcs).Parse(c)
		if err != nil {
			return false, fmt.Errorf("failed to parse criteria [%s]: %w", c, err)
		}

		var out bytes.Buffer
		if err := tmpl.Execute(&out, obj); err != nil {
			// A fetch failure might be transient (e.g. cache warm-up)
			return false, nil
		}

		if strings.TrimSpace(out.String()) != "true" {
			return false, nil
		}
	}
	return true, nil
}
