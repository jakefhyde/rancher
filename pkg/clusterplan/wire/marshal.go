package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
)

// errEmptyProbeName is returned by ToWire when a probe lacks a name; the
// existing wire format keys probes by name, so an empty name is unrecoverable.
var errEmptyProbeName = errors.New("wire: probe.name must not be empty")

// PlanSecretKey* are the keys the existing system-agent reads/writes on
// an rke.cattle.io/machine-plan Secret. Mirrored here so the adapter
// doesn't have to import the planner.
const (
	PlanSecretKeyPlan             = "plan"
	PlanSecretKeyAppliedChecksum  = "applied-checksum"
	PlanSecretKeyAppliedPlan      = "applied-plan"
	PlanSecretKeyFailedChecksum   = "failed-checksum"
	PlanSecretKeyFailureCount     = "failure-count"
	PlanSecretKeyMaxFailures      = "max-failures"
	PlanSecretKeyAppliedOutput    = "applied-output"
	PlanSecretKeyAppliedPeriodic  = "applied-periodic-output"
	PlanSecretKeyProbeStatuses    = "probe-statuses"
)

// Marshal encodes a wire NodePlan as the JSON bytes that go into the
// "plan" key of a machine-plan secret, and returns the sha-256 checksum
// of those bytes as a hex string. The checksum format matches the
// PlanHash function in pkg/capr/planner/store.go so existing planner
// code that compares checksums continues to work unchanged.
func Marshal(p rkeplan.NodePlan) (data []byte, checksum string, err error) {
	data, err = json.Marshal(p)
	if err != nil {
		return nil, "", fmt.Errorf("wire.Marshal: %w", err)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

// Unmarshal decodes the "plan" key bytes back into a wire NodePlan.
// Used by FromWireStatus to recover the agent's last-applied plan when
// projecting outputs.
func Unmarshal(data []byte) (rkeplan.NodePlan, error) {
	var p rkeplan.NodePlan
	if len(data) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, fmt.Errorf("wire.Unmarshal: %w", err)
	}
	return p, nil
}
