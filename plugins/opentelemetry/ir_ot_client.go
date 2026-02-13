package opentelemetry

import (
	"fmt"
	"path/filepath"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/blueprint"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gogen"
	"golang.org/x/exp/slog"
)

// Blueprint IR Node that wraps the client-side of a service to generate ot compatible logs
type OpenTelemetryClientWrapper struct {
	golang.Service
	golang.GeneratesFuncs

	WrapperName   string
	outputPackage string
	Wrapped       golang.Service
	Collector     OpenTelemetryCollectorInterface
}

func newOpenTelemetryClientWrapper(name string, server golang.Service, collector OpenTelemetryCollectorInterface) (*OpenTelemetryClientWrapper, error) {
	node := &OpenTelemetryClientWrapper{}
	node.WrapperName = name
	node.Wrapped = server
	node.Collector = collector
	node.outputPackage = "ot"
	return node, nil
}

func (node *OpenTelemetryClientWrapper) Name() string {
	return node.WrapperName
}

func (node *OpenTelemetryClientWrapper) String() string {
	return node.Name() + " = OTClientWrapper(" + node.Wrapped.Name() + ", " + node.Collector.Name() + ")"
}

func (node *OpenTelemetryClientWrapper) genInterface(ctx ir.BuildContext) (*gocode.ServiceInterface, error) {
	iface, err := golang.GetGoInterface(ctx, node.Wrapped)
	if err != nil {
		return nil, err
	}
	module_ctx, valid := ctx.(golang.ModuleBuilder)
	if !valid {
		return nil, blueprint.Errorf("OTClientWrapper expected build context to be a ModuleBuiler, got %v", ctx)
	}
	i := gocode.CopyServiceInterface(fmt.Sprintf("%v_OTClientWrapperInterface", iface.BaseName), module_ctx.Info().Name+"/"+node.outputPackage, iface)
	for name, method := range i.Methods {
		method.Arguments = method.Arguments[:len(method.Arguments)-1]
		i.Methods[name] = method
	}
	return i, nil
}

func (node *OpenTelemetryClientWrapper) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.genInterface(ctx)
}

// Part of code generation compilation pass; creates the interface definition code for the wrapper,
// and any new generated structs that are exposed and can be used by other IRNodes
func (node *OpenTelemetryClientWrapper) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Wrapped.AddInterfaces(builder)
}

// Part of code generation compilation pass; provides implementation of interfaces from GenerateInterfaces
func (node *OpenTelemetryClientWrapper) GenerateFuncs(builder golang.ModuleBuilder) error {
	builder.Require("go.opentelemetry.io/otel/trace", "v1.26.0")
	wrapped_iface, err := golang.GetGoInterface(builder, node.Wrapped)
	if err != nil {
		return err
	}

	coll_iface, err := golang.GetGoInterface(builder, node.Collector)
	if err != nil {
		return err
	}

	impl_iface, err := node.genInterface(builder)
	if err != nil {
		return err
	}

	// Only generate code once
	if builder.Visited(impl_iface.Name + ".ot_client_impl") {
		return nil
	}

	return generateClientHandler(builder, wrapped_iface, impl_iface, coll_iface, node.outputPackage)
}

// Part of code generation compilation pass; provides instantiation snippet
func (node *OpenTelemetryClientWrapper) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.WrapperName) {
		return nil
	}

	iface, err := golang.GetGoInterface(builder, node.Wrapped)
	if err != nil {
		return err
	}

	coll_iface, err := golang.GetGoInterface(builder, node.Collector)
	if err != nil {
		return err
	}

	constructor := &gocode.Constructor{
		Package: builder.Module().Info().Name + "/" + node.outputPackage,
		Func: gocode.Func{
			Name: fmt.Sprintf("New_%v_OTClientWrapper", iface.BaseName),
			Arguments: []gocode.Variable{
				{Name: "ctx", Type: &gocode.UserType{Package: "context", Name: "Context"}},
				{Name: "client", Type: iface},
				{Name: "coll_client", Type: coll_iface},
			},
		},
	}

	return builder.DeclareConstructor(node.WrapperName, constructor, []ir.IRNode{node.Wrapped, node.Collector})
}

