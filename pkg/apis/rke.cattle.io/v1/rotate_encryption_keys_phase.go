package v1

type RotateEncryptionKeysPhase string

const (

	RotateEncryptionKeysPhaseRotateKeys = RotateEncryptionKeysPhase("RotateKeys")
	RotateEncryptionKeysPhaseRotateKeysRestart = RotateEncryptionKeysPhase("RotateKeysRestart")

	// RotateEncryptionKeysPhaseDone is the state assigned to the RKEControlPlane upon successful completion of the encryption key rotation operation.
	RotateEncryptionKeysPhaseDone = RotateEncryptionKeysPhase("Done")

	// RotateEncryptionKeysPhaseFailed is the state assigned to the RKEControlPlane upon failure of the encryption key rotation operation.
	RotateEncryptionKeysPhaseFailed = RotateEncryptionKeysPhase("Failed")
)
