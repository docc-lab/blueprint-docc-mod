package goproc

import (
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/blueprint"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/ioutil"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang"
)

func init() {
	RegisterDefaultBuilders()
}

func RegisterDefaultBuilders() {
	/* any unattached golang nodes will be instantiated in a "default" golang workspace */
	blueprint.RegisterDefaultNamespace[golang.Node]("goproc", buildDefaultGolangWorkspace)
	blueprint.RegisterDefaultBuilder[*Process]("goproc", buildDefaultGolangProcess)
}

/*
If the Blueprint application contains any floating golang nodes, they get
built by this function.
*/
func buildDefaultGolangWorkspace(outputDir string, nodes []blueprint.IRNode) error {
	proc := newGolangProcessNode("default")
	proc.ContainedNodes = nodes
	return proc.GenerateArtifacts(outputDir)
}

/*
If the Blueprint application contains any floating goproc.Process nodes, they
get built by this function.
*/
func buildDefaultGolangProcess(outputDir string, node blueprint.IRNode) error {
	if proc, isProc := node.(*Process); isProc {
		procDir, err := ioutil.CreateNodeDir(outputDir, node.Name())
		if err != nil {
			return err
		}
		if err := proc.GenerateArtifacts(procDir); err != nil {
			return err
		}
	}
	return nil
}