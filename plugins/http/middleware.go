package http

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/blueprint"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
)

// Tomislav-RetCtx: HTTP middleware support, the HTTP counterpart of the gRPC plugin's interceptors
// (plugins/grpc/interceptors.go).
//
// A middleware is any IR node whose runtime instance is an httpmiddleware.ServerMiddleware or
// httpmiddleware.ClientMiddleware (runtime/plugins/httpmiddleware).  The HTTP plugin does not care
// what the middleware does; it only passes the instances to the generated server, which wraps each
// method's handler with them, or to the generated client, which wraps its transport with them.
//
// To provide a middleware, a plugin defines an IR node that:
//   - implements golang.Instantiable, and its AddInstantiation declares a constructor that returns
//     an httpmiddleware.ServerMiddleware (or httpmiddleware.ClientMiddleware);
//   - implements [ServerMiddleware] (or [ClientMiddleware]), most easily by embedding
//     [ServerMiddlewareNode] (or [ClientMiddlewareNode]);
//   - is registered on a service with [AddServerMiddleware] (or [AddClientMiddleware]).
type (
	// An IR node whose runtime instance is an httpmiddleware.ServerMiddleware.
	ServerMiddleware interface {
		golang.Node
		golang.Instantiable
		golang.TypedInstance
		ImplementsHTTPServerMiddleware()
	}

	// An IR node whose runtime instance is an httpmiddleware.ClientMiddleware.
	ClientMiddleware interface {
		golang.Node
		golang.Instantiable
		golang.TypedInstance
		ImplementsHTTPClientMiddleware()
	}
)

const middlewarePackage = "github.com/blueprint-uservices/blueprint/runtime/plugins/httpmiddleware"

// The Go types of middleware instances, as used in generated code.
var (
	serverMiddlewareType = &gocode.UserType{Package: middlewarePackage, Name: "ServerMiddleware"}
	clientMiddlewareType = &gocode.UserType{Package: middlewarePackage, Name: "ClientMiddleware"}
)

// Embed ServerMiddlewareNode in an IR node to get the InstanceType and marker methods of
// [ServerMiddleware].
type ServerMiddlewareNode struct{}

func (ServerMiddlewareNode) InstanceType() gocode.TypeName   { return serverMiddlewareType }
func (ServerMiddlewareNode) ImplementsHTTPServerMiddleware() {}

// Embed ClientMiddlewareNode in an IR node to get the InstanceType and marker methods of
// [ClientMiddleware].
type ClientMiddlewareNode struct{}

func (ClientMiddlewareNode) InstanceType() gocode.TypeName   { return clientMiddlewareType }
func (ClientMiddlewareNode) ImplementsHTTPClientMiddleware() {}

// Property under which middleware node names are recorded on a service's HTTP server node
// (serviceName.http_server) and HTTP client node (serviceName.http_client).
const middlewareProp = "http.middleware"

// [AddServerMiddleware] installs a middleware on the HTTP server of serviceName.
//
// middlewareName is the name of another node in the wiring spec, which must implement
// [ServerMiddleware].  It is only recorded here; the HTTP server picks it up when it is built, so
// this can be called before or after [Deploy].  Middleware runs in the order it was added: the first
// one added is the outermost, i.e. it sees the request first and the response last.
func AddServerMiddleware(spec wiring.WiringSpec, serviceName string, middlewareName string) {
	spec.AddProperty(serviceName+".http_server", middlewareProp, middlewareName)
}

// [AddClientMiddleware] installs a middleware on every HTTP client of serviceName, i.e. on the
// caller side of calls to serviceName.
//
// middlewareName is the name of another node in the wiring spec, which must implement
// [ClientMiddleware].  Like [AddServerMiddleware], this can be called before or after [Deploy], and
// the first middleware added is the outermost.
func AddClientMiddleware(spec wiring.WiringSpec, serviceName string, middlewareName string) {
	spec.AddProperty(serviceName+".http_client", middlewareProp, middlewareName)
}

// getMiddleware looks up the middleware nodes recorded on nodeName.  T is [ServerMiddleware] or
// [ClientMiddleware].
func getMiddleware[T ir.IRNode](namespace wiring.Namespace, nodeName string) ([]T, error) {
	var names []string
	if err := namespace.GetProperties(nodeName, middlewareProp, &names); err != nil {
		return nil, blueprint.Errorf("unable to read the HTTP middleware of %s: %s", nodeName, err)
	}
	var middleware []T
	for _, name := range names {
		var m T
		if err := namespace.Get(name, &m); err != nil {
			return nil, blueprint.Errorf("HTTP node %s expected %s to be an HTTP middleware, but encountered %s", nodeName, name, err)
		}
		middleware = append(middleware, m)
	}
	return middleware, nil
}
