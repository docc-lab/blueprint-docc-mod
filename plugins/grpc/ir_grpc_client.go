package grpc

import (
	"fmt"
	"time"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
	"github.com/blueprint-uservices/blueprint/plugins/grpc/grpccodegen"
	"golang.org/x/exp/slog"
)

/*
IRNode representing a client to a Golang server.
This node does not introduce any new runtime interfaces or types that can be used by other IRNodes
GRPC code generation happens during the ModuleBuilder GenerateFuncs pass
*/
type golangClient struct {
	golang.Service
	golang.GeneratesFuncs

	InstanceName string
	ServerAddr   *address.Address[*golangServer]
	// Tomislav-RetCtx: carry the validated wiring deadline into client generation.
	RequestTimeout string
	// Tomislav-RetCtx: interceptors installed on this client, outermost first (see AddClientInterceptor).
	Interceptors []ClientInterceptor

	outputPackage string
}

func newGolangClient(name string, addr *address.Address[*golangServer], requestTimeout string, interceptors []ClientInterceptor) (*golangClient, error) {
	if duration, err := time.ParseDuration(requestTimeout); err != nil || duration <= 0 {
		return nil, fmt.Errorf("gRPC timeout must be a positive duration, got %q", requestTimeout)
	}
	node := &golangClient{}
	node.InstanceName = name
	node.ServerAddr = addr
	node.RequestTimeout = requestTimeout
	node.Interceptors = interceptors
	node.outputPackage = "grpc"

	return node, nil
}

func (n *golangClient) String() string {
	args := n.ServerAddr.Dial.Name()
	for _, interceptor := range n.Interceptors {
		args += ", " + interceptor.Name()
	}
	return n.InstanceName + " = GRPCClient(" + args + ")"
}

func (n *golangClient) Name() string {
	return n.InstanceName
}

func (node *golangClient) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	iface, err := node.ServerAddr.Server.GetInterface(ctx)
	if err != nil {
		return nil, err
	}
	grpc, isGrpc := iface.(*gRPCInterface)
	if !isGrpc {
		return nil, fmt.Errorf("grpc client expected a GRPC interface from %v but found %v", node.ServerAddr.Server.Name(), iface)
	}
	wrapped, isValid := grpc.Wrapped.(*gocode.ServiceInterface)
	if !isValid {
		return nil, fmt.Errorf("grpc client expected the server's GRPC interface to wrap a gocode interface but found %v", grpc)
	}
	return wrapped, nil
}

// Just makes sure that the interface exposed by the server is included in the built module
func (node *golangClient) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.ServerAddr.Server.Wrapped.AddInterfaces(builder)
}

// Generates proto files and the RPC client
func (node *golangClient) GenerateFuncs(builder golang.ModuleBuilder) error {
	// Get the service that we are wrapping
	iface, err := golang.GetGoInterface(builder, node)
	if err != nil {
		return err
	}

	// Only generate grpc client instantiation code for this service once
	if builder.Visited(iface.Name + ".grpc.client") {
		return nil
	}

	// Generate the .proto files
	err = grpccodegen.GenerateGRPCProto(builder, iface, node.outputPackage)
	if err != nil {
		return err
	}

	// Generate the RPC client
	err = grpccodegen.GenerateClient(builder, iface, node.outputPackage)
	if err != nil {
		return err
	}

	return nil
}

func (node *golangClient) AddInstantiation(builder golang.NamespaceBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.InstanceName) {
		return nil
	}

	// Get the service that we are wrapping
	iface, err := golang.GetGoInterface(builder, node)
	if err != nil {
		return err
	}

	constructor := &gocode.Constructor{
		Package: builder.Module().Info().Name + "/" + node.outputPackage,
		Func: gocode.Func{
			Name: fmt.Sprintf("New_%v_GRPCClient", iface.BaseName),
			Arguments: []gocode.Variable{
				{Name: "ctx", Type: &gocode.UserType{Package: "context", Name: "Context"}},
				{Name: "addr", Type: &gocode.BasicType{Name: "string"}},
				// Tomislav-RetCtx: the timeout used to be variadic; it is always passed, so it is now a
				// plain argument and the variadic slot holds one argument per installed interceptor.
				{Name: "timeout", Type: &gocode.BasicType{Name: "string"}},
				{Name: "interceptors", Type: &gocode.Ellipsis{EllipsisOf: unaryClientInterceptorType}},
			},
		},
	}

	args := []ir.IRNode{node.ServerAddr.Dial, &ir.IRValue{Value: node.RequestTimeout}}
	for _, interceptor := range node.Interceptors {
		args = append(args, interceptor)
	}

	slog.Info(fmt.Sprintf("Instantiating GRPCClient %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))
	return builder.DeclareConstructor(node.InstanceName, constructor, args)
}

func (node *golangClient) ImplementsGolangNode()    {}
func (node *golangClient) ImplementsGolangService() {}
