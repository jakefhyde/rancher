package day2ops

import (
	"context"

	"github.com/rancher/rancher/pkg/wrangler"
)

//type handler struct {
//	planner *caprplanner.Planner
//}

func Register(ctx context.Context, clients *wrangler.CAPIContext) {
	//planner := caprplanner.New(ctx, clients, nil)
	//h := &handler{
	//	planner: planner,
	//}

}

//func (h *handler) OnChange(cp *rkev1.RKEControlPlane, status rkev1.RKEControlPlaneStatus) (rkev1.RKEControlPlaneStatus, error) {
//if !cp.DeletionTimestamp.IsZero() {
//	return status, nil
//}
//
//info := NewCAPRDistroInfo(cp)
//
//if cp.Spec.ETCDSnapshotCreate != nil && cp.Spec.ETCDSnapshotCreate != status.ETCDSnapshotCreate {
//	if phase, err := p.createEtcdSnapshot(info, cp.Spec.ETCDSnapshotCreate, status.ETCDSnapshotCreatePhase, plan); err != nil {
//		return status, err
//	} else if phase != "" {
//		status.ETCDSnapshotCreatePhase = phase
//		return status, errWaiting("refreshing etcd create state")
//	}
//	status.ETCDSnapshotCreate = cp.Spec.ETCDSnapshotCreate
//	status.ETCDSnapshotCreatePhase = ""
//	return status, errWaiting("refreshing etcd create state")
//}
//
//if status, err = p.restoreEtcdSnapshot(info, cp.Spec.ETCDSnapshotRestore, clusterSecretTokens, plan, currentVersion); err != nil {
//	return status, err
//}
//
//if cp.Spec.RotateCertificates != nil && cp.Spec.RotateCertificates.Generation != status.CertificateRotationGeneration {
//	if err := p.rotateCertificates(info, cp.Spec.RotateCertificates, plan); err != nil {
//		return status, err
//	}
//	status.CertificateRotationGeneration = cp.Spec.RotateCertificates.Generation
//	return status, errWaiting("refreshing encryption key rotation state")
//}
//
//if cp.Spec.RotateEncryptionKeys != nil && cp.Spec.RotateEncryptionKeys != status.RotateEncryptionKeys {
//	if phase, err := p.rotateEncryptionKeys(info, cp.Spec.RotateEncryptionKeys, status.RotateEncryptionKeysPhase, plan, nil, nil); err != nil {
//		return status, err
//	} else if phase != "" {
//		status.RotateEncryptionKeysPhase = phase
//		return status, errWaiting("refreshing encryption key rotation state")
//	}
//	status.RotateEncryptionKeys = cp.Spec.RotateEncryptionKeys
//	status.RotateEncryptionKeysPhase = ""
//	return status, errWaiting("refreshing encryption key rotation state")
//}
//}