func (node *OpenTelemetryClientWrapper) ImplementsGolangNode()    {}
func (node *OpenTelemetryClientWrapper) ImplementsGolangService() {}

func generateClientHandler(builder golang.ModuleBuilder, wrapped *gocode.ServiceInterface, impl *gocode.ServiceInterface, coll_iface *gocode.ServiceInterface, outputPackage string) error {
	pkg, err := builder.CreatePackage(outputPackage)
	if err != nil {
		return err
	}

	server := &clientArgs{
		Package:         pkg,
		Service:         wrapped,
		Impl:            impl,
		CollIface:       coll_iface,
		Name:            wrapped.BaseName + "_OTClientWrapper",
		IfaceName:       impl.Name,
		ServerIfaceName: wrapped.BaseName + "_OTServerWrapperInterface",
		Imports:         gogen.NewImports(pkg.Name),
	}

	server.Imports.AddPackages("context")
	server.Imports.AddPackages("go.opentelemetry.io/otel/trace")
	server.Imports.AddPackages("go.opentelemetry.io/otel/sdk/trace")
	server.Imports.AddPackages("go.opentelemetry.io/otel/attribute")
	server.Imports.AddPackages("github.com/blueprint-uservices/blueprint/runtime/core/backend")
	// server.Imports.AddPackages("github.com/blueprint-uservices/blueprint/runtime/plugins/critpath")
	server.Imports.AddPackages("strings")
	// server.Imports.AddPackages("sync")
	server.Imports.AddPackages("sync/atomic")
	server.Imports.AddPackages("strconv")
	// server.Imports.AddPackages("time")

	slog.Info(fmt.Sprintf("Generating %v/%v", server.Package.PackageName, impl.Name))
	outputFile := filepath.Join(server.Package.Path, impl.Name+".go")
	// return gogen.ExecuteTemplateToFile("OTClientWrapper", clientSideTemplate, server, outputFile)
	// return gogen.ExecuteTemplateToFile("OTClientWrapper", clientSideTemplateSBridge, server, outputFile)
	// return gogen.ExecuteTemplateToFile("OTClientWrapper", clientSideTemplateCGPB, server, outputFile)
	return gogen.ExecuteTemplateToFile("OTClientWrapper", clientSideTemplatePath, server, outputFile)
	// return gogen.ExecuteTemplateToFile("OTClientWrapper", clientSideTemplateVanilla, server, outputFile)
}

type clientArgs struct {
	Package         golang.PackageInfo
	Service         *gocode.ServiceInterface
	Impl            *gocode.ServiceInterface
	CollIface       *gocode.ServiceInterface
	Name            string
	IfaceName       string
	ServerIfaceName string
	Imports         *gogen.Imports
}

