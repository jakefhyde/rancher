package wire

import (
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkeplan "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1/plan"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func fixedTime() time.Time {
	return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
}

func TestToWireFiles(t *testing.T) {
	in := v1alpha1.NodePlanSpec{
		Files: []v1alpha1.File{
			{Path: "/etc/rke2/config", Content: "cni: calico\n", Permissions: "0600"},
			{Path: "/etc/cni/net.d/x", Content: "{}", Drain: true},
		},
	}
	out, err := ToWire(in)
	if err != nil {
		t.Fatalf("ToWire: %v", err)
	}
	if len(out.Files) != 2 {
		t.Fatalf("Files len = %d, want 2", len(out.Files))
	}
	// Drain=false → Minor=true.
	if !out.Files[0].Minor {
		t.Errorf("Files[0].Minor = false, want true (Drain was false)")
	}
	// Drain=true → Minor=false.
	if out.Files[1].Minor {
		t.Errorf("Files[1].Minor = true, want false (Drain was true)")
	}
	if out.Files[0].Permissions != "0600" {
		t.Errorf("Permissions not preserved")
	}
}

func TestToWireInstructionsSplitOneTimeAndPeriodic(t *testing.T) {
	in := v1alpha1.NodePlanSpec{
		Instructions: []v1alpha1.Instruction{
			{Name: "snapshot", Command: "rke2", Args: []string{"etcd-snapshot", "save"}, SaveOutput: true},
			{Name: "tail-logs", Command: "tail", Args: []string{"-f", "/var/log/x"}, Strategy: v1alpha1.InstructionStrategy{PeriodSeconds: 60}},
		},
	}
	out, err := ToWire(in)
	if err != nil {
		t.Fatalf("ToWire: %v", err)
	}
	if len(out.Instructions) != 1 {
		t.Errorf("OneTime len = %d, want 1", len(out.Instructions))
	}
	if !out.Instructions[0].SaveOutput {
		t.Errorf("SaveOutput not preserved")
	}
	if len(out.PeriodicInstructions) != 1 {
		t.Errorf("Periodic len = %d, want 1", len(out.PeriodicInstructions))
	}
	if out.PeriodicInstructions[0].PeriodSeconds != 60 {
		t.Errorf("PeriodSeconds = %d, want 60", out.PeriodicInstructions[0].PeriodSeconds)
	}
}

func TestToWireProbesKeyedByName(t *testing.T) {
	in := v1alpha1.NodePlanSpec{
		Probes: []v1alpha1.Probe{{
			Name: "kubelet",
			RetryStrategy: v1alpha1.ProbeStrategy{
				InitialDelaySeconds: 5, TimeoutSeconds: 2, SuccessThreshold: 1, FailureThreshold: 3,
			},
			HTTPGetAction: &v1alpha1.HTTPGetAction{URL: "https://localhost:10250/healthz", Insecure: true},
		}},
	}
	out, err := ToWire(in)
	if err != nil {
		t.Fatalf("ToWire: %v", err)
	}
	p, ok := out.Probes["kubelet"]
	if !ok {
		t.Fatalf("probe not keyed by name; got: %+v", out.Probes)
	}
	if p.HTTPGetAction.URL != "https://localhost:10250/healthz" {
		t.Errorf("URL not preserved")
	}
	if p.FailureThreshold != 3 {
		t.Errorf("FailureThreshold not preserved")
	}
}

func TestToWireRejectsEmptyProbeName(t *testing.T) {
	in := v1alpha1.NodePlanSpec{Probes: []v1alpha1.Probe{{Name: ""}}}
	if _, err := ToWire(in); err == nil {
		t.Errorf("expected error for empty probe name")
	}
}

func TestMarshalRoundtripChecksumStable(t *testing.T) {
	in, _ := ToWire(v1alpha1.NodePlanSpec{
		Files: []v1alpha1.File{{Path: "/x", Content: "hi"}},
		Instructions: []v1alpha1.Instruction{{Name: "echo", Command: "echo", Args: []string{"hi"}}},
	})
	dataA, sumA, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	dataB, sumB, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if sumA != sumB {
		t.Errorf("Marshal not deterministic: %s vs %s", sumA, sumB)
	}
	if string(dataA) != string(dataB) {
		t.Errorf("Marshal bytes differ across calls")
	}
	got, err := Unmarshal(dataA)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Files) != 1 || got.Files[0].Content != "hi" {
		t.Errorf("Unmarshal lost data: %+v", got)
	}
}

