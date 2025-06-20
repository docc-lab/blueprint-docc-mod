package specs

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/cart"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/catalogue"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/frontend"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/order"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/payment"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/queuemaster"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/shipping"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workflow/user"
	"github.com/blueprint-uservices/blueprint/examples/sockshop/workload/workloadgen"
	"github.com/blueprint-uservices/blueprint/plugins/clientpool"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/mongodb"
	"github.com/blueprint-uservices/blueprint/plugins/mysql"
	"github.com/blueprint-uservices/blueprint/plugins/simple"
	"github.com/blueprint-uservices/blueprint/plugins/tracecoordinator"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
	"github.com/blueprint-uservices/blueprint/plugins/workload"
	"github.com/blueprint-uservices/blueprint/plugins/greeter"
)

// A wiring spec that deploys each service into its own Docker container with an OpenTelemetry sidecar.
//
// All RPC calls are retried up to 3 times.
// RPC clients use a client pool with 10 clients.
// All services are instrumented with OpenTelemetry and traces are exported through the sidecar to Zipkin
//
// The user, cart, shipping, and orders services using separate MongoDB instances to store their data.
// The catalogue service uses MySQL to store catalogue data.
// The shipping service and queue master service run within the same process.
var DockerWithSidecar = cmdbuilder.SpecOption{
	Name:        "docker_with_sidecar",
	Description: "Deploys each service in a separate container with gRPC, uses OpenTelemetry sidecar for tracing, and uses mongodb as NoSQL database backends.",
	Build:       makeDockerWithSidecarSpec,
}

func makeDockerWithSidecarSpec(spec wiring.WiringSpec) ([]string, error) {
	tracecoordinator.NewCoordinator(spec, "coordinator")

	applyDockerDefaults := func(serviceName string, useHTTP ...bool) {
		retries.AddRetries(spec, serviceName, 3)
		clientpool.Create(spec, serviceName, 10)
		if len(useHTTP) > 0 && useHTTP[0] {
			http.Deploy(spec, serviceName)
		} else {
			grpc.Deploy(spec, serviceName)
		}
		goproc.Deploy(spec, serviceName)
		linuxcontainer.Deploy(spec, serviceName)
		gotests.Test(spec, serviceName)
	}

	// Deploy greeter sidecar for each main service
	mainServices := []string{"user_service", "payment_service", "cart_service", "shipping_service", "order_service", "catalogue_service", "frontend"}
	greeterSidecars := []string{}
	for _, svc := range mainServices {
		greeterName := svc + "_greeter_sidecar"
		greeter.Service(spec, greeterName)
		goproc.Deploy(spec, greeterName)
		linuxcontainer.Deploy(spec, greeterName)
		greeterSidecars = append(greeterSidecars, greeterName+"_ctr")
	}

	user_db := mongodb.Container(spec, "user_db")
	user_service := workflow.Service[user.UserService](spec, "user_service", user_db)
	applyDockerDefaults(user_service)

	payment_service := workflow.Service[payment.PaymentService](spec, "payment_service", "500")
	applyDockerDefaults(payment_service)

	cart_db := mongodb.Container(spec, "cart_db")
	cart_service := workflow.Service[cart.CartService](spec, "cart_service", cart_db)
	applyDockerDefaults(cart_service)

	shipqueue := simple.Queue(spec, "shipping_queue")
	shipdb := mongodb.Container(spec, "shipping_db")
	shipping_service := workflow.Service[shipping.ShippingService](spec, "shipping_service", shipqueue, shipdb)
	applyDockerDefaults(shipping_service)

	queue_master := workflow.Service[queuemaster.QueueMaster](spec, "queue_master", shipqueue, shipping_service)
	goproc.AddToProcess(spec, "shipping_proc", queue_master)

	order_db := mongodb.Container(spec, "order_db")
	order_service := workflow.Service[order.OrderService](spec, "order_service", user_service, cart_service, payment_service, shipping_service, order_db)
	applyDockerDefaults(order_service)

	catalogue_db := mysql.Container(spec, "catalogue_db")
	catalogue_service := workflow.Service[catalogue.CatalogueService](spec, "catalogue_service", catalogue_db)
	applyDockerDefaults(catalogue_service)

	frontend_service := workflow.Service[frontend.Frontend](spec, "frontend", user_service, catalogue_service, cart_service, order_service)
	applyDockerDefaults(frontend_service, true)

	wlgen := workload.Generator[workloadgen.SimpleWorkload](spec, "wlgen", frontend_service)

	return append([]string{"frontend_ctr", wlgen, "gotests"}, greeterSidecars...), nil
} 