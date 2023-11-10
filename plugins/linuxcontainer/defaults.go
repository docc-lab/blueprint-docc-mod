package linuxcontainer

import (
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/ioutil"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/ir"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/linux"
)

/*
Registers a linux process workspace as the default way of combining and building processes
*/

func init() {
	RegisterBuilders()
}

// to trigger module initialization and register builders
func RegisterBuilders() {
	ir.RegisterDefaultNamespace[linux.Process]("linuxcontainer", buildDefaultLinuxWorkspace)
	ir.RegisterDefaultBuilder[*Container]("linuxcontainer", buildDefaultLinuxContainer)
}

func buildDefaultLinuxWorkspace(outputDir string, nodes []ir.IRNode) error {
	ctr := newLinuxContainerNode("default")
	ctr.ContainedNodes = nodes
	return ctr.GenerateArtifacts(outputDir)
}

func buildDefaultLinuxContainer(outputDir string, node ir.IRNode) error {
	if ctr, isContainer := node.(*Container); isContainer {
		ctrDir, err := ioutil.CreateNodeDir(outputDir, node.Name())
		if err != nil {
			return err
		}
		if err := ctr.GenerateArtifacts(ctrDir); err != nil {
			return err
		}
	}
	return nil
}
