<!-- Code generated by gomarkdoc. DO NOT EDIT -->

# retries

```go
import "gitlab.mpi-sws.org/cld/blueprint/plugins/retries"
```

## Index

- [func AddRetries\(wiring blueprint.WiringSpec, serviceName string, max\_retries int64\)](<#AddRetries>)
- [type RetrierClient](<#RetrierClient>)
  - [func \(node \*RetrierClient\) AddInstantiation\(builder golang.GraphBuilder\) error](<#RetrierClient.AddInstantiation>)
  - [func \(node \*RetrierClient\) AddInterfaces\(builder golang.ModuleBuilder\) error](<#RetrierClient.AddInterfaces>)
  - [func \(node \*RetrierClient\) GenerateFuncs\(builder golang.ModuleBuilder\) error](<#RetrierClient.GenerateFuncs>)
  - [func \(node \*RetrierClient\) GetInterface\(ctx blueprint.BuildContext\) \(service.ServiceInterface, error\)](<#RetrierClient.GetInterface>)
  - [func \(node \*RetrierClient\) ImplementsGolangNode\(\)](<#RetrierClient.ImplementsGolangNode>)
  - [func \(node \*RetrierClient\) Name\(\) string](<#RetrierClient.Name>)
  - [func \(node \*RetrierClient\) String\(\) string](<#RetrierClient.String>)


<a name="AddRetries"></a>
## func AddRetries

```go
func AddRetries(wiring blueprint.WiringSpec, serviceName string, max_retries int64)
```

Modifies the given service such that all clients to that service retry \`max\_retries\` number of times on error.

<a name="RetrierClient"></a>
## type RetrierClient



```go
type RetrierClient struct {
    golang.Service
    golang.GeneratesFuncs
    golang.Instantiable

    InstanceName string
    Wrapped      golang.Service

    Max int64
    // contains filtered or unexported fields
}
```

<a name="RetrierClient.AddInstantiation"></a>
### func \(\*RetrierClient\) AddInstantiation

```go
func (node *RetrierClient) AddInstantiation(builder golang.GraphBuilder) error
```



<a name="RetrierClient.AddInterfaces"></a>
### func \(\*RetrierClient\) AddInterfaces

```go
func (node *RetrierClient) AddInterfaces(builder golang.ModuleBuilder) error
```



<a name="RetrierClient.GenerateFuncs"></a>
### func \(\*RetrierClient\) GenerateFuncs

```go
func (node *RetrierClient) GenerateFuncs(builder golang.ModuleBuilder) error
```



<a name="RetrierClient.GetInterface"></a>
### func \(\*RetrierClient\) GetInterface

```go
func (node *RetrierClient) GetInterface(ctx blueprint.BuildContext) (service.ServiceInterface, error)
```



<a name="RetrierClient.ImplementsGolangNode"></a>
### func \(\*RetrierClient\) ImplementsGolangNode

```go
func (node *RetrierClient) ImplementsGolangNode()
```



<a name="RetrierClient.Name"></a>
### func \(\*RetrierClient\) Name

```go
func (node *RetrierClient) Name() string
```



<a name="RetrierClient.String"></a>
### func \(\*RetrierClient\) String

```go
func (node *RetrierClient) String() string
```



Generated by [gomarkdoc](<https://github.com/princjef/gomarkdoc>)