// var clientSideTemplate = `// Blueprint: Auto-generated by OT Plugin
// package {{.Package.ShortName}}
//
// {{.Imports}}
//
// type {{.IfaceName}} interface {
// 	{{range $_, $f := .Impl.Methods -}}
// 	{{Signature $f}}
// 	{{end}}
// }
//
// type {{.Name}} struct {
// 	Client {{.ServerIfaceName}}
// 	CollClient {{.Imports.NameOf .CollIface.UserType}}
// }
//
// func New_{{.Name}}(ctx context.Context, client {{.ServerIfaceName}}, coll_client {{.Imports.NameOf .CollIface.UserType}}) (*{{.Name}}, error) {
// 	handler := &{{.Name}}{}
// 	handler.Client = client
// 	handler.CollClient = coll_client
// 	return handler, nil
// }
//
// {{$service := .Service.Name -}}
// {{$basename := .Service.BaseName -}}
// {{$receiver := .Name -}}
// {{$sdktrace := "trace2" -}}
// {{range $_, $f := .Impl.Methods}}
// func (handler *{{$receiver}}) {{$f.Name -}} ({{ArgVarsAndTypes $f "ctx context.Context"}}) ({{RetVarsAndTypes $f "err error"}}) {
// 	// Get baggage from context and create a copy to avoid mutating shared state
// 	upstreamBaggage := backend.GetBaggageFromContext(ctx)
// 	baggage := make(map[string]string)
// 	if upstreamBaggage != nil {
// 		for k, v := range upstreamBaggage {
// 			baggage[k] = v
// 		}
// 	}
//
// 	// Server always sets these values, so we can skip ok checks to reduce overhead
// 	// Cache pointers after first lookup to avoid repeated context.Value() calls
// 	childCountPtr := ctx.Value("childCount").(*atomic.Uint64)
// 	// Enable lines below for S-Bridge
// 	// ccMutexPtr := ctx.Value("ccMutex").(*sync.Mutex)
// 	// endEventsPtr := ctx.Value("endEvents").(*string)
// 	// // endEventsPtr := ctx.Value("endEvents").(*[]string) // Old
//
// 	// seqNum := int(childCountPtr.Add(1))
// 	// ctx = context.WithValue(ctx, "seqNum", seqNum)
// 	ctx = context.WithValue(ctx, "seqNum", int(childCountPtr.Add(1)))
// 	// Enable lines below for S-Bridge
// 	// ccMutexPtr.Lock()
// 	// // endEvents := slices.Clone(*endEventsPtr) // Old
// 	// endEvents := *endEventsPtr
// 	// *endEventsPtr = ""
// 	// ccMutexPtr.Unlock()
//
// 	// Enable line below for S-Bridge
// 	// ctx = context.WithValue(ctx, "endEvents", endEvents)
// 	
// 	tp, _ := handler.CollClient.GetTracerProvider(ctx)
// 	tr := tp.Tracer("{{$service}}")
// 	// ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))
// 	ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))
// 	defer span.End()
// 	
// 	// Extract baggage from span attributes by casting to ReadWriteSpan
// 	if rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {
// 		for _, attr := range rwSpan.Attributes() {
// 			if strings.HasPrefix(string(attr.Key), "__bag.") {
// 				key := strings.TrimPrefix(string(attr.Key), "__bag.")
// 				// Convert value to string for baggage based on attribute type
// 				switch attr.Value.Type() {
// 				case attribute.INT64:
// 					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
// 				case attribute.STRING:
// 					baggage[key] = attr.Value.AsString()
// 				default:
// 					baggage[key] = attr.Value.AsString()
// 				}
// 			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
// 				key := strings.TrimPrefix(string(attr.Key), "__bagdel.")
// 				delete(baggage, key)
// 			}
// 		}
// 	}
// 	
// 	// Combine trace context with baggage
// 	trace_ctx, _ := span.SpanContext().MarshalJSON()
// 	trace_ctx_with_baggage, _ := backend.AddBaggageToTraceContext(string(trace_ctx), baggage)
// 	
// 	{{RetVars $f "err"}} = handler.Client.{{$f.Name}}({{ArgVars $f "ctx"}}, trace_ctx_with_baggage)
// 	if err != nil {
// 		span.RecordError(err)
// 	}
//
// 	// Reuse cached pointers - no need to look up from context again
// 	// Enable lines below for S-Bridge
// 	// // childCountAfter := int(childCountPtr.Add(1))
// 	// // event := "," + span.SpanContext().SpanID().String() + ":" + strconv.Itoa(childCountAfter)
// 	// event := "," + span.SpanContext().SpanID().String() + ":" + strconv.Itoa(int(childCountPtr.Add(1)))
//
// 	// ccMutexPtr.Lock()
// 	// // *endEventsPtr = append(*endEventsPtr, span.SpanContext().SpanID().String() + ":" + strconv.Itoa(*childCountPtr)) // Old
// 	// // *endEventsPtr = *endEventsPtr + "," + span.SpanContext().SpanID().String() + ":" + strconv.Itoa(childCountAfter) // Old
// 	// *endEventsPtr = *endEventsPtr + event
// 	// ccMutexPtr.Unlock()
// 	
// 	return
// }
// {{end}}
// `

