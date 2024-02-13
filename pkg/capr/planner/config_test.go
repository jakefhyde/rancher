package planner

import (
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	corecontrollers "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	ctrlfake "github.com/rancher/wrangler/pkg/generic/fake"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const defaultNamespace = "fleet-default"

func TestAddAddresses(t *testing.T) {
	ctrl := gomock.NewController(t)
	tests := []struct {
		name        string
		config      map[string]any
		entry       *planEntry
		secrets     corecontrollers.SecretCache
		expectedErr error
	}{
		{
			name:   "has node-ip",
			config: map[string]any{},
			entry: &planEntry{
				Metadata: &plan.Metadata{
					Annotations: map[string]string{},
				},
				Machine: &capiv1beta1.Machine{
					Spec: capiv1beta1.MachineSpec{
						InfrastructureRef: v1.ObjectReference{
							Namespace: defaultNamespace,
							Name:      "test",
						},
					},
				},
			},
			secrets: getSecretCacheMock(ctrl, &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "test-machine-state",
				},
				Data: map[string][]byte{
					"extractedConfig": []byte("{}"),
				},
			}),
		},
		{
			name:   "does not have node-ip",
			config: map[string]any{},
			entry: &planEntry{
				Metadata: &plan.Metadata{
					Annotations: map[string]string{},
				},
				Machine: &capiv1beta1.Machine{
					Spec: capiv1beta1.MachineSpec{
						InfrastructureRef: v1.ObjectReference{
							Namespace: defaultNamespace,
							Name:      "test",
						},
					},
				},
			},
			secrets: getSecretCacheMock(ctrl, &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      "test-machine-state",
				},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := map[string]any{}
			for k, v := range tt.config {
				original[k] = v
			}

			err := addAddresses(tt.secrets, tt.config, tt.entry)
			if tt.expectedErr != nil {
				if err == nil {
					assert.Fail(t, "expected error")
				}
				assert.Equal(t, err.Error(), tt.expectedErr.Error())
			} else {
				assert.True(t, reflect.DeepEqual(original, tt.config))
			}
		})
	}
}

// getSecretCacheMock will return a Mock SecretCache that will return a secret if it is not nil, or an NotFound error.
func getSecretCacheMock(ctrl *gomock.Controller, secret *v1.Secret) *ctrlfake.MockCacheInterface[*v1.Secret] {
	mockSecretCache := ctrlfake.NewMockCacheInterface[*v1.Secret](ctrl)
	if secret != nil {
		mockSecretCache.EXPECT().Get(secret.Namespace, secret.Name).DoAndReturn(func() (*v1.Secret, error) {
			return secret.DeepCopy(), nil
		})
	}
	mockSecretCache.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func() (*v1.Secret, error) {
		return nil, apierrors.NewNotFound(v1.Resource("Secret"), secret.Name)
	})
	return mockSecretCache
}
