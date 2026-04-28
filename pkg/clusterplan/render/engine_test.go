package render

import (
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func fixedNow() time.Time {
	return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
}

func newTestEngine() *Engine {
	c := &FakeClient{
		Secrets: map[string]*corev1.Secret{
			"fleet-default/agent": {
				Data: map[string][]byte{
					"token": []byte("s3cret"),
				},
			},
		},
	}
	return New(c, fixedNow)
}

func TestRenderClusterPlanMinimal(t *testing.T) {
	body := `
operation: etcd-snapshot-create
clusterRef:
  apiVersion: provisioning.cattle.io/v1
  kind: Cluster
  namespace: fleet-default
  name: foo
lifecycle:
  cancellable: false
  cancelsOthers: false
  timeoutSeconds: 1800
plan:
  - name: snapshot
    selector:
      matchLabels:
        rke.cattle.io/etcd-role: "true"
    instructions:
      - name: create
        command: rke2
        args: ["etcd-snapshot", "save"]
`
	ctx := ClusterPlanContext{
		Cluster:   ClusterReference{Type: "capr", Namespace: "fleet-default", Name: "foo"},
		Inputs:    map[string]string{},
		Timestamp: fixedNow().UTC().Format(time.RFC3339),
	}
	spec, err := newTestEngine().RenderClusterPlan(body, ctx)
	if err != nil {
		t.Fatalf("RenderClusterPlan: %v", err)
	}
	if spec.Operation != "etcd-snapshot-create" {
		t.Errorf("Operation = %q, want etcd-snapshot-create", spec.Operation)
	}
	if len(spec.Plan) != 1 {
		t.Fatalf("Plan len = %d, want 1", len(spec.Plan))
	}
	if spec.Plan[0].Name != "snapshot" {
		t.Errorf("Plan[0].Name = %q, want snapshot", spec.Plan[0].Name)
	}
	if got := spec.Plan[0].Instructions[0].Command; got != "rke2" {
		t.Errorf("instruction command = %q, want rke2", got)
	}
}

func TestRenderClusterPlanWithSecretAndTimestamp(t *testing.T) {
	body := `
operation: bootstrap
clusterRef: {apiVersion: provisioning.cattle.io/v1, kind: Cluster, namespace: fleet-default, name: foo}
lifecycle: {timeoutSeconds: 600}
plan:
  - name: write-token
    selector: {matchLabels: {rke.cattle.io/etcd-role: "true"}}
    files:
      - path: /etc/rke2/token
        content: '{{ secret "agent" "token" }}'
        permissions: "0600"
    instructions:
      - name: stamp
        command: echo
        args: ['{{ .Timestamp }}']
`
	ctx := ClusterPlanContext{
		Cluster:   ClusterReference{Type: "capr", Namespace: "fleet-default", Name: "foo"},
		Timestamp: fixedNow().UTC().Format(time.RFC3339),
	}
	spec, err := newTestEngine().RenderClusterPlan(body, ctx)
	if err != nil {
		t.Fatalf("RenderClusterPlan: %v", err)
	}
	if got := spec.Plan[0].Files[0].Content; got != "s3cret" {
		t.Errorf("file content = %q, want s3cret", got)
	}
	if got := spec.Plan[0].Instructions[0].Args[0]; !strings.HasPrefix(got, "2026-04-27") {
		t.Errorf("timestamp arg = %q, want 2026-04-27...", got)
	}
}

func TestRenderClusterPlanRejectsOutputAtOuterLayer(t *testing.T) {
	// `output` is intentionally absent from outerFuncs; referencing it
	// must surface a parse error so the author notices early.
	body := `
operation: x
clusterRef: {apiVersion: a/b, kind: c, namespace: ns, name: n}
lifecycle: {timeoutSeconds: 600}
plan:
  - name: bad
    selector: {matchLabels: {a: b}}
    files:
      - path: /x
        content: '{{ output "a=b" "join-url" }}'
`
	if _, err := newTestEngine().RenderClusterPlan(body, ClusterPlanContext{}); err == nil {
		t.Fatalf("expected error referencing output at outer layer")
	}
}

func TestRenderNodePlanSpecResolvesOutputs(t *testing.T) {
	in := v1alpha1.NodePlanSpec{
		Files: []v1alpha1.File{{
			Path:    "/etc/rke2/server",
			Content: `{{ output "rke.cattle.io/bootstrap-role=true" "join-url" }}`,
		}},
		Instructions: []v1alpha1.Instruction{{
			Name:    "join",
			Command: "rke2",
			Args:    []string{"server", "--server", `{{ output "rke.cattle.io/bootstrap-role=true" "join-url" }}`},
		}},
	}
	ctx := NodePlanContext{
		Cluster:     ClusterReference{Type: "capr", Namespace: "fleet-default", Name: "foo"},
		ClusterType: "capr",
		NodeLabels:  map[string]string{"rke.cattle.io/control-plane-role": "true"},
		Outputs: map[string]NodeOutputs{
			"node-a": {
				Labels: map[string]string{
					"rke.cattle.io/etcd-role":      "true",
					"rke.cattle.io/bootstrap-role": "true",
				},
				Values: map[string]string{"join-url": "https://10.0.0.1:9345"},
			},
		},
	}
	out, err := newTestEngine().RenderNodePlanSpec(in, ctx)
	if err != nil {
		t.Fatalf("RenderNodePlanSpec: %v", err)
	}
	if got := out.Files[0].Content; got != "https://10.0.0.1:9345" {
		t.Errorf("file content = %q, want join URL", got)
	}
	if got := out.Instructions[0].Args[2]; got != "https://10.0.0.1:9345" {
		t.Errorf("arg[2] = %q, want join URL", got)
	}
	// Source spec is not mutated.
	if in.Files[0].Content == out.Files[0].Content {
		t.Errorf("input was mutated")
	}
}

func TestRenderNodePlanSpecShardDeterministic(t *testing.T) {
	// Two control-plane nodes published join URLs; two consumers should
	// each pick a stable shard, possibly the same one but never random.
	in := v1alpha1.NodePlanSpec{
		Files: []v1alpha1.File{{
			Path:    "/etc/rke2/server",
			Content: `{{ shard "rke.cattle.io/control-plane-role=true" "join-url" }}`,
		}},
	}
	outputs := map[string]NodeOutputs{
		"cp-1": {
			Labels: map[string]string{"rke.cattle.io/control-plane-role": "true"},
			Values: map[string]string{"join-url": "https://cp-1:9345"},
		},
		"cp-2": {
			Labels: map[string]string{"rke.cattle.io/control-plane-role": "true"},
			Values: map[string]string{"join-url": "https://cp-2:9345"},
		},
	}
	mk := func(node string) NodePlanContext {
		return NodePlanContext{
			Cluster:     ClusterReference{Type: "capr", Namespace: "fleet-default", Name: "foo"},
			ElectedNode: node,
			Outputs:     outputs,
		}
	}
	e := newTestEngine()
	first, err := e.RenderNodePlanSpec(in, mk("worker-1"))
	if err != nil {
		t.Fatalf("RenderNodePlanSpec: %v", err)
	}
	second, err := e.RenderNodePlanSpec(in, mk("worker-1"))
	if err != nil {
		t.Fatalf("RenderNodePlanSpec: %v", err)
	}
	if first.Files[0].Content != second.Files[0].Content {
		t.Errorf("shard not deterministic: %q vs %q",
			first.Files[0].Content, second.Files[0].Content)
	}
	if !strings.HasPrefix(first.Files[0].Content, "https://cp-") {
		t.Errorf("shard didn't pick a CP value: %q", first.Files[0].Content)
	}
}

func TestRenderCriterionTrueAndFalse(t *testing.T) {
	// A simple criterion that evaluates against the candidate node's labels.
	e := newTestEngine()
	node := &unstructured.Unstructured{}
	node.SetName("etcd-1")
	node.SetLabels(map[string]string{"rke.cattle.io/etcd-role": "true"})
	ctx := ElectionContext{
		Cluster:     ClusterReference{Type: "capr"},
		ClusterType: "capr",
		Node:        node,
	}
	got, err := e.RenderCriterion(`{{ hasRole .Node "etcd" }}`, ctx)
	if err != nil {
		t.Fatalf("RenderCriterion: %v", err)
	}
	if !got {
		t.Errorf("expected true criterion")
	}
	got, err = e.RenderCriterion(`{{ hasRole .Node "controlplane" }}`, ctx)
	if err != nil {
		t.Fatalf("RenderCriterion: %v", err)
	}
	if got {
		t.Errorf("expected false criterion")
	}
}

func TestSemverGTE(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.31.1", "v1.26.0", true},
		{"v1.26.0", "v1.31.1", false},
		{"v1.26.0+rke2r1", "v1.26.0", true},
		{"1.26.0", "v1.26.0", true},
	}
	for _, c := range cases {
		got, err := semverGTEFunc(c.a, c.b)
		if err != nil {
			t.Errorf("semverGTE(%q, %q): %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("semverGTE(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

