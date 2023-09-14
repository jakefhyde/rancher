package main

var templateSource = `
package {{.PackageVersion}}

import (
	"github.com/rancher/lasso/pkg/controller"
	{{.PackageVersion}} "github.com/rancher/rancher/pkg/apis/{{.PackageSource}}/{{.PackageVersion}}"
	controllers "github.com/rancher/rancher/pkg/generated/controllers/{{.PackageSource}}/{{.PackageVersion}}"
	stevev1 "github.com/rancher/rancher/tests/framework/clients/rancher/v1"
	"github.com/rancher/rancher/tests/framework/pkg/steve/generic"
	"github.com/rancher/wrangler/pkg/schemes"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() {
	schemes.Register({{.PackageVersion}}.AddToScheme)
}
{{range $_, $name := .Names}}
type {{$name}}Controller interface {
	controllers.{{$name}}Controller
}
{{end}}
type Interface interface { {{range $_, $name := .Names}}
	{{$name}}() {{$name}}Controller{{end}}
}

func New(controllerFactory controller.SharedControllerFactory, client *stevev1.Client) Interface {
	return &version{
		controllerFactory: controllerFactory,
		client:            client,
	}
}

type version struct {
	controllerFactory controller.SharedControllerFactory
	client            *stevev1.Client
}

{{range $_, $name := .Names}}
func (v *version) {{$name}}() {{$name}}Controller {
	return generic.NewController[*v1.{{$name}}, *v1.{{$name}}List](v.client, schema.GroupVersionKind{Group: "{{$.PackageSource}}", Version: "{{$.PackageVersion}}", Kind: "{{$name}}"}, "{{$name | lower}}s", true, v.controllerFactory)
}
{{end}}
`
