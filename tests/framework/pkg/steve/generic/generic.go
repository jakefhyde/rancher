// Package generic provides generic types and implementations for Controllers, Clients, and Caches.
package generic

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/rancher/lasso/pkg/controller"
	v1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	"github.com/rancher/rancher/tests/framework/pkg/session"
	"github.com/rancher/wrangler/pkg/generic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

type Controller[T generic.RuntimeMetaObject, TList runtime.Object] struct {
	generic.ControllerInterface[T, TList]
	Session       *session.Session
	Steve         *v1.Client
	gvk           schema.GroupVersionKind
	groupResource schema.GroupResource
	objType       reflect.Type
	objListType   reflect.Type
}

func (s *Controller[T, TList]) Watch(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return s.ControllerInterface.Watch(namespace, opts)
}

func (s *Controller[T, TList]) Get(namespace, name string, opts metav1.GetOptions) (T, error) {
	result := reflect.New(s.objType).Interface().(T)

	steveType := fmt.Sprintf("%s.%s", s.gvk.Group, strings.ToLower(s.gvk.Kind))

	id := name
	if namespace != "" {
		id = namespace + "/" + id
	}

	apiObj, err := s.Steve.SteveType(steveType).ByID(id)
	if err != nil {
		return result, err
	}

	err = v1.ConvertToK8sType(apiObj.JSONResp, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func ListOptionsToQuery(opts metav1.ListOptions) (url.Values, error) {
	var values url.Values
	if opts.FieldSelector != "" {
		//values["fieldSelector"] = opts.FieldSelector
	}
	panic("not implemented")

	return values, nil
}

func (s *Controller[T, TList]) List(namespace string, opts metav1.ListOptions) (TList, error) {
	result := reflect.New(s.objListType).Interface().(TList)

	steveType := fmt.Sprintf("%s.%s", s.gvk.Group, strings.ToLower(s.gvk.Kind))

	values, err := ListOptionsToQuery(opts)
	if err != nil {
		return result, err
	}

	// NamespacedSteveClient should work regardless if a namespace is specified or not
	apiObj, err := s.Steve.SteveType(steveType).NamespacedSteveClient(namespace).List(values)
	if err != nil {
		return result, err
	}

	for _, obj := range apiObj.Data {
		r := reflect.New(s.objType).Interface().(T)

		err = v1.ConvertToK8sType(obj.JSONResp, r)
		if err != nil {
			return result, err
		}
		panic("not implemented")
	}

	return result, nil
}

func (s *Controller[T, TList]) Create(t T) (T, error) {
	result := reflect.New(s.objType).Interface().(T)

	steveType := fmt.Sprintf("%s.%s", s.gvk.Group, strings.ToLower(s.gvk.Kind))

	apiObj, err := s.Steve.SteveType(steveType).Create(t)
	if err != nil {
		return result, err
	}

	err = v1.ConvertToK8sType(apiObj.JSONResp, result)
	if err != nil {
		return result, err
	}

	s.Session.RegisterCleanupFunc(func() error {
		return s.Delete(result.GetNamespace(), result.GetName(), &metav1.DeleteOptions{})
	})

	return result, nil
}

func (s *Controller[T, TList]) Update(t T) (T, error) {
	result := reflect.New(s.objType).Interface().(T)

	steveType := fmt.Sprintf("%s.%s", s.gvk.Group, strings.ToLower(s.gvk.Kind))

	apiObj, err := s.Steve.SteveType(steveType).ByID(t.GetNamespace() + "/" + t.GetName())
	if err != nil {
		return result, err
	}

	apiObj, err = s.Steve.SteveType(steveType).Update(apiObj, t)
	if err != nil {
		return result, err
	}

	err = v1.ConvertToK8sType(apiObj.JSONResp, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

// NewController creates a new controller for the given Object type and ObjectList type.
func NewController[T generic.RuntimeMetaObject, TList runtime.Object](client *v1.Client, session *session.Session, gvk schema.GroupVersionKind, resource string, namespaced bool, controller controller.SharedControllerFactory) *Controller[T, TList] {
	var obj T
	objPtrType := reflect.TypeOf(obj)
	if objPtrType.Kind() != reflect.Pointer {
		panic(fmt.Sprintf("Controller requires Object T to be a pointer not %v", objPtrType))
	}
	var objList TList
	objListPtrType := reflect.TypeOf(objList)
	if objListPtrType.Kind() != reflect.Pointer {
		panic(fmt.Sprintf("Controller requires Object TList to be a pointer not %v", objListPtrType))
	}
	embedded := generic.NewController[T, TList](gvk, resource, namespaced, controller)
	return &Controller[T, TList]{
		ControllerInterface: embedded,
		Session:             session,
		Steve:               client,
		gvk:                 gvk,
		groupResource: schema.GroupResource{
			Group:    gvk.Group,
			Resource: resource,
		},
		objType:     objPtrType.Elem(),
		objListType: objListPtrType.Elem(),
	}
}
