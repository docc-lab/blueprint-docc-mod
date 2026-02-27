package burstload

import (
	"fmt"
	"reflect"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/blueprint"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"github.com/blueprint-uservices/blueprint/plugins/golang/gocode"
)

// Blueprint IR node representing a Burst Load Generator
type BurstLoadGenerator struct {
	golang.Service
	golang.GeneratesFuncs
	golang.Instantiable

	InstanceName  string
	Wrapped       golang.Service
	outputPackage string
	BurstSize     int64  // X requests/sec
	BurstDuration string // Y seconds (burst generation period)
	BurstInterval string // Z seconds (wait between burst periods)
}

func (node *BurstLoadGenerator) ImplementsGolangNode() {}

func (node *BurstLoadGenerator) Name() string {
	return node.InstanceName
}

func (node *BurstLoadGenerator) String() string {
	return node.Name() + " = BurstLoadGenerator(" + node.Wrapped.Name() + ")"
}

func newBurstLoadGenerator(name string, server ir.IRNode, burst_size int64, burst_duration string, burst_interval string) (*BurstLoadGenerator, error) {
	serverNode, is_callable := server.(golang.Service)
	if !is_callable {
		return nil, blueprint.Errorf("burst load client wrapper requires %s to be a golang service but got %s", server.Name(), reflect.TypeOf(server).String())
	}

	node := &BurstLoadGenerator{}
	node.InstanceName = name
	node.Wrapped = serverNode
	node.outputPackage = "burstload"
	node.BurstSize = burst_size
	node.BurstDuration = burst_duration
	node.BurstInterval = burst_interval

	return node, nil
}

func (node *BurstLoadGenerator) AddInterfaces(builder golang.ModuleBuilder) error {
	return node.Wrapped.AddInterfaces(builder)
}

func (node *BurstLoadGenerator) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	return node.Wrapped.GetInterface(ctx)
}

func (node *BurstLoadGenerator) GenerateFuncs(builder golang.ModuleBuilder) error {
	if builder.Visited(node.InstanceName + ".generateFuncs") {
		return nil
	}

	iface, err := golang.GetGoInterface(builder, node)
	if err != nil {
		return err
	}

	return generateBurstLoadGenerator(builder, iface, node.outputPackage, node.BurstSize, node.BurstDuration, node.BurstInterval)
}

func (node *BurstLoadGenerator) AddInstantiation(builder golang.NamespaceBuilder) error {
	if builder.Visited(node.InstanceName) {
		return nil
	}

	iface, err := golang.GetGoInterface(builder, node.Wrapped)
	if err != nil {
		return err
	}

	constructor := &gocode.Constructor{
		Package: builder.Module().Info().Name + "/" + node.outputPackage,
		Func: gocode.Func{
			Name: fmt.Sprintf("New_%v_BurstLoadGenerator", iface.BaseName),
			Arguments: []gocode.Variable{
				{Name: "ctx", Type: &gocode.UserType{Package: "context", Name: "Context"}},
				{Name: "client", Type: iface},
			},
		},
	}

	return builder.DeclareConstructor(node.InstanceName, constructor, []ir.IRNode{node.Wrapped})
}


