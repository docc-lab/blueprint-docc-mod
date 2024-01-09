package thriftcodegen

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gogen"
	"golang.org/x/exp/slog"
)

// This function is used by the Thrift plugin to generate the server-side Thrift service.
//
// It is assumed that outputPackage is the same as the one where the .thrift is generated to
func GenerateServerHandler(builder golang.ModuleBuilder, service *gocode.ServiceInterface, outputPackage string) error {
	pkg, err := builder.CreatePackage(outputPackage)
	if err != nil {
		return err
	}

	innerPkg := strings.ToLower(service.BaseName)

	server := &serverArgs{
		Package:      pkg,
		Service:      service,
		Name:         service.BaseName + "_ThriftServerHandler",
		Imports:      gogen.NewImports(pkg.Name),
		ImportPrefix: innerPkg,
	}

	innerPkgPath := builder.Info().Name + "/" + outputPackage + "/" + innerPkg

	server.Imports.AddPackages("context", "github.com/apache/thrift/lib/go/thrift", innerPkgPath)

	slog.Info(fmt.Sprintf("Generating %v/%v_ThriftServer.go", server.Package.PackageName, service.Name))
	outputFile := filepath.Join(server.Package.Path, service.Name+
		"_ThriftServer.go")
	return gogen.ExecuteTemplateToFile("ThriftServer", serverTemplate, server, outputFile)
}

// Arguments to the template code
type serverArgs struct {
	Package      golang.PackageInfo
	Service      *gocode.ServiceInterface
	Name         string
	Imports      *gogen.Imports
	ImportPrefix string
}

var serverTemplate = `// Blueprint: Auto-generated by Thrift Plugin

package {{.Package.ShortName}}

{{.Imports}}

type {{.Name}} struct {
	Service {{.Imports.NameOf .Service.UserType}}
	Address string
}

func New_{{.Name}}(ctx context.Context, service {{.Imports.NameOf .Service.UserType}}, serverAddress string) (*{{.Name}}, error) {
	handler := &{{.Name}}{}
	handler.Service = service
	handler.Address = serverAddress
	return handler, nil
}

// Blueprint: Run is automatically called in a separate goroutine by runtime/plugins/golang/di.go
func (handler *{{.Name}}) Run(ctx context.Context) error {
	var protocolFactory thrift.TProtocolFactory
	protocolFactory = thrift.NewTBinaryProtocolFactory(true, true)
	var transportFactory thrift.TTransportFactory
	transportFactory = thrift.NewTTransportFactory()
	var transport thrift.TServerTransport
	var err error
	transport, err = thrift.NewTServerSocket(handler.Address)
	if err != nil {
		return err
	}
	processor := {{.ImportPrefix}}.New{{.Service.BaseName}}Processor(handler)
	server := thrift.NewTSimpleServer4(processor, transport, transportFactory, protocolFactory)

	go func() {
		select {
		case <-ctx.Done():
			server.Stop()
		}
	}()

	return server.Serve()
}

{{$service := .Service.Name -}}
{{$receiver := .Name -}}
{{$prefix := .ImportPrefix -}}
{{ range $_, $f := .Service.Methods }}
func (handler *{{$receiver}}) {{$f.Name -}}(ctx context.Context, req *{{$prefix}}.{{$service}}_{{$f.Name}}_Request) (*{{$prefix}}.{{$service}}_{{$f.Name}}_Response, error) {
	{{ArgVarsEquals $f}} unmarshall_{{$f.Name}}_req(req)
	{{RetVars $f "err"}} := handler.Service.{{$f.Name}}({{ArgVars $f "ctx"}})
	if err != nil {
		return nil, err
	}
	rsp := &{{$prefix}}.{{$service}}_{{$f.Name}}_Response{}
	marshall_{{$f.Name}}_rsp(rsp, {{RetVars $f}})
	return rsp, nil
}
{{end}}
`
