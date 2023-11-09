<!-- Code generated by gomarkdoc. DO NOT EDIT -->

# dockerdeployment

```go
import "gitlab.mpi-sws.org/cld/blueprint/plugins/dockerdeployment"
```

## Index

- [func AddContainerToDeployment\(wiring blueprint.WiringSpec, deploymentName, containerName string\)](<#AddContainerToDeployment>)
- [func NewDeployment\(wiring blueprint.WiringSpec, deploymentName string, containers ...string\) string](<#NewDeployment>)
- [func RegisterBuilders\(\)](<#RegisterBuilders>)
- [type Deployment](<#Deployment>)
  - [func \(node \*Deployment\) AddArg\(argnode blueprint.IRNode\)](<#Deployment.AddArg>)
  - [func \(node \*Deployment\) AddChild\(child blueprint.IRNode\) error](<#Deployment.AddChild>)
  - [func \(node \*Deployment\) GenerateArtifacts\(dir string\) error](<#Deployment.GenerateArtifacts>)
  - [func \(node \*Deployment\) Name\(\) string](<#Deployment.Name>)
  - [func \(node \*Deployment\) String\(\) string](<#Deployment.String>)
- [type DockerComposeNamespace](<#DockerComposeNamespace>)


<a name="AddContainerToDeployment"></a>
## func AddContainerToDeployment

```go
func AddContainerToDeployment(wiring blueprint.WiringSpec, deploymentName, containerName string)
```

Adds a child node to an existing container deployment

<a name="NewDeployment"></a>
## func NewDeployment

```go
func NewDeployment(wiring blueprint.WiringSpec, deploymentName string, containers ...string) string
```

Adds a deployment that explicitly instantiates all of the containers provided. The deployment will also implicitly instantiate any of the dependencies of the containers

<a name="RegisterBuilders"></a>
## func RegisterBuilders

```go
func RegisterBuilders()
```

to trigger module initialization and register builders

<a name="Deployment"></a>
## type Deployment

A deployment is a collection of containers

```go
type Deployment struct {
    core.DeploymentNode

    DeploymentName string
    ArgNodes       []blueprint.IRNode
    ContainedNodes []blueprint.IRNode
    // contains filtered or unexported fields
}
```

<a name="Deployment.AddArg"></a>
### func \(\*Deployment\) AddArg

```go
func (node *Deployment) AddArg(argnode blueprint.IRNode)
```



<a name="Deployment.AddChild"></a>
### func \(\*Deployment\) AddChild

```go
func (node *Deployment) AddChild(child blueprint.IRNode) error
```



<a name="Deployment.GenerateArtifacts"></a>
### func \(\*Deployment\) GenerateArtifacts

```go
func (node *Deployment) GenerateArtifacts(dir string) error
```



<a name="Deployment.Name"></a>
### func \(\*Deployment\) Name

```go
func (node *Deployment) Name() string
```



<a name="Deployment.String"></a>
### func \(\*Deployment\) String

```go
func (node *Deployment) String() string
```



<a name="DockerComposeNamespace"></a>
## type DockerComposeNamespace

Used during building to accumulate docker container nodes Non\-container nodes will just be recursively fetched from the parent namespace

```go
type DockerComposeNamespace struct {
    blueprint.SimpleNamespace
    // contains filtered or unexported fields
}
```

Generated by [gomarkdoc](<https://github.com/princjef/gomarkdoc>)