func TestExtractOutputs(t *testing.T) {
	encoded, err := EncodeAppliedOutput(map[string][]byte{
		"join-info": []byte(`{"clientURLs":["https://10.0.0.1:9345"]}`),
		"version":   []byte("v1.31.1\n"),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	secret := map[string][]byte{PlanSecretKeyAppliedOutput: encoded}

	outs := []v1alpha1.Output{
		{Name: "join-url", Source: "join-info:.clientURLs[0]", Persistent: true},
		{Name: "raw-version", Source: "version"},
		{Name: "missing", Source: "not-an-instruction"},
	}
	got, err := ExtractOutputs(secret, outs)
	if err != nil {
		t.Fatalf("ExtractOutputs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 outputs (missing skipped), got %d: %+v", len(got), got)
	}
	if got["join-url"].Value != "https://10.0.0.1:9345" {
		t.Errorf("join-url = %q", got["join-url"].Value)
	}
	if !got["join-url"].Persistent {
		t.Errorf("join-url should be persistent")
	}
	if got["raw-version"].Value != "v1.31.1\n" {
		t.Errorf("raw-version = %q", got["raw-version"].Value)
	}
}

func TestExtractOutputsEmpty(t *testing.T) {
	got, err := ExtractOutputs(nil, []v1alpha1.Output{{Name: "a", Source: "x"}})
	if err != nil {
		t.Fatalf("ExtractOutputs: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no outputs, got %v", got)
	}
}

func TestFromWireStatusPhases(t *testing.T) {
	expected := "abc123"
	cases := []struct {
		name       string
		secret     map[string][]byte
		max        int
		wantPhase  string
		wantReady  *metav1.ConditionStatus
	}{
		{
			name:      "pending — no applied checksum",
			secret:    map[string][]byte{},
			wantPhase: v1alpha1.NodePlanPhasePending,
		},
		{
			name: "succeeded — checksum match, no failures",
			secret: map[string][]byte{
				PlanSecretKeyAppliedChecksum: []byte(expected),
			},
			wantPhase: v1alpha1.NodePlanPhaseSucceeded,
			wantReady: ptr(metav1.ConditionTrue),
		},
		{
			name: "running — checksum mismatch, no failures",
			secret: map[string][]byte{
				PlanSecretKeyAppliedChecksum: []byte("stale"),
			},
			wantPhase: v1alpha1.NodePlanPhaseRunning,
		},
		{
			name: "running — failure-count > 0 but below threshold",
			secret: map[string][]byte{
				PlanSecretKeyAppliedChecksum: []byte(expected),
				PlanSecretKeyFailureCount:    []byte("2"),
			},
			max:       5,
			wantPhase: v1alpha1.NodePlanPhaseRunning,
			wantReady: ptr(metav1.ConditionFalse),
		},
		{
			name: "failed — failure-count meets MaxFailures",
			secret: map[string][]byte{
				PlanSecretKeyAppliedChecksum: []byte(expected),
				PlanSecretKeyFailureCount:    []byte("5"),
			},
			max:       5,
			wantPhase: v1alpha1.NodePlanPhaseFailed,
			wantReady: ptr(metav1.ConditionFalse),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FromWireStatus(c.secret, StatusInputs{
				ExpectedChecksum: expected,
				MaxFailures:      c.max,
				Now:              fixedTime,
			})
			if err != nil {
				t.Fatalf("FromWireStatus: %v", err)
			}
			if got.Phase != c.wantPhase {
				t.Errorf("Phase = %q, want %q", got.Phase, c.wantPhase)
			}
			if c.wantReady != nil {
				ready := findCondition(got.Conditions, v1alpha1.ConditionReady)
				if ready == nil {
					t.Errorf("missing Ready condition")
				} else if ready.Status != *c.wantReady {
					t.Errorf("Ready = %q, want %q", ready.Status, *c.wantReady)
				}
			}
		})
	}
}

func TestFromWireStatusPropagatesOutputs(t *testing.T) {
	encoded, err := EncodeAppliedOutput(map[string][]byte{
		"snap": []byte(`{"name":"snap-001"}`),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	secret := map[string][]byte{
		PlanSecretKeyAppliedChecksum: []byte("matched"),
		PlanSecretKeyAppliedOutput:   encoded,
	}
	got, err := FromWireStatus(secret, StatusInputs{
		ExpectedChecksum: "matched",
		Outputs:          []v1alpha1.Output{{Name: "snap-name", Source: "snap:.name"}},
		Now:              fixedTime,
	})
	if err != nil {
		t.Fatalf("FromWireStatus: %v", err)
	}
	if got.Outputs["snap-name"].Value != "snap-001" {
		t.Errorf("snap-name = %q, want snap-001", got.Outputs["snap-name"].Value)
	}
}

func TestSplitSource(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantJP   string
	}{
		{"plain", "plain", ""},
		{"name:.foo", "name", ".foo"},
		{"name:{.spec.x}", "name", "{.spec.x}"},
		{"  spaced  ", "spaced", ""},
	}
	for _, c := range cases {
		n, j := splitSource(c.in)
		if n != c.wantName || j != c.wantJP {
			t.Errorf("splitSource(%q) = (%q,%q), want (%q,%q)", c.in, n, j, c.wantName, c.wantJP)
		}
	}
}

// --- helpers ----------------------------------------------------------------

func ptr(s metav1.ConditionStatus) *metav1.ConditionStatus { return &s }

func findCondition(cs []metav1.Condition, t string) *metav1.Condition {
	for i := range cs {
		if cs[i].Type == t {
			return &cs[i]
		}
	}
	return nil
}

// Compile-time check that the wire types are usable from outside the
// package — referenced via unused alias so the import is preserved if
// the test file is the only consumer.
var _ = rkeplan.NodePlan{}
