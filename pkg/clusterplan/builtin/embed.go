// Package builtin embeds the ClusterPlanTemplate YAMLs Rancher ships
// out-of-the-box and exposes them as parsed v1alpha1 objects. The
// builtin controller (registered in pkg/controllers/plan/builtin/, not
// in this package) is responsible for ensuring these objects exist on
// the management cluster at startup; this package supplies the source
// of truth.
//
// Per-operation, per-cluster-type naming convention:
//
//	<operation>-<cluster-type>
//
// e.g. etcd-snapshot-create-capr, etcd-snapshot-create-caprke2,
// etcd-snapshot-create-imported. The constraint is deliberate — the
// reconciler that creates a ClusterPlan from a per-op trigger CR
// (e.g. ETCDSnapshotCreate) computes the template name as
// `<op>-<clusterType>` and looks it up directly. This keeps all
// cluster-type-specific logic confined to the YAML template body; the
// downstream ClusterPlan / NodePlanTemplate / NodePlan never branches
// on cluster type.
package builtin

import (
	"embed"
	"fmt"
	"strings"

	v1alpha1 "github.com/rancher/rancher/pkg/apis/plan.cattle.io/v1alpha1"
	"sigs.k8s.io/yaml"
)

//go:embed *.yaml
var templatesFS embed.FS

// TemplateNameFor returns the canonical built-in template name for an
// (operation, clusterType) pair. Cluster types are the
// v1alpha1.ClusterType{CAPR,CAPRKE2,Imported} constants.
func TemplateNameFor(operation, clusterType string) string {
	return operation + "-" + clusterType
}

// ClusterPlanTemplates loads every embedded YAML, parses it as a
// v1alpha1.ClusterPlanTemplate, and returns the result. Templates are
// returned in lexicographic filename order so callers that materialise
// them get a deterministic apply ordering.
func ClusterPlanTemplates() ([]*v1alpha1.ClusterPlanTemplate, error) {
	entries, err := templatesFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("builtin: read embed FS: %w", err)
	}
	var out []*v1alpha1.ClusterPlanTemplate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := templatesFS.ReadFile(e.Name())
		if err != nil {
			return nil, fmt.Errorf("builtin: read %s: %w", e.Name(), err)
		}
		t := &v1alpha1.ClusterPlanTemplate{}
		if err := yaml.UnmarshalStrict(data, t); err != nil {
			return nil, fmt.Errorf("builtin: unmarshal %s: %w", e.Name(), err)
		}
		out = append(out, t)
	}
	return out, nil
}

// ClusterPlanTemplateByName returns the embedded template with the
// supplied metadata.name, or an error if no such template exists.
func ClusterPlanTemplateByName(name string) (*v1alpha1.ClusterPlanTemplate, error) {
	all, err := ClusterPlanTemplates()
	if err != nil {
		return nil, err
	}
	for _, t := range all {
		if t.Name == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("builtin: no template named %q", name)
}