var clientSideTemplate = `// Blueprint: Auto-generated by OT Plugin
package {{.Package.ShortName}}

{{.Imports}}

type {{.IfaceName}} interface {
	{{range $_, $f := .Impl.Methods -}}
	{{Signature $f}}
	{{end}}
}

type {{.Name}} struct {
	Client {{.ServerIfaceName}}
	CollClient {{.Imports.NameOf .CollIface.UserType}}
}

func New_{{.Name}}(ctx context.Context, client {{.ServerIfaceName}}, coll_client {{.Imports.NameOf .CollIface.UserType}}) (*{{.Name}}, error) {
	handler := &{{.Name}}{}
	handler.Client = client
	handler.CollClient = coll_client
	return handler, nil
}

{{$service := .Service.Name -}}
{{$basename := .Service.BaseName -}}
{{$receiver := .Name -}}
{{$sdktrace := "trace2" -}}
{{range $_, $f := .Impl.Methods}}
func (handler *{{$receiver}}) {{$f.Name -}} ({{ArgVarsAndTypes $f "ctx context.Context"}}) ({{RetVarsAndTypes $f "err error"}}) {
	// Get baggage from context and create a copy to avoid mutating shared state
	upstreamBaggage := backend.GetBaggageFromContext(ctx)
	baggage := make(map[string]string)
	if upstreamBaggage != nil {
		for k, v := range upstreamBaggage {
			baggage[k] = v
		}
	}

	// Server always sets these values, so we can skip ok checks to reduce overhead
	// Cache pointers after first lookup to avoid repeated context.Value() calls
	childCountPtr := ctx.Value("childCount").(*atomic.Uint64)
	openChildCountPtr := ctx.Value("openChildCount").(*atomic.Uint64)
	childrenMutexPtr := ctx.Value("childrenMutex").(*sync.Mutex)

	ctx = context.WithValue(ctx, "seqNum", int(childCountPtr.Add(1)))
	
	tp, _ := handler.CollClient.GetTracerProvider(ctx)
	tr := tp.Tracer("{{$service}}")


	// Increment open child count before starting the span
	criticalPathList := ctx.Value("criticalPathList").(*string)
	childrenTracker := ctx.Value("childrenTracker").(*map[string]critpath.StartEnd)

	openChildCountPtr.Add(1)
	childrenMutexPtr.Lock()
	ctx = context.WithValue(ctx, "critPathIds", *criticalPathList)
	*criticalPathList = ""
	
	ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))
	(*childrenTracker)[span.SpanContext().SpanID().String()] = critpath.StartEnd{
		Start: uint64(time.Now().UnixNano()),
		End:   uint64(time.Now().UnixNano()),
	}
	childrenMutexPtr.Unlock()

	defer span.End()
	
	// Extract baggage from span attributes by casting to ReadWriteSpan
	if rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {
		for _, attr := range rwSpan.Attributes() {
			if strings.HasPrefix(string(attr.Key), "__bag.") {
				key := strings.TrimPrefix(string(attr.Key), "__bag.")
				// Convert value to string for baggage based on attribute type
				switch attr.Value.Type() {
				case attribute.INT64:
					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
				case attribute.STRING:
					baggage[key] = attr.Value.AsString()
				default:
					baggage[key] = attr.Value.AsString()
				}
			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
				key := strings.TrimPrefix(string(attr.Key), "__bagdel.")
				delete(baggage, key)
			}
		}
	}
	
	// Combine trace context with baggage
	trace_ctx, _ := span.SpanContext().MarshalJSON()
	trace_ctx_with_baggage, _ := backend.AddBaggageToTraceContext(string(trace_ctx), baggage)
	
	{{RetVars $f "err"}} = handler.Client.{{$f.Name}}({{ArgVars $f "ctx"}}, trace_ctx_with_baggage)
	if err != nil {
		span.RecordError(err)
	}

	// Decrement open child count after the call completes
	// If counter goes to 0, this was the last child - compute critical path
	childrenMutexPtr.Lock()
	spanID := span.SpanContext().SpanID().String()
	entry := (*childrenTracker)[spanID]
	entry.End = uint64(time.Now().UnixNano())
	(*childrenTracker)[spanID] = entry
	if openChildCountPtr.Add(^uint64(0)) == 0 {
		*criticalPathList = strings.Join(critpath.ComputeCriticalPath(*childrenTracker), ",")
		*childrenTracker = make(map[string]critpath.StartEnd)
	}
	childrenMutexPtr.Unlock()
	
	return
}
{{end}}
`

