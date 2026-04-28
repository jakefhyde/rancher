package render

import (
	"strings"
	"testing"
)

func TestPreprocessPassthrough(t *testing.T) {
	// Bodies with no `{{ if ... }}` containing &|! and no `{{ fi }}` should
	// pass through untouched.
	cases := []string{
		"plain text",
		`{{ if eq .Cluster.Type "capr" }}A{{ end }}`,
		`{{ .Inputs.snapshotName }}`,
		`{{ secret "agent" "token" }}`,
	}
	for _, in := range cases {
		out, err := preprocess(in)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", in, err)
		}
		if out != in {
			t.Errorf("expected passthrough\nin:  %q\nout: %q", in, out)
		}
	}
}

func TestPreprocessRoleSugar(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single role",
			in:   `{{ if etcd }}YES{{ fi }}`,
			want: `{{- if (hasRole .Node "etcd") }}YES{{- end }}`,
		},
		{
			name: "and not",
			in:   `{{ if etcd & !controlplane }}A{{ fi }}`,
			want: `{{- if (and (hasRole .Node "etcd") (not (hasRole .Node "controlplane"))) }}A{{- end }}`,
		},
		{
			name: "or",
			in:   `{{ if etcd | worker }}A{{ fi }}`,
			want: `{{- if (or (hasRole .Node "etcd") (hasRole .Node "worker")) }}A{{- end }}`,
		},
		{
			name: "parens precedence",
			in:   `{{ if (etcd | worker) & !controlplane }}A{{ fi }}`,
			want: `{{- if (and (or (hasRole .Node "etcd") (hasRole .Node "worker")) (not (hasRole .Node "controlplane"))) }}A{{- end }}`,
		},
		{
			name: "multiple sugar blocks survive together",
			in:   `{{ if etcd }}A{{ fi }}{{ if worker }}B{{ fi }}`,
			want: `{{- if (hasRole .Node "etcd") }}A{{- end }}{{- if (hasRole .Node "worker") }}B{{- end }}`,
		},
		{
			name: "case-folded identifiers",
			in:   `{{ if Etcd }}A{{ fi }}`,
			want: `{{- if (hasRole .Node "etcd") }}A{{- end }}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := preprocess(c.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("\n want %q\n got  %q", c.want, got)
			}
		})
	}
}

func TestPreprocessMixedSyntax(t *testing.T) {
	// A body with both stdlib `{{ if eq ... }}` and sugar `{{ if etcd }}`
	// should rewrite only the sugar.
	in := `{{ if eq .ClusterType "capr" }}{{ if etcd }}A{{ fi }}{{ end }}`
	got, err := preprocess(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `{{- if (hasRole .Node "etcd") }}`) {
		t.Errorf("sugar branch missing; got: %q", got)
	}
	if !strings.Contains(got, `{{ if eq .ClusterType "capr" }}`) {
		t.Errorf("stdlib if was rewritten unexpectedly; got: %q", got)
	}
	if !strings.Contains(got, `{{- end }}`) {
		t.Errorf("`{{ fi }}` not rewritten; got: %q", got)
	}
	if !strings.Contains(got, "{{ end }}") {
		t.Errorf("stdlib `{{ end }}` should remain; got: %q", got)
	}
}

func TestPreprocessRejectsInvalidDSL(t *testing.T) {
	cases := []string{
		`{{ if etcd & }}A{{ fi }}`,    // trailing operator
		`{{ if & etcd }}A{{ fi }}`,    // leading operator
		`{{ if etcd | | }}A{{ fi }}`,  // double operator
		`{{ if (etcd }}A{{ fi }}`,     // unbalanced paren
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := preprocess(in); err == nil {
				t.Errorf("expected error for %q", in)
			}
		})
	}
}

func TestLooksLikeRoleDSL(t *testing.T) {
	cases := map[string]bool{
		"etcd & !controlplane":   true,
		"etcd | worker":          true,
		"!etcd":                  true,
		"(a | b) & c":            true,
		"etcd":                   true, // single ident is still DSL
		"true":                   true, // also DSL — would fail as stdlib too
		`eq .ClusterType "capr"`: false, // contains quotes/dot
		`"etcd" | "worker"`:      false, // contains quotes
	}
	for in, want := range cases {
		got := looksLikeRoleDSL(in)
		if got != want {
			t.Errorf("looksLikeRoleDSL(%q) = %v, want %v", in, got, want)
		}
	}
}
