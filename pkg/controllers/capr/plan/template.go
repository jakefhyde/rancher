package plan

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"reflect"
	"slices"
	"strings"
	"text/template"

	planv1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"github.com/rancher/wrangler/v3/pkg/name"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/jsonpath"
)

func (h *handler) getGenericFuncs(source *unstructured.Unstructured) template.FuncMap {
	return template.FuncMap{
		"instruction": func(i int) (string, error) {
			secretName := name.SafeConcatName(source.GetName(), "machine", "plan")
			mps, err := h.secretCache.Get(source.GetNamespace(), secretName)
			if err != nil {
				return "", err
			}

			data := map[string]any{}
			buf := bytes.NewBuffer(mps.Data["applied-output"])

			gzipReader, err := gzip.NewReader(buf)
			if err != nil {
				return "", err
			}
			defer gzipReader.Close()

			if err := json.NewDecoder(gzipReader).Decode(&data); err != nil {
				return "", err
			}

			p, err := h.nodePlanCache.Get(source.GetNamespace(), source.GetName())
			if err != nil {
				return "", err
			}

			if len(p.Spec.Instructions) < i {
				return "", fmt.Errorf("instruction %d does not exist", i)
			}

			instruction := p.Spec.Instructions[i]
			if stdout, ok := data[instruction.Name]; ok {
				return stdout.(string), nil
			}

			return "", fmt.Errorf("stdout not captured for instruction %d", i)
		},
		"leader": func(labelKey string) *unstructured.Unstructured {
			sel := labels.SelectorFromSet(labels.Set{labelKey: "true"})
			plans, err := h.nodePlanCache.List(source.GetNamespace(), sel)
			if err != nil || len(plans) == 0 {
				return nil
			}
			// Sort to ensure the "first" is deterministic if multiple exist
			slices.SortFunc(plans, func(i, j *planv1alpha1.NodePlan) int {
				return strings.Compare(i.Name, j.Name)
			})

			content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(plans[0])
			return &unstructured.Unstructured{Object: content}
		},
		"shard": func(labelKey string) *unstructured.Unstructured {
			sel := labels.SelectorFromSet(labels.Set{labelKey: "true"})
			plans, err := h.nodePlanCache.List(source.GetNamespace(), sel)
			if err != nil || len(plans) == 0 {
				return nil
			}

			// Sort to ensure the index calculation is stable
			slices.SortFunc(plans, func(i, j *planv1alpha1.NodePlan) int {
				return strings.Compare(i.Name, j.Name)
			})

			// Use the current NodePlan's UID for the hash seed
			uid := string(source.GetUID())
			ck := crc32.ChecksumIEEE([]byte(uid))
			index := int(ck) % len(plans)

			content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(plans[index])
			return &unstructured.Unstructured{Object: content}
		},
		"output": func(outputName string, obj *unstructured.Unstructured) string {
			if obj == nil {
				return ""
			}
			annotationKey := fmt.Sprintf("rke.cattle.io/output_%s", outputName)
			val := obj.GetAnnotations()[annotationKey]
			return val
		},
		"owner": func(apiVersionKind string) *corev1.ObjectReference {
			for _, ref := range source.GetOwnerReferences() {
				// Support matching "Kind" or "apiVersion/Kind"
				fullPath := fmt.Sprintf("%s/%s", ref.APIVersion, ref.Kind)
				if strings.EqualFold(fullPath, apiVersionKind) || strings.EqualFold(ref.Kind, apiVersionKind) {
					return &corev1.ObjectReference{
						APIVersion: ref.APIVersion,
						Kind:       ref.Kind,
						Namespace:  source.GetNamespace(),
						Name:       ref.Name,
					}
				}
			}
			return nil
		},
		"fetch": func(ref *corev1.ObjectReference) *unstructured.Unstructured {
			if ref == nil {
				logrus.Errorf("[template] failed to fetch nil ObjectReference")
				return nil
			}
			// Convert APIVersion/Kind to GVK
			gv, err := schema.ParseGroupVersion(ref.APIVersion)
			if err != nil {
				logrus.Errorf("[template] failed to parse apiVersion %s: %v", ref.APIVersion, err)
				return nil
			}
			gvk := gv.WithKind(ref.Kind)

			// Fetch from dynamic client or cache
			// Note: Use your h.dynamic or a cache getter here
			obj, err := h.dynamic.Get(gvk, ref.Namespace, ref.Name)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					logrus.Errorf("[template] failed to fetch %s %s/%s: %v", ref.Kind, ref.Namespace, ref.Name, err)
				}
				return nil
			}

			// 1. Try a direct type assertion (fastest path)
			if unstr, ok := obj.(*unstructured.Unstructured); ok {
				return unstr
			}

			// 2. If it's a concrete type, convert it to Unstructured
			// This ensures your jsonPath and getCondition functions always have a consistent interface
			content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				logrus.Errorf("[template] failed to convert %T to unstructured: %v", obj, err)
				return nil
			}

			return &unstructured.Unstructured{Object: content}
		},
		"isNil": func(val interface{}) bool {
			return val == nil || reflect.ValueOf(val).IsZero()
		},
		"getCondition": func(condType string, obj *unstructured.Unstructured) string {
			if obj == nil {
				return "Unknown"
			}

			data := obj.Object

			// Navigate to .status.conditions
			status, ok := data["status"].(map[string]interface{})
			if !ok {
				return "Unknown"
			}

			conditions, ok := status["conditions"].([]interface{})
			if !ok {
				return "Unknown"
			}

			for _, c := range conditions {
				condition, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if condition["type"] == condType {
					if s, ok := condition["status"].(string); ok {
						return s
					}
				}
			}

			return "Unknown"
		},
		"jsonPath": func(path string, obj interface{}) interface{} {
			// Use k8s.io/client-go/util/jsonpath to extract value
			if obj == nil {
				return nil
			}

			jp := jsonpath.New("plan-eval")
			// Add the '{' and '}' if the user didn't provide them to make it idiomatic
			if !strings.HasPrefix(path, "{") {
				path = "{" + path + "}"
			}

			if err := jp.Parse(path); err != nil {
				return fmt.Errorf("invalid jsonpath %s: %v", path, err)
			}

			var buf bytes.Buffer
			if err := jp.Execute(&buf, obj); err != nil {
				// If path doesn't exist, we return nil/empty rather than erroring
				// out the whole template, allowing for "isNil" checks.
				return nil
			}

			return buf.String()
		},
	}
}
