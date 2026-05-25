package redis

import (
	"os"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/address"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/backend"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/service"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/docker"
	"github.com/blueprint-uservices/blueprint/plugins/golang/goparser"
	"github.com/blueprint-uservices/blueprint/plugins/workflow/workflowspec"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/redis"
)

// RedisImageEnv overrides the docker image used for the prebuilt redis
// container. Read at compile time; empty falls back to defaultRedisImage.
//
// Examples:
//
//	REDIS_IMAGE=redis:7.4 go run wiring/main.go ...
//	REDIS_IMAGE=redis:8-alpine go run wiring/main.go ...
const RedisImageEnv = "REDIS_IMAGE"

// defaultRedisImage is the safe pin used when REDIS_IMAGE is unset.
// 8.6 is the current stable minor (auto-bumps patch releases) while
// staying off the floating `latest` tag.
const defaultRedisImage = "redis:8.6"

func redisImage() string {
	if v := os.Getenv(RedisImageEnv); v != "" {
		return v
	}
	return defaultRedisImage
}

// Blueprint IR Node that represents a redis container
type RedisContainer struct {
	backend.Cache
	docker.Container
	docker.ProvidesContainerInstance

	InstanceName string
	BindAddr     *address.BindConfig
	Iface        *goparser.ParsedInterface
}

// Redis interface exposed to other services.
// This interface can not be modified further.
type RedisInterface struct {
	service.ServiceInterface
	Wrapped service.ServiceInterface
}

func (r *RedisInterface) GetName() string {
	return "redis(" + r.Wrapped.GetName() + ")"
}

func (r *RedisInterface) GetMethods() []service.Method {
	return r.Wrapped.GetMethods()
}

func newRedisContainer(name string) (*RedisContainer, error) {
	spec, err := workflowspec.GetService[redis.RedisCache]()
	if err != nil {
		return nil, err
	}

	proc := &RedisContainer{
		InstanceName: name,
		Iface:        spec.Iface,
	}
	return proc, nil
}

// Implements ir.IRNode
func (r *RedisContainer) String() string {
	return r.InstanceName + " = RedisProcess(" + r.BindAddr.Name() + ")"
}

// Implements ir.IRNode
func (r *RedisContainer) Name() string {
	return r.InstanceName
}

// Implements service.ServiceNode
func (node *RedisContainer) GetInterface(ctx ir.BuildContext) (service.ServiceInterface, error) {
	iface := node.Iface.ServiceInterface(ctx)
	return &RedisInterface{Wrapped: iface}, nil
}

// Implements docker.ProvidesContainerInstance
func (node *RedisContainer) AddContainerInstance(target docker.ContainerWorkspace) error {
	node.BindAddr.Port = 6379 // Just use default redis port
	return target.DeclarePrebuiltInstance(node.InstanceName, redisImage(), node.BindAddr)
}
