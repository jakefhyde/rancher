package configserver

import (
	"net/http"
	"strings"

	"github.com/rancher/rancher/pkg/capr"
	"github.com/sirupsen/logrus"
	api "k8s.io/api/core/v1"
)

func (r *RKE2ConfigServer) findMachineByProvisioningSA(req *http.Request, machineID string) (string, string, error) {
	logrus.Debugf("[rke2configserver] [%s] finding machine by provisioning service account", machineID)
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	secrets, err := r.secretsCache.GetByIndex(tokenIndex, token)
	if err != nil {
		return "", "", err
	} else if len(secrets) == 0 {
		logrus.Debugf("[rke2configserver] [%s] could not find secret for provisioning service account", machineID)
		return "", "", nil
	}

	sa, err := r.serviceAccountsCache.Get(secrets[0].Namespace, secrets[0].Annotations[api.ServiceAccountNameKey])
	if err != nil {
		return "", "", err
	}

	if sa.Labels[capr.RoleLabel] != capr.RoleBootstrap {
		logrus.Debugf("[rke2configserver] [%s] provisioning service account does not have required label", machineID)
		return "", "", nil
	}

	if string(sa.UID) != secrets[0].Annotations[api.ServiceAccountUIDKey] {
		logrus.Debugf("[rke2configserver] [%s] provisioning service account token UID annotation does not match service account", machineID)
		return "", "", nil
	}

	if foundParent, err := capr.IsOwnedByMachine(r.bootstrapCache, sa.Labels[capr.MachineNameLabel], sa); err != nil {
		return "", "", err
	} else if !foundParent {
		logrus.Debugf("[rke2configserver] [%s] provisioning service account not owned by bootstrap object", machineID)
		return "", "", nil
	}

	return sa.Namespace, sa.Labels[capr.MachineNameLabel], nil
}
