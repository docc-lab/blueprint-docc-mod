package grpc

import (
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
)

// Tomislav-RetCtx: gRPC interceptor support.
//
// An interceptor is any IR node whose runtime instance is a standard grpc-go interceptor function.
// The gRPC plugin does not care what the interceptor does (tracing, logging, ...); it only passes the
// instances to the generated server or client, which installs them with grpc.ChainUnaryInterceptor /
// grpc.WithChainUnaryInterceptor.
//
// Only unary interceptors are supported, because the gRPC plugin only generates unary methods.
//
// To provide an interceptor, a plugin defines an IR node that:
//   - implements golang.Instantiable, and its AddInstantiation declares a constructor that returns a
//     grpc.UnaryServerInterceptor (or grpc.UnaryClientInterceptor);
//   - implements [ServerInterceptor] (or [ClientInterceptor]);
//   - is registered on a service with [AddServerInterceptor] (or [AddClientInterceptor]).
//
// The InstanceType methods below come from the golang.TypedInstance interface.  They tell the generated
// dependency-injection code to fetch the instance as an interceptor function rather than as a string.
// Plugins get them for free by embedding [ServerInterceptorNode] or [ClientInterceptorNode].
type (
	// An IR node whose runtime instance is a grpc.UnaryServerInterceptor.
	ServerInterceptor interface {
		golang.Node
		golang.Instantiable
		golang.TypedInstance
		ImplementsGRPCServerInterceptor()
	}

	// An IR node whose runtime instance is a grpc.UnaryClientInterceptor.
	ClientInterceptor interface {
		golang.Node
		golang.Instantiable
		golang.TypedInstance
		ImplementsGRPCClientInterceptor()
	}
)

// The Go types of interceptor instances, as used in generated code.
var (
	unaryServerInterceptorType = &gocode.UserType{Package: "google.golang.org/grpc", Name: "UnaryServerInterceptor"}
	unaryClientInterceptorType = &gocode.UserType{Package: "google.golang.org/grpc", Name: "UnaryClientInterceptor"}
)

// Embed ServerInterceptorNode in an IR node to get the InstanceType and marker methods of
// [ServerInterceptor].
type ServerInterceptorNode struct{}

func (ServerInterceptorNode) InstanceType() gocode.TypeName     { return unaryServerInterceptorType }
func (ServerInterceptorNode) ImplementsGRPCServerInterceptor() {}

// Embed ClientInterceptorNode in an IR node to get the InstanceType and marker methods of
// [ClientInterceptor].
type ClientInterceptorNode struct{}

func (ClientInterceptorNode) InstanceType() gocode.TypeName     { return unaryClientInterceptorType }
func (ClientInterceptorNode) ImplementsGRPCClientInterceptor() {}
