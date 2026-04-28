package builtin

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

// recordingApplier captures every ApplyObjects invocation for assertion.
type recordingApplier struct {
	calls   int
	lastSet string
	lastObj []runtime.Object
	err     error
}

func (r *recordingApplier) ApplyObjects(setID string, objects ...runtime.Object) error {
	r.calls++
	r.lastSet = setID
	r.lastObj = objects
	return r.err
}

func TestApplyBuiltinsIncludesAllEmbeddedTemplates(t *testing.T) {
	a := &recordingApplier{}
	h := New(a)
	if err := h.ApplyBuiltins(context.Background()); err != nil {
		t.Fatalf("ApplyBuiltins: %v", err)
	}
	if a.calls != 1 {
		t.Errorf("expected 1 apply call, got %d", a.calls)
	}
	if a.lastSet != SetID {
		t.Errorf("setID = %q, want %q", a.lastSet, SetID)
	}
	want := map[string]bool{
		"etcd-snapshot-create-capr":     false,
		"etcd-snapshot-create-caprke2":  false,
		"etcd-snapshot-create-imported": false,
	}
	for _, o := range a.lastObj {
		tmpl, ok := o.(*v1alpha1.ClusterPlanTemplate)
		if !ok {
			t.Errorf("apply set contained a non-ClusterPlanTemplate object: %T", o)
			continue
		}
		want[tmpl.Name] = true
		if tmpl.Labels[v1alpha1.BuiltinLabel] != "true" {
			t.Errorf("template %q is missing the builtin label: %v", tmpl.Name, tmpl.Labels)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected built-in template %q in apply set", name)
		}
	}
}

func TestApplyBuiltinsPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	a := &recordingApplier{err: wantErr}
	h := New(a)
	err := h.ApplyBuiltins(context.Background())
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

func TestRegisterCallsApply(t *testing.T) {
	a := &recordingApplier{}
	h := New(a)
	if err := h.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if a.calls != 1 {
		t.Errorf("Register did not invoke apply")
	}
}

func TestTemplatesByNameHelper(t *testing.T) {
	in := []*v1alpha1.ClusterPlanTemplate{{}, {}}
	in[0].Name = "a"
	in[1].Name = "b"
	got := templatesByName(in)
	if got["a"] != in[0] || got["b"] != in[1] {
		t.Errorf("templatesByName returned wrong map: %v", got)
	}
}
