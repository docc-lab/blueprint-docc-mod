package goproc

import (
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/ir"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang"
)

/*
goproc.Process is a node that represents a runnable Golang process.  It can contain any number of
other golang.Node IRNodes.  When it's compiled, the goproc.Process will generate a go module with
a runnable main method that instantiates and initializes the contained go nodes.  To achieve this,
the golang.Process also collects module dependencies from its contained nodes.

The `GenerateArtifacts` method generates the main method based on the process's contained nodes.

Most of the heavy lifting of code generation is done by the following:
* gogen/workspacebuilder
* gogen/modulebuilder
* gogen/namespacebuilder

*/

var generatedModulePrefix = "blueprint/goproc"

// An IRNode representing a golang process.
// This is Blueprint's main implementation of Golang processes
type Process struct {
	/* The implemented build targets for golang.Process nodes */
	filesystemDeployer /* Can be deployed as a basic go process; implemented in deploy.go */
	linuxDeployer      /* Can be deployed to linux; implemented in deploylinux.go */

	InstanceName string
	ProcName     string
	ModuleName   string
	Nodes        []ir.IRNode
	Edges        []ir.IRNode
}

// A Golang Process Node can either be given the child nodes ahead of time, or they can be added using AddArtifactNode / AddCodeNode
func newGolangProcessNode(name string) *Process {
	proc := Process{
		InstanceName: name,
		ProcName:     ir.CleanName(name),
	}
	proc.ModuleName = generatedModulePrefix + "/" + proc.ProcName
	return &proc
}

// Implements ir.IRNode
func (proc *Process) Name() string {
	return proc.InstanceName
}

// Implements ir.IRNode
func (proc *Process) String() string {
	return ir.PrettyPrintNamespace(proc.InstanceName, "GolangProcessNode", proc.Edges, proc.Nodes)
}

// Implements NamespaceHandler
func (proc *Process) Accepts(nodeType any) bool {
	_, isGolangNode := nodeType.(golang.Node)
	return isGolangNode
}

// Implements NamespaceHandler
func (proc *Process) AddEdge(name string, edge ir.IRNode) error {
	proc.Edges = append(proc.Edges, edge)
	return nil
}

// Implements NamespaceHandler
func (proc *Process) AddNode(name string, node ir.IRNode) error {
	proc.Nodes = append(proc.Nodes, node)
	return nil
}
