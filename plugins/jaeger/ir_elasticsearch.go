package jaeger

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
)

// ElasticsearchContainer is a single-node Elasticsearch instance used
// exclusively as jaeger's persistent storage backend. There is no
// Go-side ES client in the application — jaeger reaches the container
// over the cluster network on its REST port.
//
// Configured as single-node + xpack.security disabled (the cluster
// network already isolates it). Heap sized via ES_JAVA_OPTS env var
// inside AddContainerInstance.
type ElasticsearchContainer struct {
	docker.Container
	docker.ProvidesContainerInstance

	InstanceName string
	BindAddr     *address.BindConfig
}

func newElasticsearchContainer(name string) *ElasticsearchContainer {
	return &ElasticsearchContainer{InstanceName: name}
}

// Implements ir.IRNode
func (e *ElasticsearchContainer) Name() string {
	return e.InstanceName
}

// Implements ir.IRNode
func (e *ElasticsearchContainer) String() string {
	return e.InstanceName + " = Elasticsearch(" + e.BindAddr.Name() + ")"
}

// Implements docker.ProvidesContainerInstance
func (e *ElasticsearchContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	e.BindAddr.Port = 9200
	if err := target.DeclarePrebuiltInstance(e.InstanceName, "docker.elastic.co/elasticsearch/elasticsearch:7.17.20", e.BindAddr); err != nil {
		return err
	}
	// ES_JAVA_OPTS is one of the few settings ES actually reads from
	// an uppercase env var. Pin heap to 4 GiB — the Elastic-recommended
	// upper bound (above this you start losing the zero-based-oops
	// compressed-pointer optimization). Container memory limit is
	// intentionally unset so the JVM has full off-heap headroom for
	// Lucene caches, Netty buffers, etc.
	if err := target.SetEnvironmentVariable(e.InstanceName, "ES_JAVA_OPTS", "-Xms4g -Xmx4g"); err != nil {
		return err
	}
	// Everything else uses dotted setting names that Kubernetes env
	// var names disallow — pass via container args (`-Ekey=value`)
	// which the elasticsearch entrypoint forwards to the JVM. With
	// `discovery.type=single-node`, ES bypasses the production-mode
	// bootstrap checks (including the vm.max_map_count host sysctl
	// requirement) we'd otherwise fail.
	if err := target.SetContainerCommand(e.InstanceName, []string{
		"elasticsearch",
		"-Ediscovery.type=single-node",
		"-Expack.security.enabled=false",
		"-Ebootstrap.memory_lock=false",
	}); err != nil {
		return err
	}
	return nil
}

// Compile-time check that ElasticsearchContainer satisfies the IR
// interface — Blueprint's wiring will pull this in via the address
// binding mechanism.
var _ ir.IRNode = (*ElasticsearchContainer)(nil)