var clientSideTemplateSBridge = `// Blueprint: Auto-generated by OpenTelemetry Plugin
package {{.Package.ShortName}}

{{.Imports}}

type {{.IfaceName}} interface {
	{{range $_, $f := .Impl.Methods -}}
	{{Signature $f}}
	{{end}}
}

type {{.Name}} struct {
	Client {{.ServerIfaceName}}
	CollClient {{.Imports.NameOf .CollIface.UserType}}
}

func New_{{.Name}}(ctx context.Context, client {{.ServerIfaceName}}, coll_client {{.Imports.NameOf .CollIface.UserType}}) (*{{.Name}}, error) {
	handler := &{{.Name}}{}
	handler.Client = client
	handler.CollClient = coll_client
	return handler, nil
}

{{$service := .Service.Name -}}
{{$basename := .Service.BaseName -}}
{{$receiver := .Name -}}
{{$sdktrace := "trace2" -}}
{{range $_, $f := .Impl.Methods}}
func (handler *{{$receiver}}) {{$f.Name -}} ({{ArgVarsAndTypes $f "ctx context.Context"}}) ({{RetVarsAndTypes $f "err error"}}) {
	// Get baggage from context and create a copy to avoid mutating shared state
	upstreamBaggage := backend.GetBaggageFromContext(ctx)
	baggage := make(map[string]string)
	if upstreamBaggage != nil {
		for k, v := range upstreamBaggage {
			baggage[k] = v
		}
	}

	// Server always sets these values, so we can skip ok checks to reduce overhead
	// Cache pointers after first lookup to avoid repeated context.Value() calls
	eventCountPtr := ctx.Value("eventCount").(*atomic.Uint64)
	endEventsPtr := ctx.Value("endEvents").(*string)
	childrenMutexPtr := ctx.Value("childrenMutex").(*sync.Mutex)
	seqNum := int(eventCountPtr.Add(1))

	ctx = context.WithValue(ctx, "seqNum", seqNum)
	childrenMutexPtr.Lock()
	curEndEvents := *endEventsPtr
	*endEventsPtr = ""
	childrenMutexPtr.Unlock()

	ctx = context.WithValue(ctx, "curEndEvents", curEndEvents)
	
	tp, _ := handler.CollClient.GetTracerProvider(ctx)
	tr := tp.Tracer("{{$service}}")

	// childrenMutexPtr.Lock()
	
	ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))

	// childrenMutexPtr.Unlock()

	defer span.End()
	
	// Extract baggage from span attributes by casting to ReadWriteSpan
	if rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {
		for _, attr := range rwSpan.Attributes() {
			if strings.HasPrefix(string(attr.Key), "__bag.") {
				key := strings.TrimPrefix(string(attr.Key), "__bag.")
				// Convert value to string for baggage based on attribute type
				switch attr.Value.Type() {
				case attribute.INT64:
					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
				case attribute.STRING:
					baggage[key] = attr.Value.AsString()
				default:
					baggage[key] = attr.Value.AsString()
				}
			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
				key := strings.TrimPrefix(string(attr.Key), "__bagdel.")
				delete(baggage, key)
			}
		}
	}
	
	// Combine trace context with baggage
	trace_ctx, _ := span.SpanContext().MarshalJSON()
	trace_ctx_with_baggage, _ := backend.AddBaggageToTraceContext(string(trace_ctx), baggage)
	
	{{RetVars $f "err"}} = handler.Client.{{$f.Name}}({{ArgVars $f "ctx"}}, trace_ctx_with_baggage)
	if err != nil {
		span.RecordError(err)
	}

	endSeqNum := int(eventCountPtr.Add(1))
	toAppend := "," + strconv.Itoa(seqNum) + ":" + strconv.Itoa(endSeqNum)
	childrenMutexPtr.Lock()
	*endEventsPtr = *endEventsPtr + toAppend
	childrenMutexPtr.Unlock()
	
	return
}
{{end}}
`

