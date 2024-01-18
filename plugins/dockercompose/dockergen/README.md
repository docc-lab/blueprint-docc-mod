<!-- Code generated by gomarkdoc. DO NOT EDIT -->

# dockergen

```go
import "github.com/blueprint-uservices/blueprint/plugins/dockercompose/dockergen"
```

## Index

- [func ExecuteTemplate\(name string, body string, args any\) \(string, error\)](<#ExecuteTemplate>)
- [func ExecuteTemplateToFile\(name string, body string, args any, filename string\) error](<#ExecuteTemplateToFile>)
- [type DockerComposeFile](<#DockerComposeFile>)
  - [func NewDockerComposeFile\(workspaceName, workspaceDir, fileName string\) \*DockerComposeFile](<#NewDockerComposeFile>)
  - [func \(d \*DockerComposeFile\) AddBuildInstance\(instanceName string, containerTemplateName string, args ...ir.IRNode\) error](<#DockerComposeFile.AddBuildInstance>)
  - [func \(d \*DockerComposeFile\) AddEnvVar\(instanceName string, key string, val string\) error](<#DockerComposeFile.AddEnvVar>)
  - [func \(d \*DockerComposeFile\) AddImageInstance\(instanceName string, image string, args ...ir.IRNode\) error](<#DockerComposeFile.AddImageInstance>)
  - [func \(d \*DockerComposeFile\) Generate\(\) error](<#DockerComposeFile.Generate>)
  - [func \(d \*DockerComposeFile\) ResolveLocalDials\(\) error](<#DockerComposeFile.ResolveLocalDials>)


<a name="ExecuteTemplate"></a>
## func [ExecuteTemplate](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/template.go#L17>)

```go
func ExecuteTemplate(name string, body string, args any) (string, error)
```



<a name="ExecuteTemplateToFile"></a>
## func [ExecuteTemplateToFile](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/template.go#L21>)

```go
func ExecuteTemplateToFile(name string, body string, args any, filename string) error
```



<a name="DockerComposeFile"></a>
## type [DockerComposeFile](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L17-L25>)

Used for generating the docker\-compose file of a docker app

```go
type DockerComposeFile struct {
    WorkspaceName string
    WorkspaceDir  string
    FileName      string
    FilePath      string
    Instances     map[string]instance // Container instance declarations
    // contains filtered or unexported fields
}
```

<a name="NewDockerComposeFile"></a>
### func [NewDockerComposeFile](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L36>)

```go
func NewDockerComposeFile(workspaceName, workspaceDir, fileName string) *DockerComposeFile
```



<a name="DockerComposeFile.AddBuildInstance"></a>
### func \(\*DockerComposeFile\) [AddBuildInstance](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L60>)

```go
func (d *DockerComposeFile) AddBuildInstance(instanceName string, containerTemplateName string, args ...ir.IRNode) error
```



<a name="DockerComposeFile.AddEnvVar"></a>
### func \(\*DockerComposeFile\) [AddEnvVar](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L64>)

```go
func (d *DockerComposeFile) AddEnvVar(instanceName string, key string, val string) error
```



<a name="DockerComposeFile.AddImageInstance"></a>
### func \(\*DockerComposeFile\) [AddImageInstance](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L56>)

```go
func (d *DockerComposeFile) AddImageInstance(instanceName string, image string, args ...ir.IRNode) error
```



<a name="DockerComposeFile.Generate"></a>
### func \(\*DockerComposeFile\) [Generate](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L48>)

```go
func (d *DockerComposeFile) Generate() error
```



<a name="DockerComposeFile.ResolveLocalDials"></a>
### func \(\*DockerComposeFile\) [ResolveLocalDials](<https://github.com/blueprint-uservices/blueprint/blob/main/plugins/dockercompose/dockergen/dockercompose.go#L134>)

```go
func (d *DockerComposeFile) ResolveLocalDials() error
```



Generated by [gomarkdoc](<https://github.com/princjef/gomarkdoc>)