package opentelemetry

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gogen"
	"golang.org/x/tools/imports"
)

// Compile and exercise the actual templates: string matching cannot catch
// reading reverse baggage before SDK preparation or losing it across RPCs.
func TestGeneratedReverseCheckpointWrappers(t *testing.T) {
	dir := t.TempDir()
	runtimeDir, err := filepath.Abs("../../runtime")
	if err != nil {
		t.Fatal(err)
	}
	leafDir, err := filepath.Abs("../../examples/leaf/workflow")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte(fmt.Sprintf(`module checkpointwrappers

go 1.23.6

require (
    github.com/blueprint-uservices/blueprint/runtime v0.0.0
    github.com/blueprint-uservices/blueprint/examples/leaf/workflow v0.0.0
)

replace github.com/blueprint-uservices/blueprint/runtime => %q
replace github.com/blueprint-uservices/blueprint/examples/leaf/workflow => %q
`, runtimeDir, leafDir)))
	fixture, err := os.ReadFile("testdata/reverse_checkpoint_test.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	write("reverse_checkpoint_test.go", fixture)

	pkg := golang.PackageInfo{ShortName: "generated", Name: "generated"}
	app := &gocode.ServiceInterface{
		UserType: gocode.UserType{Name: "App", Package: pkg.Name}, BaseName: "App",
		Methods: map[string]gocode.Func{"Call": {
			Name:      "Call",
			Arguments: []gocode.Variable{{Name: "n", Type: &gocode.BasicType{Name: "int"}}},
			Returns:   []gocode.Variable{{Name: "result", Type: &gocode.BasicType{Name: "int"}}},
		}},
	}
	coll := &gocode.ServiceInterface{UserType: gocode.UserType{Name: "Collector", Package: pkg.Name}}
	newImports := func() *gogen.Imports {
		imp := gogen.NewImports(pkg.Name)
		imp.AddPackages("context", "go.opentelemetry.io/otel/trace", "go.opentelemetry.io/otel/sdk/trace",
			"go.opentelemetry.io/otel/attribute", "github.com/blueprint-uservices/blueprint/runtime/core/backend",
			"strings", "sync", "sync/atomic", "strconv", "encoding/base64", "encoding/binary")
		return imp
	}
	render := func(name, template string, args any) {
		t.Helper()
		code, err := gogen.ExecuteTemplate(name, template, args)
		if err != nil {
			t.Fatal(err)
		}
		// Generated deployments also run goimports to prune template imports.
		formatted, err := imports.Process(filepath.Join(dir, name+".go"), []byte(code), nil)
		if err != nil {
			t.Fatalf("format %s: %v", name, err)
		}
		write(name+".go", formatted)
	}
	var factories strings.Builder
	factories.WriteString("package generated\nimport \"context\"\nvar wrapperVariants = []wrapperVariant{\n")
	for _, variant := range []struct{ name, server, client string }{
		{"PB", serverTemplatePath, clientSideTemplatePath},
		{"CGPB", serverTemplateCGPB, clientSideTemplateCGPB},
		{"SB", serverTemplateSBridge, clientSideTemplateSBridge},
		{"Vanilla", serverTemplateVanilla, clientSideTemplateVanilla},
	} {
		wire := gocode.CopyServiceInterface(variant.name+"Wire", pkg.Name, app)
		method := wire.Methods["Call"]
		method.AddArgument(gocode.Variable{Name: "traceCtx", Type: &gocode.BasicType{Name: "string"}})
		method.AddRetVar(gocode.Variable{Name: "retCtx", Type: &gocode.BasicType{Name: "string"}})
		wire.Methods["Call"] = method
		render(variant.name+"Server", variant.server, &serverArgs{
			Package: pkg, Service: app, Impl: wire, CollIface: coll,
			Name: variant.name + "Server", IfaceName: wire.Name, Imports: newImports(),
		})
		render(variant.name+"Client", variant.client, &clientArgs{
			Package: pkg, Service: wire, Impl: app, CollIface: coll,
			Name: variant.name + "Client", IfaceName: variant.name + "ClientInterface",
			ServerIfaceName: wire.Name, Imports: newImports(),
		})
		fmt.Fprintf(&factories, `{name: %q,
server: func(app App, coll Collector) Wire { s, _ := New_%sServer(context.Background(), app, coll); return s },
client: func(wire Wire, coll Collector) App { c, _ := New_%sClient(context.Background(), wire, coll); return c }},
`, variant.name, variant.name, variant.name)
	}
	factories.WriteString("}\n")
	write("variants_test.go", []byte(factories.String()))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-race", "-mod=mod", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated wrappers: %v\n%s", err, output)
	}
}
