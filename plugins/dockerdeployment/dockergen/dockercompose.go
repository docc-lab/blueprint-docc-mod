package dockergen

import (
	"fmt"
	"path/filepath"

	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/blueprint"
	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/core/address"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/linux"
	"golang.org/x/exp/slog"
)

/*
Generates the docker-compose file of a docker app
*/

type DockerComposeFile struct {
	WorkspaceName string
	WorkspaceDir  string
	FileName      string
	FilePath      string
	Instances     map[string]instance            // Container instance declarations
	localServers  map[string]*address.BindConfig // Servers that have been defined within this docker-compose file
	localDials    map[string]*address.DialConfig // All servers that will be dialed from within this docker-compose file
}

type instance struct {
	InstanceName      string
	ContainerTemplate string              // only used if built; empty if not
	Image             string              // only used by prebuilt; empty if not
	Ports             map[string]uint16   // Map from bindconfig name to internal port
	Config            map[string]string   // Map from environment variable name to value
	Passthrough       map[string]struct{} // Environment variables that just get passed through to the container
}

func NewDockerComposeFile(workspaceName, workspaceDir, fileName string) *DockerComposeFile {
	return &DockerComposeFile{
		WorkspaceName: workspaceName,
		WorkspaceDir:  workspaceDir,
		FileName:      fileName,
		FilePath:      filepath.Join(workspaceDir, fileName),
		Instances:     make(map[string]instance),
		localServers:  make(map[string]*address.BindConfig),
		localDials:    make(map[string]*address.DialConfig),
	}
}

func (d *DockerComposeFile) Generate() error {
	// Point any local dials directly to the local server
	d.ResolveLocalDials()
	slog.Info(fmt.Sprintf("Generating %v/%v", d.WorkspaceName, d.FileName))
	return ExecuteTemplateToFile("docker-compose", dockercomposeTemplate, d, d.FilePath)

}

func (d *DockerComposeFile) AddImageInstance(instanceName string, image string, args ...blueprint.IRNode) error {
	return d.addInstance(instanceName, image, "", args...)
}

func (d *DockerComposeFile) AddBuildInstance(instanceName string, containerTemplateName string, args ...blueprint.IRNode) error {
	return d.addInstance(instanceName, "", containerTemplateName, args...)
}

func (d *DockerComposeFile) addInstance(instanceName string, image string, containerTemplateName string, args ...blueprint.IRNode) error {
	if _, exists := d.Instances[instanceName]; exists {
		return blueprint.Errorf("re-declaration of container instance %v of image %v", instanceName, image)
	}
	instance := instance{
		InstanceName:      instanceName,
		ContainerTemplate: containerTemplateName,
		Image:             image,
		Ports:             make(map[string]uint16),
		Config:            make(map[string]string),
		Passthrough:       make(map[string]struct{}),
	}
	for _, node := range args {
		varname := linux.EnvVar(node.Name())

		// TODO: only the server addrs that get passed in as node args to the docker deployment actually need to be included
		//    in the docker-compose ports section; the remainder only need to be exposed to containers within the deployment
		//    but not to the outsdie world

		// Docker containers should assign all internal server ports (typically using address.AssignPorts) before adding an instance
		if bind, isBindConfig := node.(*address.BindConfig); isBindConfig {
			if bind.Port == 0 {
				return fmt.Errorf("cannot add docker instance %v due to unbound server port %v", instanceName, bind.Name())
			}
			instance.Ports[requiredEnvVar(node)] = bind.Port
		}

		if conf, isConfig := node.(blueprint.IRConfig); isConfig {
			if conf.HasValue() {
				instance.Config[varname] = conf.Value()
				continue
			} else if conf.Optional() {
				instance.Passthrough[varname] = struct{}{}
				continue
			}
		}
		instance.Config[varname] = requiredEnvVar(node)
	}
	d.Instances[instanceName] = instance

	// Save all the dial and binds that we see; before compiling the docker-compose, we'll link them up to each other
	d.checkForAddrs(args)

	return nil
}

func (d *DockerComposeFile) checkForAddrs(nodes []blueprint.IRNode) {
	for _, node := range nodes {
		switch c := node.(type) {
		case *address.BindConfig:
			d.localServers[c.AddressName] = c
		case *address.DialConfig:
			d.localDials[c.AddressName] = c
		}
	}
}

func (d *DockerComposeFile) ResolveLocalDials() error {
	for name, bind := range d.localServers {
		dial, hasLocalDial := d.localDials[name]
		if !hasLocalDial {
			continue
		}

		// Update the configured value for any instance that uses this dial addr
		// to point it directly towards the local server
		dialVarname := linux.EnvVar(dial.Name())
		for _, instance := range d.Instances {
			if _, hasConfig := instance.Config[dialVarname]; hasConfig {
				instance.Config[dialVarname] = bind.Value()
			}
		}
	}
	return nil
}

func requiredEnvVar(node blueprint.IRNode) string {
	return fmt.Sprintf("${%v?%v must be set by the calling environment}", linux.EnvVar(node.Name()), node.Name())
}

var dockercomposeTemplate = `
version: '3'
services:
{{range $_, $decl := .Instances}}
  {{.InstanceName}}:
    {{if .Image -}}
    image: {{.Image}}
    {{- else if .ContainerTemplate -}}
    build:
      context: {{.ContainerTemplate}}
      dockerfile: ./Dockerfile
    {{- end}}
    hostname: {{.InstanceName}}
    {{- if .Ports}}
    expose:
    {{- range $_, $internal := .Ports}}
     - "{{$internal}}"
    {{- end}}
    ports:
    {{- range $external, $internal := .Ports}}
     - "{{$external}}:{{$internal}}"
    {{- end}}
    {{- end}}
    {{- if .Config}}
    environment:
    {{- range $name, $value := .Config}}
     - {{$name}}={{$value}}
    {{- end}}
    restart: always
    {{- end}}
{{end}}
`