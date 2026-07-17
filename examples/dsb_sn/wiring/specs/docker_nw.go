package specs

import (
	"fmt"
	"os"
	"strings"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/dsb_sn/workflow/snnw"
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

// ZERO-WORK variants: identical topology/call-graph to the standard specs, but
// backed by the `snnw` workflow package whose services do NO DB/cache I/O and NO
// app compute — they just forward to downstream services. Isolates pure tracing
// overhead (Hindsight MicroBricks-style). Backend containers (redis/mongo) are
// still wired but never touched (zero activity). Pick with `-w docker_<kind>_nw`.
var (
	DockerVNW    = makeVariantNW("v_es_nw", vanillaConfig, true)
	DockerPBNW   = makeVariantNW("pb_es_nw", bridgesConfig, true)
	DockerCGPBNW = makeVariantNW("cgpb_es_nw", bridgesConfig, true)
	DockerSBNW   = makeVariantNW("sb_es_nw", bridgesConfig, true)
)

func makeVariantNW(kind, configPath string, useES bool) cmdbuilder.SpecOption {
	return cmdbuilder.SpecOption{
		Name:        "docker_" + kind,
		Description: fmt.Sprintf("ZERO-WORK DSB SocialNetwork (snnw: no DB/cache/compute), bridge kind %q, config %s", kind, configPath),
		Build: func(spec wiring.WiringSpec) ([]string, error) {
			bridgeKind := strings.TrimSuffix(strings.TrimSuffix(kind, "_nw"), "_es")
			if _, set := os.LookupEnv("BLUEPRINT_BRIDGE_KIND"); !set {
				os.Setenv("BLUEPRINT_BRIDGE_KIND", bridgeKind)
			}
			return makeDockerSpecNW(spec, kind+*extraSuffix, configPath, useES)
		},
	}
}

func makeDockerSpecNW(spec wiring.WiringSpec, suffix, configPath string, useES bool) ([]string, error) {
	sn := func(name string) string { return name + "_" + suffix }

	var containers []string
	var allServices []string

	var jaeger_collector string
	if useES {
		// Single-node ES container co-located with jaeger as storage
		// backend — jaeger gets SPAN_STORAGE_TYPE=elasticsearch +
		// ES_SERVER_URLS env vars wired automatically.
		jaeger_collector = jaeger.CollectorWithElasticsearch(spec, sn("jaeger"), sn("elasticsearch"))
	} else {
		jaeger_collector = jaeger.Collector(spec, sn("jaeger"))
	}
	trace_collector := otelcol.CollectorWithConfig(
		spec, sn("otelcol"),
		jaeger_collector,
		configPath,
		otelImage,
		8080, "jaeger")

	applyDockerDefaults := func(serviceName string) string {
		retries.AddRetries(spec, serviceName, 3)
		opentelemetry.Instrument(spec, serviceName, trace_collector)
		grpc.Deploy(spec, serviceName)
		goproc.Deploy(spec, serviceName)
		return linuxcontainer.Deploy(spec, serviceName)
	}

	applyHTTPDefaults := func(serviceName string, collector string) string {
		retries.AddRetries(spec, serviceName, 3)
		clientpool.Create(spec, serviceName, 100)
		opentelemetry.Instrument(spec, serviceName, collector)
		http.Deploy(spec, serviceName)
		goproc.Deploy(spec, serviceName)
		return linuxcontainer.Deploy(spec, serviceName)
	}

	// Backends
	user_cache := redis.Container(spec, sn("user_cache"))
	user_db := mongodb.Container(spec, sn("user_db"))
	post_cache := redis.Container(spec, sn("post_cache"))
	post_db := mongodb.Container(spec, sn("post_db"))
	social_cache := redis.Container(spec, sn("social_cache"))
	social_db := mongodb.Container(spec, sn("social_db"))
	urlshorten_db := mongodb.Container(spec, sn("urlshorten_db"))
	usertimeline_cache := redis.Container(spec, sn("usertimeline_cache"))
	usertimeline_db := mongodb.Container(spec, sn("usertimeline_db"))
	hometimeline_cache := redis.Container(spec, sn("hometimeline_cache"))

	allServices = append(allServices,
		user_cache, user_db,
		post_cache, post_db,
		social_cache, social_db,
		usertimeline_cache, usertimeline_db,
		hometimeline_cache,
	)

	urlshorten_service := workflow.Service[snnw.UrlShortenService](spec, sn("urlshorten_service"), urlshorten_db)
	containers = append(containers, applyDockerDefaults(urlshorten_service))
	allServices = append(allServices, urlshorten_service)

	usermention_service := workflow.Service[snnw.UserMentionService](spec, sn("usermention_service"), user_cache, user_db)
	containers = append(containers, applyDockerDefaults(usermention_service))
	allServices = append(allServices, usermention_service)

	post_storage_service := workflow.Service[snnw.PostStorageService](spec, sn("post_storage_service"), post_cache, post_db)
	containers = append(containers, applyDockerDefaults(post_storage_service))
	allServices = append(allServices, post_storage_service)

	media_service := workflow.Service[snnw.MediaService](spec, sn("media_service"))
	containers = append(containers, applyDockerDefaults(media_service))
	allServices = append(allServices, media_service)

	uniqueId_service := workflow.Service[snnw.UniqueIdService](spec, sn("uniqueid_service"))
	containers = append(containers, applyDockerDefaults(uniqueId_service))
	allServices = append(allServices, uniqueId_service)

	userid_service := workflow.Service[snnw.UserIDService](spec, sn("userid_service"), user_cache, user_db)
	containers = append(containers, applyDockerDefaults(userid_service))
	allServices = append(allServices, userid_service)

	socialgraph_service := workflow.Service[snnw.SocialGraphService](spec, sn("socialgraph_service"), social_cache, social_db, userid_service)
	containers = append(containers, applyDockerDefaults(socialgraph_service))
	allServices = append(allServices, socialgraph_service)

	hometimeline_service := workflow.Service[snnw.HomeTimelineService](spec, sn("hometimeline_service"), hometimeline_cache, post_storage_service, socialgraph_service)
	containers = append(containers, applyDockerDefaults(hometimeline_service))
	allServices = append(allServices, hometimeline_service)

	user_service := workflow.Service[snnw.UserService](spec, sn("user_service"), user_cache, user_db, socialgraph_service, "secret")
	containers = append(containers, applyDockerDefaults(user_service))
	allServices = append(allServices, user_service)

	text_service := workflow.Service[snnw.TextService](spec, sn("text_service"), urlshorten_service, usermention_service)
	containers = append(containers, applyDockerDefaults(text_service))
	allServices = append(allServices, text_service)

	usertimeline_service := workflow.Service[snnw.UserTimelineService](spec, sn("usertimeline_service"), usertimeline_cache, usertimeline_db, post_storage_service)
	containers = append(containers, applyDockerDefaults(usertimeline_service))
	allServices = append(allServices, usertimeline_service)

	composepost_service := workflow.Service[snnw.ComposePostService](spec, sn("composepost_service"), post_storage_service, usertimeline_service, user_service, uniqueId_service, media_service, text_service, hometimeline_service)
	containers = append(containers, applyDockerDefaults(composepost_service))
	allServices = append(allServices, composepost_service)

	wrk2api_service := workflow.Service[snnw.Wrk2APIService](spec, sn("wrk2api_service"), user_service, composepost_service, usertimeline_service, hometimeline_service, socialgraph_service)
	containers = append(containers, applyHTTPDefaults(wrk2api_service, trace_collector))
	allServices = append(allServices, wrk2api_service)

	// Synthetic span-pressure pump. Same HTTP-deploy pattern as wrk2api so
	// an external wrk can pace request rate. Each request emits N forced-LP
	// child spans via the TracePressureService implementation. Becomes a
	// DaemonSet with non-local traffic policy at d2k8s time.
	tracepressure_service := workflow.Service[snnw.TracePressureService](spec, sn("tracepressure_service"))
	containers = append(containers, applyHTTPDefaults(tracepressure_service, trace_collector))
	allServices = append(allServices, tracepressure_service)

	tests := gotests.Test(spec, allServices...)
	containers = append(containers, tests, sn("otelcol"), sn("jaeger"))
	if useES {
		// ES has no pointer wrapper (jaeger talks to it over plain
		// TCP), so we add the .ctr IR node name directly.
		containers = append(containers, sn("elasticsearch")+".ctr")
	}

	return containers, nil
}
