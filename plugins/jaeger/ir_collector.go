package jaeger

import (
	"strings"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
	"github.com/blueprint-uservices/blueprint/plugins/golang/goparser"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/jaeger"
)

// Blueprint IR node that represents the Jaeger container
type JaegerCollectorContainer struct {
	docker.Container

	CollectorName string
	BindAddr      *address.BindConfig
	UIBindAddr    *address.BindConfig
	OTLPBindAddr  *address.BindConfig

	// ESDialAddr, when non-nil, switches jaeger from its default
	// in-memory storage to an external Elasticsearch storage backend.
	// Wired up by [CollectorWithElasticsearch]; the standard [Collector]
	// wiring function leaves this nil (in-memory storage).
	ESDialAddr *address.DialConfig

	// ESContainerName is the docker-compose service name of the ES
	// container. Set alongside ESDialAddr by CollectorWithElasticsearch.
	// We use this directly for the ES_SERVER_URLS env var because the
	// DialConfig's Hostname is resolved by Blueprint after this node's
	// AddContainerInstance returns — too late for env var stamping.
	ESContainerName string

	Iface *goparser.ParsedInterface
}

// Jaeger interface exposed to the application.
type JaegerInterface struct {
	service.ServiceInterface
	Wrapped service.ServiceInterface
}

func (j *JaegerInterface) GetName() string {
	return "j(" + j.Wrapped.GetName() + ")"
}

func (j *JaegerInterface) GetMethods() []service.Method {
	return j.Wrapped.GetMethods()
}

func newJaegerCollectorContainer(name string) (*JaegerCollectorContainer, error) {
	spec, err := workflowspec.GetService[jaeger.JaegerTracer]()
	if err != nil {
		return nil, err
	}

	collector := &JaegerCollectorContainer{
		CollectorName: name,
		Iface:         spec.Iface,
	}
	return collector, nil
}

func (node *JaegerCollectorContainer) Name() string {
	return node.CollectorName
}

func (node *JaegerCollectorContainer) String() string {
	return node.Name() + " = JaegerCollector(" + node.BindAddr.Name() + ")"
}

func (node *JaegerCollectorContainer) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	iface := node.Iface.ServiceInterface(ctx)
	return &JaegerInterface{Wrapped: iface}, nil
}

func (node *JaegerCollectorContainer) AddContainerArtifacts(targer docker.ContainerWorkspace) error {
	return nil
}

func (node *JaegerCollectorContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	node.UIBindAddr.Port = 16686
	node.BindAddr.Port = 14268
	node.OTLPBindAddr.Port = 4317

	// Declare the prebuilt instance
	err := target.DeclarePrebuiltInstance(node.CollectorName, "jaegertracing/all-in-one:latest", node.BindAddr, node.UIBindAddr, node.OTLPBindAddr)
	if err != nil {
		return err
	}

	// Add clock skew adjustment environment variable
	err = target.SetEnvironmentVariable(node.CollectorName, "QUERY_MAX_CLOCK_SKEW_ADJUSTMENT", "1m")
	if err != nil {
		return err
	}

	// If an Elasticsearch dial address was wired in, switch jaeger to
	// the ES storage backend. Default (ESDialAddr==nil) leaves jaeger
	// on its in-memory storage.
	if node.ESDialAddr != nil {
		if err := target.SetEnvironmentVariable(node.CollectorName, "SPAN_STORAGE_TYPE", "elasticsearch"); err != nil {
			return err
		}
		// docker-compose converts dots in IR node names to underscores
		// when generating service names — replicate that here so the
		// hostname jaeger dials matches the compose service name.
		esHost := strings.ReplaceAll(node.ESContainerName, ".", "_")
		if err := target.SetEnvironmentVariable(node.CollectorName, "ES_SERVER_URLS", "http://"+esHost+":9200"); err != nil {
			return err
		}
	}

	return nil
}
