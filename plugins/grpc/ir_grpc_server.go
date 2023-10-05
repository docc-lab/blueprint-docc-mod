package grpc

import (
	"fmt"
	"reflect"

	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/blueprint"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/core/irutil"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/core/service"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang/gocode"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/grpc/grpccodegen"
	"golang.org/x/exp/slog"
)

/*
IRNode representing a Golang GPRC server.
This node does not introduce any new runtime interfaces or types that can be used by other IRNodes
GRPC code generation happens during the ModuleBuilder GenerateFuncs pass
*/
type GolangServer struct {
	service.ServiceNode
	golang.GeneratesFuncs
	golang.Instantiable

	InstanceName string
	Addr         *GolangServerAddress
	Wrapped      golang.Service

	outputPackage string
}

// Represents a service that is exposed over GRPC
type GRPCInterface struct {
	service.ServiceInterface
	Wrapped service.ServiceInterface
}

func (grpc *GRPCInterface) GetName() string {
	return "grpc(" + grpc.Wrapped.GetName() + ")"
}

func (grpc *GRPCInterface) GetMethods() []service.Method {
	return grpc.Wrapped.GetMethods()
}

func newGolangServer(name string, serverAddr blueprint.IRNode, wrapped blueprint.IRNode) (*GolangServer, error) {
	addr, is_addr := serverAddr.(*GolangServerAddress)
	if !is_addr {
		return nil, blueprint.Errorf("GRPC server %s expected %s to be an address, but got %s", name, serverAddr.Name(), reflect.TypeOf(serverAddr).String())
	}

	service, is_service := wrapped.(golang.Service)
	if !is_service {
		return nil, blueprint.Errorf("GRPC server %s expected %s to be a golang service, but got %s", name, wrapped.Name(), reflect.TypeOf(wrapped).String())
	}

	node := &GolangServer{}
	node.InstanceName = name
	node.Addr = addr
	node.Wrapped = service
	node.outputPackage = "grpc"
	return node, nil
}

func (n *GolangServer) String() string {
	return n.InstanceName + " = GRPCServer(" + n.Wrapped.Name() + ", " + n.Addr.Name() + ")"
}

func (n *GolangServer) Name() string {
	return n.InstanceName
}

// Generates proto files and the RPC server handler
func (node *GolangServer) GenerateFuncs(builder golang.ModuleBuilder) error {
	// Get the service that we are wrapping
	service := node.Wrapped.GetGoInterface(builder)

	// Only generate grpc server instantiation code for this service once
	if builder.Visited(service.Name + ".grpc.server") {
		return nil
	}

	// Generate the .proto files
	err := grpccodegen.GenerateGRPCProto(builder, service, node.outputPackage)
	if err != nil {
		return err
	}

	// Generate the RPC server handler
	err = grpccodegen.GenerateServerHandler(builder, service, node.outputPackage)
	if err != nil {
		return err
	}

	return nil
}

func (node *GolangServer) AddInstantiation(builder golang.GraphBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.InstanceName) {
		return nil
	}

	service := node.Wrapped.GetGoInterface(builder)

	constructor := &gocode.Constructor{
		Package: builder.Module().Info().Name + "/" + node.outputPackage,
		Func: gocode.Func{
			Name: fmt.Sprintf("New_%v_GRPCServerHandler", service.Name),
			Arguments: []gocode.Variable{
				{Name: "service", Type: service},
				{Name: "serverAddr", Type: &gocode.BasicType{Name: "string"}},
			},
		},
	}

	slog.Info(fmt.Sprintf("Instantiating GRPCServer %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))
	return builder.DeclareConstructor(node.InstanceName, constructor, []blueprint.IRNode{node.Wrapped, node.Addr})
}

func (node *GolangServer) GetInterface(visitor irutil.BuildContext) service.ServiceInterface {
	return &GRPCInterface{Wrapped: node.Wrapped.GetInterface(visitor)}
}
func (node *GolangServer) ImplementsGolangNode() {}
