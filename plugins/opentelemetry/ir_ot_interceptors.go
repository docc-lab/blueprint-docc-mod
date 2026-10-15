package opentelemetry

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
)

// Tomislav-RetCtx: IR nodes for tracing gRPC services with interceptors (see [InstrumentGRPC]).
//
// Unlike the wrapper nodes, these generate no code.  The interceptors live in the runtime package
// runtime/plugins/opentelemetry/interceptors; these nodes only declare the call that creates them,
// e.g. in the generated process code:
//
//	interceptors.NewServerInterceptor(ctx, collectorClient, "v")
//
// The gRPC plugin then passes the resulting interceptor to the generated gRPC server or client.

const interceptorsPackage = "github.com/blueprint-uservices/blueprint/runtime/plugins/opentelemetry/interceptors"

// The tracing server interceptor of one service.
type OpenTelemetryServerInterceptor struct {
	grpc.ServerInterceptorNode // makes this a grpc.ServerInterceptor; instance type grpc.UnaryServerInterceptor

	InterceptorName string
	Collector       OpenTelemetryCollectorInterface
	BridgeKind      string // OT_BRIDGE at build time
}

// The tracing client interceptor of one service (used by every caller of that service).
type OpenTelemetryClientInterceptor struct {
	grpc.ClientInterceptorNode // makes this a grpc.ClientInterceptor; instance type grpc.UnaryClientInterceptor

	InterceptorName string
	Collector       OpenTelemetryCollectorInterface
	BridgeKind      string // OT_BRIDGE at build time
}

func (n *OpenTelemetryServerInterceptor) Name() string { return n.InterceptorName }
func (n *OpenTelemetryClientInterceptor) Name() string { return n.InterceptorName }

func (n *OpenTelemetryServerInterceptor) String() string {
	return fmt.Sprintf("%s = OTServerInterceptor(%s, kind=%q)", n.InterceptorName, n.Collector.Name(), n.BridgeKind)
}
func (n *OpenTelemetryClientInterceptor) String() string {
	return fmt.Sprintf("%s = OTClientInterceptor(%s, kind=%q)", n.InterceptorName, n.Collector.Name(), n.BridgeKind)
}

func (n *OpenTelemetryServerInterceptor) ImplementsGolangNode() {}
func (n *OpenTelemetryClientInterceptor) ImplementsGolangNode() {}

func (n *OpenTelemetryServerInterceptor) AddInstantiation(builder golang.NamespaceBuilder) error {
	return declareInterceptor(builder, n.InterceptorName, "NewServerInterceptor", n.Collector, n.BridgeKind)
}
func (n *OpenTelemetryClientInterceptor) AddInstantiation(builder golang.NamespaceBuilder) error {
	return declareInterceptor(builder, n.InterceptorName, "NewClientInterceptor", n.Collector, n.BridgeKind)
}

// The tracing HTTP server middleware of one service (see [InstrumentHTTP]).
type OpenTelemetryHTTPServerMiddleware struct {
	http.ServerMiddlewareNode // makes this an http.ServerMiddleware; instance type httpmiddleware.ServerMiddleware

	MiddlewareName string
	Collector      OpenTelemetryCollectorInterface
	BridgeKind     string // OT_BRIDGE at build time
}

// The tracing HTTP client middleware of one service (used by every caller of that service).
type OpenTelemetryHTTPClientMiddleware struct {
	http.ClientMiddlewareNode // makes this an http.ClientMiddleware; instance type httpmiddleware.ClientMiddleware

	MiddlewareName string
	Collector      OpenTelemetryCollectorInterface
	BridgeKind     string // OT_BRIDGE at build time
}

func (n *OpenTelemetryHTTPServerMiddleware) Name() string { return n.MiddlewareName }
func (n *OpenTelemetryHTTPClientMiddleware) Name() string { return n.MiddlewareName }

func (n *OpenTelemetryHTTPServerMiddleware) String() string {
	return fmt.Sprintf("%s = OTHTTPServerMiddleware(%s, kind=%q)", n.MiddlewareName, n.Collector.Name(), n.BridgeKind)
}
func (n *OpenTelemetryHTTPClientMiddleware) String() string {
	return fmt.Sprintf("%s = OTHTTPClientMiddleware(%s, kind=%q)", n.MiddlewareName, n.Collector.Name(), n.BridgeKind)
}

func (n *OpenTelemetryHTTPServerMiddleware) ImplementsGolangNode() {}
func (n *OpenTelemetryHTTPClientMiddleware) ImplementsGolangNode() {}

func (n *OpenTelemetryHTTPServerMiddleware) AddInstantiation(builder golang.NamespaceBuilder) error {
	return declareInterceptor(builder, n.MiddlewareName, "NewHTTPServerMiddleware", n.Collector, n.BridgeKind)
}
func (n *OpenTelemetryHTTPClientMiddleware) AddInstantiation(builder golang.NamespaceBuilder) error {
	return declareInterceptor(builder, n.MiddlewareName, "NewHTTPClientMiddleware", n.Collector, n.BridgeKind)
}

// declareInterceptor emits `interceptors.<constructor>(ctx, <collector client>, "<kind>")` for the node
// (a gRPC interceptor or an HTTP middleware).
func declareInterceptor(builder golang.NamespaceBuilder, name, constructorName string, collector ir.IRNode, kind string) error {
	if builder.Visited(name) {
		return nil
	}
	constructor := &gocode.Constructor{
		Package: interceptorsPackage,
		Func: gocode.Func{
			Name: constructorName,
			Arguments: []gocode.Variable{
				{Name: "ctx", Type: &gocode.UserType{Package: "context", Name: "Context"}},
				{Name: "tracer", Type: &gocode.UserType{Package: "github.com/blueprint-uservices/blueprint/runtime/core/backend", Name: "Tracer"}},
				{Name: "kind", Type: &gocode.BasicType{Name: "string"}},
			},
		},
	}
	return builder.DeclareConstructor(name, constructor, []ir.IRNode{collector, &ir.IRValue{Value: kind}})
}