var clientSideTemplateVanilla = `// Blueprint: Auto-generated by OpenTelemetry Plugin
package {{.Package.ShortName}}

{{.Imports}}

type {{.IfaceName}} interface {
	{{range $_, $f := .Impl.Methods -}}
	{{Signature $f}}
	{{end}}
}

type {{.Name}} struct {
	Client {{.ServerIfaceName}}
	CollClient {{.Imports.NameOf .CollIface.UserType}}
}

func New_{{.Name}}(ctx context.Context, client {{.ServerIfaceName}}, coll_client {{.Imports.NameOf .CollIface.UserType}}) (*{{.Name}}, error) {
	handler := &{{.Name}}{}
	handler.Client = client
	handler.CollClient = coll_client
	return handler, nil
}

{{$service := .Service.Name -}}
{{$basename := .Service.BaseName -}}
{{$receiver := .Name -}}
{{$sdktrace := "trace2" -}}
{{range $_, $f := .Impl.Methods}}
func (handler *{{$receiver}}) {{$f.Name -}} ({{ArgVarsAndTypes $f "ctx context.Context"}}) ({{RetVarsAndTypes $f "err error"}}) {
	// Get baggage from context and create a copy to avoid mutating shared state
	upstreamBaggage := backend.GetBaggageFromContext(ctx)
	baggage := make(map[string]string)
	if upstreamBaggage != nil {
		for k, v := range upstreamBaggage {
			baggage[k] = v
		}
	}
	
	tp, _ := handler.CollClient.GetTracerProvider(ctx)
	tr := tp.Tracer("{{$service}}")
	
	ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))

	defer span.End()
	
	// Extract baggage from span attributes by casting to ReadWriteSpan
	if rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {
		for _, attr := range rwSpan.Attributes() {
			if strings.HasPrefix(string(attr.Key), "__bag.") {
				key := strings.TrimPrefix(string(attr.Key), "__bag.")
				// Convert value to string for baggage based on attribute type
				switch attr.Value.Type() {
				case attribute.INT64:
					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
				case attribute.STRING:
					baggage[key] = attr.Value.AsString()
				default:
					baggage[key] = attr.Value.AsString()
				}
			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
				key := strings.TrimPrefix(string(attr.Key), "__bagdel.")
				delete(baggage, key)
			}
		}
	}
	
	// Combine trace context with baggage
	trace_ctx, _ := span.SpanContext().MarshalJSON()
	trace_ctx_with_baggage, _ := backend.AddBaggageToTraceContext(string(trace_ctx), baggage)
	
	{{RetVars $f "err"}} = handler.Client.{{$f.Name}}({{ArgVars $f "ctx"}}, trace_ctx_with_baggage)
	if err != nil {
		span.RecordError(err)
	}
	
	return
}
{{end}}
`

var clientSideTemplateCGPB = `// Blueprint: Auto-generated by OpenTelemetry Plugin
package {{.Package.ShortName}}

{{.Imports}}

type {{.IfaceName}} interface {
	{{range $_, $f := .Impl.Methods -}}
	{{Signature $f}}
	{{end}}
}

type {{.Name}} struct {
	Client {{.ServerIfaceName}}
	CollClient {{.Imports.NameOf .CollIface.UserType}}
}

func New_{{.Name}}(ctx context.Context, client {{.ServerIfaceName}}, coll_client {{.Imports.NameOf .CollIface.UserType}}) (*{{.Name}}, error) {
	handler := &{{.Name}}{}
	handler.Client = client
	handler.CollClient = coll_client
	return handler, nil
}

{{$service := .Service.Name -}}
{{$basename := .Service.BaseName -}}
{{$receiver := .Name -}}
{{$sdktrace := "trace2" -}}
{{range $_, $f := .Impl.Methods}}
func (handler *{{$receiver}}) {{$f.Name -}} ({{ArgVarsAndTypes $f "ctx context.Context"}}) ({{RetVarsAndTypes $f "err error"}}) {
	// Get baggage from context and create a copy to avoid mutating shared state
	upstreamBaggage := backend.GetBaggageFromContext(ctx)
	baggage := make(map[string]string)
	if upstreamBaggage != nil {
		for k, v := range upstreamBaggage {
			baggage[k] = v
		}
	}
	
	tp, _ := handler.CollClient.GetTracerProvider(ctx)
	tr := tp.Tracer("{{$service}}")

	childCountPtr := ctx.Value("childCount").(*atomic.Uint64)
	ctx = context.WithValue(ctx, "seqNum", int(childCountPtr.Add(1)))
	
	ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))

	defer span.End()
	
	// Extract baggage from span attributes by casting to ReadWriteSpan
	if rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {
		for _, attr := range rwSpan.Attributes() {
			if strings.HasPrefix(string(attr.Key), "__bag.") {
				key := strings.TrimPrefix(string(attr.Key), "__bag.")
				// Convert value to string for baggage based on attribute type
				switch attr.Value.Type() {
				case attribute.INT64:
					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
				case attribute.STRING:
					baggage[key] = attr.Value.AsString()
				default:
					baggage[key] = attr.Value.AsString()
				}
			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
				key := strings.TrimPrefix(string(attr.Key), "__bagdel.")
				delete(baggage, key)
			}
		}
	}
	
	// Combine trace context with baggage
	trace_ctx, _ := span.SpanContext().MarshalJSON()
	trace_ctx_with_baggage, _ := backend.AddBaggageToTraceContext(string(trace_ctx), baggage)
	
	{{RetVars $f "err"}} = handler.Client.{{$f.Name}}({{ArgVars $f "ctx"}}, trace_ctx_with_baggage)
	if err != nil {
		span.RecordError(err)
	}
	
	return
}
{{end}}
`

