package certrotation

import (
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	provcontrollers "github.com/rancher/rancher/pkg/generated/controllers/provisioning.cattle.io/v1"
	rkecontrollers "github.com/rancher/rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/rancher/tests/framework/extensions/clusters"
	"github.com/rancher/rancher/tests/framework/extensions/defaults"
	"github.com/rancher/rancher/tests/framework/extensions/provisioning"
	"github.com/rancher/rancher/tests/framework/pkg/wait"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespace = "fleet-default"
)

// RotateCerts rotates the certificates in a cluster
func RotateCerts(clusterName string, clusterController provcontrollers.ClusterClient, rkeControlPlane rkecontrollers.RKEControlPlaneClient) error {
	cluster, err := clusterController.Get(namespace, clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	cluster = cluster.DeepCopy()
	if cluster.Spec.RKEConfig.RotateCertificates == nil {
		cluster.Spec.RKEConfig.RotateCertificates = &rkev1.RotateCertificates{}
	}
	cluster.Spec.RKEConfig.RotateCertificates.Generation++

	cluster, err = clusterController.Update(cluster)
	if err != nil {
		return err
	}

	logrus.Infof("updated cluster, certs are rotating...")

	result, err := rkeControlPlane.Watch(namespace, metav1.ListOptions{
		FieldSelector:  "metadata.name=" + clusterName,
		TimeoutSeconds: &defaults.WatchTimeoutSeconds,
	})
	if err != nil {
		return err
	}

	checkFunc := provisioning.CertRotationCompleteCheckFunc(cluster.Spec.RKEConfig.RotateCertificates.Generation)
	logrus.Infof("waiting for certs to rotate, checking status now...")
	err = wait.WatchWait(result, checkFunc)
	if err != nil {
		return err
	}

	result, err = clusterController.Watch(namespace, metav1.ListOptions{
		FieldSelector:  "metadata.name=" + clusterName,
		TimeoutSeconds: &defaults.WatchTimeoutSeconds,
	})
	if err != nil {
		return err
	}

	clusterCheckFunc := clusters.IsProvisioningClusterReady
	logrus.Infof("waiting for cluster to become active again, checking status now...")
	err = wait.WatchWait(result, clusterCheckFunc)
	if err != nil {
		return err
	}
	return nil
}
