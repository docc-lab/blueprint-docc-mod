package specs

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/dsb_sn/workflow/socialnetwork"
	"github.com/blueprint-uservices/blueprint/plugins/clientpool"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/mongodb"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/otelcol"
	"github.com/blueprint-uservices/blueprint/plugins/redis"
	"github.com/blueprint-uservices/blueprint/plugins/retries"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
)

// A wiring spec that deploys each service into its own Docker container and using thrift to communicate between services.
// All services except the Wrk2API service use thrift for communication; WRK2API service provides the http frontend.
// The user, socialgraph, urlshorten, and usertimeline services use MongoDB instances to store their data.
// The user, socialgraph, urlshorten, usertimeine, and hometimeline services use redis instances as the cache data for faster responses.
// (Previously used memcached, but switched to redis to support sorted sets for efficient timeline operations)
var Docker = cmdbuilder.SpecOption{
	Name:        "docker",
	Description: "Deploys each service in a separate container with thrift, and uses mongodb as NoSQL database backends.",
	Build:       makeDockerSpec,
}

// Create a basic social network wiring spec.
// Returns the names of the nodes to instantiate or an error.
func makeDockerSpec(spec wiring.WiringSpec) ([]string, error) {
	var containers []string
	var allServices []string

	jaeger_collector := jaeger.Collector(spec, "jaeger_v2")
	// trace_collector := otelcol.Collector(spec, "otelcol", jaeger_collector, "jaeger")
	// TODO: Document new fields in a readme + explain how to use otelcol plugin
	trace_collector := otelcol.CollectorWithConfig(
		spec, "otelcol_v2",
		jaeger_collector,
		// "/users/tomislav/opentelemetry-collector-contrib/test-config-bridges.yaml",
		"/users/tomislav/opentelemetry-collector-contrib/config-vanilla.yaml",
		"10.10.1.1:30000/otelcontribcol:latest",
		8080, "jaeger")

	applyDockerDefaults := func(serviceName string) string {
		retries.AddRetries(spec, serviceName, 3)
		// clientpool.Create(spec, serviceName, 10)
		// timeouts.Add(spec, serviceName, "5s")
		opentelemetry.Instrument(spec, serviceName, trace_collector)
		// opentelemetry.Instrument(spec, serviceName, jaeger_collector)
		// opentelemetry.Instrument(spec, serviceName)

		// thrift.Deploy(spec, serviceName)
		grpc.Deploy(spec, serviceName)
		// goproc.CreateProcess(spec, procName, serviceName)
		goproc.Deploy(spec, serviceName)
		return linuxcontainer.Deploy(spec, serviceName)
	}

	applyHTTPDefaults := func(serviceName string, collector string) string {
		retries.AddRetries(spec, serviceName, 3)
		clientpool.Create(spec, serviceName, 100)
		// timeouts.Add(spec, serviceName, "5s")
		opentelemetry.Instrument(spec, serviceName, collector)
		http.Deploy(spec, serviceName)
		// goproc.CreateProcess(spec, procName, serviceName)
		// linuxcontainer.CreateContainer(spec, ctrName, procName)
		goproc.Deploy(spec, serviceName)
		return linuxcontainer.Deploy(spec, serviceName)
	}

	// Define backends
	// user_cache := memcached.Container(spec, "user_cache")
	user_cache := redis.Container(spec, "user_cache_v2")
	user_db := mongodb.Container(spec, "user_db_v2")
	// post_cache := memcached.Container(spec, "post_cache")
	post_cache := redis.Container(spec, "post_cache_v2")
	post_db := mongodb.Container(spec, "post_db_v2")
	// social_cache := memcached.Container(spec, "social_cache")
	social_cache := redis.Container(spec, "social_cache_v2")
	social_db := mongodb.Container(spec, "social_db_v2")
	urlshorten_db := mongodb.Container(spec, "urlshorten_db_v2")
	// usertimeline_cache := memcached.Container(spec, "usertimeline_cache")
	usertimeline_cache := redis.Container(spec, "usertimeline_cache_v2")
	usertimeline_db := mongodb.Container(spec, "usertimeline_db_v2")
	// hometimeline_cache := memcached.Container(spec, "hometimeline_cache")
	hometimeline_cache := redis.Container(spec, "hometimeline_cache_v2")

	// Add backends to services list so that their client libraries are used in the generated tests!
	allServices = append(allServices, user_cache)
	allServices = append(allServices, user_db)
	allServices = append(allServices, post_cache)
	allServices = append(allServices, post_db)
	allServices = append(allServices, social_cache)
	allServices = append(allServices, social_db)
	allServices = append(allServices, usertimeline_cache)
	allServices = append(allServices, usertimeline_db)
	allServices = append(allServices, hometimeline_cache)

	// Define url_shorten service
	urlshorten_service := workflow.Service[socialnetwork.UrlShortenService](spec, "urlshorten_service_v2", urlshorten_db)
	containers = append(containers, applyDockerDefaults(urlshorten_service))
	// containers = append(containers, "urlshorten_ctr_v2")
	allServices = append(allServices, "urlshorten_service_v2")

	// Define user_mention service
	usermention_service := workflow.Service[socialnetwork.UserMentionService](spec, "usermention_service_v2", user_cache, user_db)
	containers = append(containers, applyDockerDefaults(usermention_service))
	// containers = append(containers, "usermention_ctr_v2")
	allServices = append(allServices, "usermention_service_v2")

	// Define post_storage service
	post_storage_service := workflow.Service[socialnetwork.PostStorageService](spec, "post_storage_service_v2", post_cache, post_db)
	containers = append(containers, applyDockerDefaults(post_storage_service))
	// containers = append(containers, "post_storage_ctr_v2")
	allServices = append(allServices, "post_storage_service_v2")

	// Define media service
	media_service := workflow.Service[socialnetwork.MediaService](spec, "media_service_v2")
	containers = append(containers, applyDockerDefaults(media_service))
	// containers = append(containers, "media_ctr_v2")
	allServices = append(allServices, "media_service_v2")

	// Define uniqueid service
	uniqueId_service := workflow.Service[socialnetwork.UniqueIdService](spec, "uniqueid_service_v2")
	containers = append(containers, applyDockerDefaults(uniqueId_service))
	// containers = append(containers, "uniqueid_ctr_v2")
	allServices = append(allServices, "uniqueid_service_v2")

	// Define user_id service
	userid_service := workflow.Service[socialnetwork.UserIDService](spec, "userid_service_v2", user_cache, user_db)
	containers = append(containers, applyDockerDefaults(userid_service))
	// containers = append(containers, "userid_ctr_v2")
	allServices = append(allServices, "userid_service_v2")

	// Define social_graph service
	socialgraph_service := workflow.Service[socialnetwork.SocialGraphService](spec, "socialgraph_service_v2", social_cache, social_db, userid_service)
	containers = append(containers, applyDockerDefaults(socialgraph_service))
	// containers = append(containers, "socialgraph_ctr_v2")
	allServices = append(allServices, "socialgraph_service_v2")

	// Define home_timeline service
	hometimeline_service := workflow.Service[socialnetwork.HomeTimelineService](spec, "hometimeline_service_v2", hometimeline_cache, post_storage_service, socialgraph_service)
	containers = append(containers, applyDockerDefaults(hometimeline_service))
	// containers = append(containers, "hometimeline_ctr_v2")
	allServices = append(allServices, "hometimeline_service_v2")

	// Define user service
	user_service := workflow.Service[socialnetwork.UserService](spec, "user_service_v2", user_cache, user_db, socialgraph_service, "secret")
	containers = append(containers, applyDockerDefaults(user_service))
	// containers = append(containers, "user_ctr_v2")
	allServices = append(allServices, "user_service_v2")

	// Define text service
	text_service := workflow.Service[socialnetwork.TextService](spec, "text_service_v2", urlshorten_service, usermention_service)
	containers = append(containers, applyDockerDefaults(text_service))
	// containers = append(containers, "text_ctr_v2")
	allServices = append(allServices, "text_service_v2")

	// Define user_timeline service
	usertimeline_service := workflow.Service[socialnetwork.UserTimelineService](spec, "usertimeline_service_v2", usertimeline_cache, usertimeline_db, post_storage_service)
	containers = append(containers, applyDockerDefaults(usertimeline_service))
	// containers = append(containers, "usertimeline_ctr_v2")
	allServices = append(allServices, "usertimeline_service_v2")

	// Define compose post service
	composepost_service := workflow.Service[socialnetwork.ComposePostService](spec, "composepost_service_v2", post_storage_service, usertimeline_service, user_service, uniqueId_service, media_service, text_service, hometimeline_service)
	containers = append(containers, applyDockerDefaults(composepost_service))
	// containers = append(containers, "composepost_ctr_v2")
	allServices = append(allServices, "composepost_service_v2")

	// Define frontend service
	wrk2api_service := workflow.Service[socialnetwork.Wrk2APIService](spec, "wrk2api_service_v2", user_service, composepost_service, usertimeline_service, hometimeline_service, socialgraph_service)
	containers = append(containers, applyHTTPDefaults(wrk2api_service, trace_collector))
	// containers = append(containers, applyHTTPDefaults(wrk2api_service, ""))
	// applyHTTPDefaults(wrk2api_service, jaeger_collector)
	// containers = append(containers, applyHTTPDefaults(wrk2api_service, ""))
	// containers = append(containers, "wrk2api_ctr_v2")
	allServices = append(allServices, "wrk2api_service_v2")

	tests := gotests.Test(spec, allServices...)
	containers = append(containers, tests, "otelcol_v2", "jaeger_v2")
	// containers = append(containers, tests, "jaeger")
	// containers = append(containers, tests)

	return containers, nil
}

// func applyDockerDefaults(spec wiring.WiringSpec, serviceName, procName, ctrName string) string {
// 	retries.AddRetries(spec, serviceName, 3)
// 	clientpool.Create(spec, serviceName, 20)

// 	thrift.Deploy(spec, serviceName)
// 	goproc.CreateProcess(spec, procName, serviceName)
// 	return linuxcontainer.CreateContainer(spec, ctrName, procName)
// }