var clientSideTemplatePath = `// Blueprint: Auto-generated by OpenTelemetry Plugin
package {{.Package.ShortName}}

{{.Imports}}

type {{.IfaceName}} interface {
	{{range $_, $f := .Impl.Methods -}}
	{{Signature $f}}
	{{end}}
}

type {{.Name}} struct {
	Client {{.ServerIfaceName}}
	CollClient {{.Imports.NameOf .CollIface.UserType}}
}

func New_{{.Name}}(ctx context.Context, client {{.ServerIfaceName}}, coll_client {{.Imports.NameOf .CollIface.UserType}}) (*{{.Name}}, error) {
	handler := &{{.Name}}{}
	handler.Client = client
	handler.CollClient = coll_client
	return handler, nil
}

{{$service := .Service.Name -}}
{{$basename := .Service.BaseName -}}
{{$receiver := .Name -}}
{{$sdktrace := "trace2" -}}
{{range $_, $f := .Impl.Methods}}
func (handler *{{$receiver}}) {{$f.Name -}} ({{ArgVarsAndTypes $f "ctx context.Context"}}) ({{RetVarsAndTypes $f "err error"}}) {
	// Get baggage from context and create a copy to avoid mutating shared state
	upstreamBaggage := backend.GetBaggageFromContext(ctx)
	baggage := make(map[string]string)
	if upstreamBaggage != nil {
		for k, v := range upstreamBaggage {
			baggage[k] = v
		}
	}
	
	tp, _ := handler.CollClient.GetTracerProvider(ctx)
	tr := tp.Tracer("{{$service}}")

	childCountPtr := ctx.Value("childCount").(*atomic.Uint64)
	ctx = context.WithValue(ctx, "seqNum", int(childCountPtr.Add(1)))
	
	ctx, span := tr.Start(ctx, "{{$basename}}Client_{{$f.Name}}", trace.WithSpanKind(trace.SpanKindClient))

	defer span.End()
	
	// Extract baggage from span attributes by casting to ReadWriteSpan
	if rwSpan, ok := span.({{$sdktrace}}.ReadWriteSpan); ok {
		for _, attr := range rwSpan.Attributes() {
			if strings.HasPrefix(string(attr.Key), "__bag.") {
				key := strings.TrimPrefix(string(attr.Key), "__bag.")
				// Convert value to string for baggage based on attribute type
				switch attr.Value.Type() {
				case attribute.INT64:
					baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
				case attribute.STRING:
					baggage[key] = attr.Value.AsString()
				default:
					baggage[key] = attr.Value.AsString()
				}
			} else if strings.HasPrefix(string(attr.Key), "__bagdel.") {
				key := strings.TrimPrefix(string(attr.Key), "__bagdel.")
				delete(baggage, key)
			}
		}
	}
	
	// Combine trace context with baggage
	trace_ctx, _ := span.SpanContext().MarshalJSON()
	trace_ctx_with_baggage, _ := backend.AddBaggageToTraceContext(string(trace_ctx), baggage)
	
	{{RetVars $f "err"}} = handler.Client.{{$f.Name}}({{ArgVars $f "ctx"}}, trace_ctx_with_baggage)
	if err != nil {
		span.RecordError(err)
	}
	
	return
}
{{end}}
`
