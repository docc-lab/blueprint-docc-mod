package simplenosqldb

import (
	"fmt"

	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/core/backend"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/core/irutil"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/core/service"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang/gocode"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/workflow"
	"golang.org/x/exp/slog"
)

type SimpleNoSQLDB struct {
	golang.Service
	backend.NoSQLDB

	// Interfaces for generating Golang artifacts
	golang.ProvidesModule
	golang.Instantiable

	InstanceName string

	Iface       *gocode.ServiceInterface // The NoSQLDB interface
	Constructor *gocode.Constructor      // Constructor for this SimpleNoSQLDB implementation
}

func newSimpleNoSQLDB(name string) (*SimpleNoSQLDB, error) {
	node := &SimpleNoSQLDB{}
	err := node.init(name)
	if err != nil {
		return nil, err
	}

	return node, nil
}

func (node *SimpleNoSQLDB) init(name string) error {
	// We use the workflow spec to load the nosqldb interface details
	workflow.Init("../../runtime")

	// Look up the service details; errors out if the service doesn't exist
	spec, err := workflow.GetSpec()
	if err != nil {
		return err
	}
	details, err := spec.Get("SimpleNoSQLDB")
	if err != nil {
		return err
	}

	node.InstanceName = name
	node.Iface = details.Iface.ServiceInterface(&irutil.NullBuildContext{})
	node.Constructor = details.Constructor.AsConstructor()
	return nil
}

func (node *SimpleNoSQLDB) Name() string {
	return node.InstanceName
}

func (node *SimpleNoSQLDB) GetInterface(visitor irutil.BuildContext) service.ServiceInterface {
	return node.GetGoInterface(visitor)
}

func (node *SimpleNoSQLDB) GetGoInterface(visitor irutil.BuildContext) *gocode.ServiceInterface {
	return node.Iface
}

/* The nosqldb interface and SimpleNoSQLDB implementation exist in the runtime package */
func (node *SimpleNoSQLDB) AddToWorkspace(builder golang.WorkspaceBuilder) error {
	// Add blueprint runtime to the workspace
	if !builder.Visited("runtime") {
		slog.Info("Copying local module runtime to workspace")
		return builder.AddLocalModuleRelative("runtime", "../../../runtime")
	}
	return nil
}

func (node *SimpleNoSQLDB) AddInstantiation(builder golang.GraphBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.InstanceName) {
		return nil
	}

	slog.Info(fmt.Sprintf("Instantiating SimpleNoSQLDB %v in %v/%v", node.InstanceName, builder.Info().Package.PackageName, builder.Info().FileName))
	return builder.DeclareConstructor(node.InstanceName, node.Constructor, nil)
}

func (node *SimpleNoSQLDB) String() string {
	return fmt.Sprintf("%v = SimpleNoSQLDB()", node.InstanceName)
}

func (node *SimpleNoSQLDB) ImplementsGolangNode()    {}
func (node *SimpleNoSQLDB) ImplementsGolangService() {}