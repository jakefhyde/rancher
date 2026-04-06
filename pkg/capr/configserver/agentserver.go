package configserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/rancher/rancher/pkg/wrangler"
	"github.com/rancher/shepherd/clients/dynamic"
	"github.com/rancher/wrangler/v3/pkg/apply"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	wname "github.com/rancher/wrangler/v3/pkg/name"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierror "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type LocalObjectReference struct {
	Kind       string `json:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
}

type AgentConnectRequest struct {
	// -H X-Cattle-Id: <machine-id>
	MachineID string `json:"machineId"`
}

type AgentConnectResponse struct {
	ObjectRef  LocalObjectReference `json:"objectRef"`
	Kubeconfig string               `json:"kubeconfig"`
}

type Beacon struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BeaconSpec   `json:"spec,omitempty"`
	Status BeaconStatus `json:"status,omitempty"`
}

type BeaconSpec struct {
	Active bool `json:"active"`
}

type BeaconStatus struct {
	Active bool `json:"active"`
}

type agentServer struct {
	secretCache         corecontrollers.SecretCache
	secrets             corecontrollers.SecretClient
	serviceAccountCache corecontrollers.ServiceAccountCache
}

func (s *agentServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case "/v1/agent/register":
		s.register(rw, req)
	case "/v1/agent/connect":
		s.connect(rw, req)
	}
}

// register receives a request when the system-agent is registered (at `/v1/agent/register`) and will return the
// system-agent configuration for the node, including the name of the beacon and a kubeconfig for watching it.
// The `X-Cattle-Id` is generated at installation time by the server and is expected to be unique across machines.
// The `/v1/agent/register` endpoint is idempotent and will return the system-agent installation script for a given
// machine id as long as the cluster registration token is valid.
// The `X-Cattle-Token` header contains the service account token used to watch the beacon.
func (s *agentServer) register(rw http.ResponseWriter, req *http.Request) {
	machineID := req.Header.Get("X-Cattle-Id")
	logrus.Debugf("[agentserver] parsed %s as machineID", machineID)
	if machineID == "" {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}

	// find cluster this belongs to based on CRT

	// get beacon object and ensure svct acct+token

	// return
}

// connect receives a request when the system-agent sees the beacon is active and attempts to download it's specific
// configuration (at `/v1/agent/connect`).
// The `X-Cattle-Id` is generated at installation time by the server and is expected to be unique across machines.
// The `/v1/agent/connect` endpoint is idempotent and will return the system-agent configuration for a given
// machine id as long as the machine's specific token is valid.
// The `X-Cattle-Token` header contains the service account token used to watch the beacon.
func (s *agentServer) connect(rw http.ResponseWriter, req *http.Request) {
	machineID := req.Header.Get("X-Cattle-Id")
	logrus.Debugf("[agentserver] parsed %s as machineID", machineID)
	if machineID == "" {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")

	// find cluster this belongs to based on svc acct token
	secrets, err := s.secretCache.GetByIndex(tokenIndex, token)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(secrets) == 0 {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	}

	tokenSecret := secrets[0]

	// todo: actually figure out the namespace this belongs to.
	sa, err := s.serviceAccountCache.Get(tokenSecret.Namespace, tokenSecret.Annotations[corev1.ServiceAccountNameKey])
	if apierror.IsNotFound(err) {
		rw.WriteHeader(http.StatusUnauthorized)
		return
	} else if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	// todo: ensure the role matches

	// get beacon object and ensure beacon is active
	beacon := &Beacon{}
	if !beacon.Status.Active {
		rw.WriteHeader(http.StatusForbidden)
		return
	}

	// create machine-plan, svc acct, token, role & rb
	hash := sha256.Sum256([]byte(machineID))
	name := strings.Join([]string{"agent", base64.URLEncoding.EncodeToString(hash[:][:12])}, "-")

	secret, err := s.secretCache.Get(tokenSecret.Namespace, name)
	if apierror.IsNotFound(err) {
		d := map[string]any{}
		for k, v := range req.Header {
			if strings.HasPrefix(k, "X-Cattle-") {
				d[strings.ToLower(strings.TrimPrefix(k, "X-Cattle-"))] = v[0]
			}
		}

		b, err := json.Marshal(d)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: tokenSecret.Namespace,
				Name:      name,
			},
			Type: AgentRequestSecretType,
			Data: map[string][]byte{
				"data": b,
			},
		}
		secret, err = s.secrets.Create(secret)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}

	// read beacon (or potentially clusterplan) and annotate the representative object marking it as registered
	// do we need to annotate the object? Why?

	// return
}

// template is responsible for
var (
	template = `

