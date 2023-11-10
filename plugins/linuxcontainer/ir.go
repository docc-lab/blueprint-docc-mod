package linuxcontainer

import (
	"strings"

	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/ir"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/stringutil"
)

/*
linuxcontainer.Container is a node that represents a collection of runnable linux processes.
It can contain any number of other process.Node IRNodes.  When it's compiled, the goproc.Process
will generate a run script that instantiates all contained processes.
*/

type Container struct {
	ir.IRNode

	/* The implemented build targets for linuxcontainer.Container nodes */
	filesystemDeployer /* Can be deployed as a basic collection of processes; implemented in deploy.go */
	dockerDeployer     /* Can be deployed as a docker container; implemented in deploydocker.go */

	InstanceName   string
	ImageName      string
	ArgNodes       []ir.IRNode
	ContainedNodes []ir.IRNode
}

func newLinuxContainerNode(name string) *Container {
	node := Container{}
	node.InstanceName = name
	node.ImageName = ir.CleanName(name)
	return &node
}

func (node *Container) Name() string {
	return node.InstanceName
}

func (node *Container) String() string {
	var b strings.Builder
	b.WriteString(node.InstanceName)
	b.WriteString(" = LinuxContainer(")
	var args []string
	for _, arg := range node.ArgNodes {
		args = append(args, arg.Name())
	}
	b.WriteString(strings.Join(args, ", "))
	b.WriteString(") {\n")
	var children []string
	for _, child := range node.ContainedNodes {
		children = append(children, child.String())
	}
	b.WriteString(stringutil.Indent(strings.Join(children, "\n"), 2))
	b.WriteString("\n}")
	return b.String()
}

func (node *Container) AddArg(argnode ir.IRNode) {
	node.ArgNodes = append(node.ArgNodes, argnode)
}

func (node *Container) AddChild(child ir.IRNode) error {
	node.ContainedNodes = append(node.ContainedNodes, child)
	return nil
}
