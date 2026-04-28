package builtin

import (
	"testing"
	"time"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	rkev1 "github.com/rancher/rancher/pkg/apis/rke.cattle.io/v1"
	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	"github.com/rancher/rancher/pkg/clusterplan/render"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
)

func fixedNow() time.Time { return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC) }

func TestTemplatesParse(t *testing.T) {
	tmpls, err := ClusterPlanTemplates()
	if err != nil {
		t.Fatalf("ClusterPlanTemplates: %v", err)
	}
	want := map[string]bool{
		"etcd-snapshot-create-capr":     false,
		"etcd-snapshot-create-caprke2":  false,
		"etcd-snapshot-create-imported": false,
	}
	for _, tmpl := range tmpls {
		if _, ok := want[tmpl.Name]; ok {
			want[tmpl.Name] = true
		}
		if tmpl.Spec.Operation != "etcd-snapshot-create" {
			t.Errorf("template %q has Operation=%q, want etcd-snapshot-create",
				tmpl.Name, tmpl.Spec.Operation)
		}
		if len(tmpl.Spec.Entrypoints) != 1 {
			t.Errorf("template %q has %d entrypoints, want 1",
				tmpl.Name, len(tmpl.Spec.Entrypoints))
		}
		if tmpl.Labels[v1alpha1.BuiltinLabel] != "true" {
			t.Errorf("template %q missing %s=true label",
				tmpl.Name, v1alpha1.BuiltinLabel)
		}
		if tmpl.Spec.Plan == "" {
			t.Errorf("template %q has empty plan body", tmpl.Name)
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("expected built-in template %q not found", n)
		}
	}
}

func TestTemplateNameForConvention(t *testing.T) {
	cases := []struct {
		op, ct, want string
	}{
		{"etcd-snapshot-create", "capr", "etcd-snapshot-create-capr"},
		{"etcd-snapshot-create", "caprke2", "etcd-snapshot-create-caprke2"},
		{"etcd-snapshot-create", "imported", "etcd-snapshot-create-imported"},
	}
	for _, c := range cases {
		if got := TemplateNameFor(c.op, c.ct); got != c.want {
			t.Errorf("TemplateNameFor(%q,%q) = %q, want %q", c.op, c.ct, got, c.want)
		}
	}
}

func TestTemplateByName(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		tmpl, err := ClusterPlanTemplateByName("etcd-snapshot-create-capr")
		if err != nil {
			t.Fatalf("ClusterPlanTemplateByName: %v", err)
		}
		if tmpl.Name != "etcd-snapshot-create-capr" {
			t.Errorf("name = %q", tmpl.Name)
		}
	})
	t.Run("missing", func(t *testing.T) {
		if _, err := ClusterPlanTemplateByName("nonexistent"); err == nil {
			t.Errorf("expected error for unknown template")
		}
	})
}

// TestRenderRoundtripPerClusterType exercises the canonical
// "template renders cleanly to ClusterPlanSpec" path for each of the
// three built-in templates, with the matching entrypoint object as
// .Entrypoint context. It's the single most valuable per-template test
// because it catches both YAML errors and template-func mistakes
// (runtimeCommand on a wrong type, missing inputs, etc.).
func TestRenderRoundtripPerClusterType(t *testing.T) {
	cases := []struct {
		name        string
		entrypoint  runtime.Object
		wantCommand string
	}{
		{
			name: "etcd-snapshot-create-capr",
			entrypoint: &rkev1.RKEControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "fleet-default"},
				Spec:       rkev1.RKEControlPlaneSpec{KubernetesVersion: "v1.31.1+rke2r1"},
			},
			wantCommand: "rke2",
		},
		{
			name:        "etcd-snapshot-create-caprke2",
			entrypoint:  nil, // template hardcodes "rke2"; entrypoint not consulted
			wantCommand: "rke2",
		},
		{
			name: "etcd-snapshot-create-imported",
			entrypoint: &apimgmtv3.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "c-m-12345"},
				Status: apimgmtv3.ClusterStatus{
					Driver:  apimgmtv3.ClusterDriverRke2,
					Version: &version.Info{GitVersion: "v1.31.1+rke2r1"},
				},
			},
			wantCommand: "rke2",
		},
	}
	engine := render.New(&render.FakeClient{}, fixedNow)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := ClusterPlanTemplateByName(c.name)
			if err != nil {
				t.Fatalf("ClusterPlanTemplateByName: %v", err)
			}
			ctx := render.ClusterPlanContext{
				Cluster:    render.ClusterReference{Type: "test", Namespace: "fleet-default", Name: "foo"},
				Entrypoint: c.entrypoint,
				Inputs:     map[string]string{"snapshotName": "snap-001"},
				Timestamp:  fixedNow().UTC().Format(time.RFC3339),
			}
			spec, err := engine.RenderClusterPlan(tmpl.Spec.Plan, ctx)
			if err != nil {
				t.Fatalf("RenderClusterPlan: %v\nbody:\n%s", err, tmpl.Spec.Plan)
			}
			if len(spec.Plan) == 0 {
				t.Fatalf("rendered spec has no plan pools: %+v", spec)
			}
			pool := spec.Plan[0]
			if pool.Name != "snapshot" {
				t.Errorf("pool[0].Name = %q, want snapshot", pool.Name)
			}
			if len(pool.Instructions) == 0 {
				t.Fatalf("pool has no instructions")
			}
			if got := pool.Instructions[0].Command; got != c.wantCommand {
				t.Errorf("command = %q, want %q", got, c.wantCommand)
			}
			// snapshotName input should have been passed through.
			args := pool.Instructions[0].Args
			found := false
			for i, a := range args {
				if a == "--name" && i+1 < len(args) && args[i+1] == "snap-001" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected --name snap-001 in args, got %v", args)
			}
		})
	}
}