`
)

func (s *agentServer) findServiceAccountByToken(token string) error {

}

const (
	MachinePlanSecretType  = "plan.cattle.io/machine-plan"
	AgentRequestSecretType = "plan.cattle.io/agent-request"

	MachineIDLabel = "plan.cattle.io/machine-id"

	OwnerGVKAnnotation       = "plan.cattle.io/owner-gvk"
	OwnerNameAnnotation      = "plan.cattle.io/owner-name"
	OwnerNamespaceAnnotation = "plan.cattle.io/owner-namespace"

	MachineRoleLabelPrefix = "machine-role.plan.cattle.io/"

	ServiceAccountRoleLabel       = "plan.cattle.io/service-account-role"
	ServiceAccountRoleBeacon      = "beacon"       // alternatively beacon
	ServiceAccountRoleMachinePlan = "machine-plan" // alternatively machine-plan
)

type agentHandler struct {
	secrets corecontrollers.SecretClient
	dynamic dynamic.Client
	apply   apply.Apply
	mapper  meta.RESTMapper
}

func Register(w *wrangler.Context) {
	h := &agentHandler{
		mapper: w.RESTMapper,
	}

}

// when a request comes in, we need to find the

func (h *agentHandler) OnChange(_ string, secret *corev1.Secret) error {
	if secret == nil {
		return nil
	}

	if secret.Type != AgentRequestSecretType {
		return nil
	}

	go func() {
		time.Sleep(time.Minute)
		_ = h.secrets.Delete(secret.Namespace, secret.Name, nil)
	}()

	// owner is represented as a series of annotations because
	if secret.Annotations[OwnerGVKAnnotation] == "" {
		return errors.New("secret does not have an owner GVK")
	}

	if secret.Annotations[OwnerNameAnnotation] == "" {
		return errors.New("secret does not have an owner name")
	}

	if secret.Annotations[OwnerNamespaceAnnotation] == "" {
		return errors.New("secret does not have an owner namespace")
	}

	gvk, err := ParseGVKString(secret.Annotations[OwnerGVKAnnotation])
	if err != nil {
		return err
	}

	mapping, err := h.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	dc := h.dynamic.Resource(mapping.Resource).Namespace(secret.Annotations[OwnerNamespaceAnnotation])

	obj, err := dc.Get(context.TODO(), secret.Annotations[OwnerNameAnnotation], metav1.GetOptions{})
	if err != nil {
		return err
	}

	objs, err := h.createMachinePlanForObject(obj)
	if err != nil {
		return err
	}

	err = h.apply.WithOwner(obj).ApplyObjects(objs...)
	if err != nil {
		return err
	}

	return nil
}

func ParseGVKString(gvkStr string) (schema.GroupVersionKind, error) {
	parts := strings.Split(gvkStr, ", Kind=")
	if len(parts) != 2 {
		return schema.GroupVersionKind{}, fmt.Errorf("invalid GVK string format: %s", gvkStr)
	}

	gvStr := parts[0]
	kind := parts[1]

	gv, err := schema.ParseGroupVersion(gvStr)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("could not parse group/version: %v", err)
	}

	return gv.WithKind(kind), nil
}

func (h *agentHandler) createMachinePlanForObject(obj *unstructured.Unstructured) ([]runtime.Object, error) {
	namespace := obj.GetNamespace()
	name := wname.SafeConcatName(obj.GetName(), "machine-plan")

	labels := map[string]string{}
	annotations := map[string]string{}

	objLabels := obj.GetLabels()
	if objLabels == nil {
		return nil, errors.New("object does not have labels")
	}

	if objLabels[MachineIDLabel] == "" {
		return nil, errors.New("object does not have a machine ID")
	}
	labels[MachineIDLabel] = objLabels[MachineIDLabel]

	// copy machine role labels
	for k, v := range objLabels {
		if strings.HasPrefix(k, MachineRoleLabelPrefix) {
			labels[k] = v
		}
	}

	saLabels := maps.Clone(labels)
	saLabels[ServiceAccountRoleLabel] = ServiceAccountRoleMachinePlan

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Labels:      saLabels,
			Annotations: annotations,
		},
	}
	plan := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:         []string{"watch", "get", "update", "list"},
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				ResourceNames: []string{name},
			},
		},
	}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.Name,
				Namespace: sa.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     name,
		},
	}

	objs := []runtime.Object{sa, plan, role, rb}
	return objs, nil
}
