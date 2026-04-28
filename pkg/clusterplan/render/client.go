package render

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// RenderClient is the sandboxed read API exposed to template functions.
// Implementations must enforce a GVK + namespace + name allow-list so a
// malicious or buggy template cannot exfiltrate arbitrary cluster state.
//
// The render package never holds a write client; patches against
// arbitrary resources go through the stage controller via a separate
// writer with its own RBAC.
type RenderClient interface {
	// GetSecret fetches a secret by namespace + name. Implementations
	// should restrict by name allow-list.
	GetSecret(namespace, name string) (*corev1.Secret, error)

	// Get fetches a typed object by GVK + namespace + name.
	// Implementations should restrict by GVK allow-list.
	Get(apiVersion, kind, namespace, name string) (runtime.Object, error)

	// List fetches typed objects matching a label selector.
	// Implementations should restrict by GVK allow-list.
	List(apiVersion, kind, namespace string, sel labels.Selector) ([]runtime.Object, error)
}

// ErrNotAllowed is returned by a RenderClient when the caller asks for a
// resource outside the allow-list.
var ErrNotAllowed = errors.New("render: resource not on allow-list")

// ErrNotFound is returned when the requested object does not exist.
var ErrNotFound = errors.New("render: object not found")

// FakeClient is an in-memory RenderClient suitable for unit tests. It
// does not enforce an allow-list — tests can pre-populate any object.
type FakeClient struct {
	Secrets map[string]*corev1.Secret  // key: "<namespace>/<name>"
	Objects map[string]runtime.Object  // key: "<apiVersion>/<kind>/<namespace>/<name>"
	Lists   map[string][]runtime.Object // key: "<apiVersion>/<kind>/<namespace>"
}

func (f *FakeClient) GetSecret(namespace, name string) (*corev1.Secret, error) {
	if s, ok := f.Secrets[namespace+"/"+name]; ok {
		return s, nil
	}
	return nil, ErrNotFound
}

func (f *FakeClient) Get(apiVersion, kind, namespace, name string) (runtime.Object, error) {
	key := fmt.Sprintf("%s/%s/%s/%s", apiVersion, kind, namespace, name)
	if o, ok := f.Objects[key]; ok {
		return o, nil
	}
	return nil, ErrNotFound
}

func (f *FakeClient) List(apiVersion, kind, namespace string, sel labels.Selector) ([]runtime.Object, error) {
	key := fmt.Sprintf("%s/%s/%s", apiVersion, kind, namespace)
	objs := f.Lists[key]
	if sel == nil || sel.Empty() {
		return objs, nil
	}
	var out []runtime.Object
	for _, o := range objs {
		l, ok := objectLabels(o)
		if !ok {
			continue
		}
		if sel.Matches(labels.Set(l)) {
			out = append(out, o)
		}
	}
	return out, nil
}

// objectLabels extracts metadata.labels from a runtime.Object via the
// standard accessor. Returns nil + false if the object isn't a metav1
// object. Used by FakeClient.List for selector matching in tests.
func objectLabels(obj runtime.Object) (map[string]string, bool) {
	type metaProvider interface {
		GetLabels() map[string]string
	}
	if m, ok := obj.(metaProvider); ok {
		return m.GetLabels(), true
	}
	// runtime.Object that wraps a metav1.Object via accessor.
	type accessor interface {
		GetObjectMeta() interface{ GetLabels() map[string]string }
	}
	if a, ok := obj.(accessor); ok {
		return a.GetObjectMeta().GetLabels(), true
	}
	return nil, false
}
