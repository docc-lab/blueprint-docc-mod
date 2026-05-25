package specs

import (
	"flag"
	"fmt"
	"os"

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

// Per-variant defaults. Adjust here if a variant needs its own collector
// config or otelcontribcol image.
const (
	otelImage     = "10.10.1.1:30000/otelcontribcol:latest"
	bridgesConfig = "/users/tomislav/opentelemetry-collector-contrib/test-config-bridges.yaml"
	vanillaConfig = "/users/tomislav/opentelemetry-collector-contrib/config-vanilla.yaml"
)

// Spec-level Go runtime tuning baked into every Blueprint-built service
// container for this example. Both the docker-compose `environment:` block
// and the per-service run.sh `export` lines pick these up at compile time
// (via the dockercompose and goproc/linuxgen plugins respectively), and
// kompose carries them through to the k8s deployment env entries.
//
// To override from the CLI, set the env var explicitly before `go run`:
//
//	BLUEPRINT_GC_INTERVAL_SEC=0.1 BLUEPRINT_GOGC= go run ./wiring ...
//
// Set to empty string in the env to disable injection entirely.
const (
	DefaultGCIntervalSec = "0.1"
	DefaultGOGC          = "off"
)

func init() {
	// `_, ok := os.LookupEnv(...)` distinguishes "unset" (use spec default)
	// from "set to empty" (explicitly disabled by the caller).
	if _, set := os.LookupEnv("BLUEPRINT_GC_INTERVAL_SEC"); !set {
		os.Setenv("BLUEPRINT_GC_INTERVAL_SEC", DefaultGCIntervalSec)
	}
	if _, set := os.LookupEnv("BLUEPRINT_GOGC"); !set {
		os.Setenv("BLUEPRINT_GOGC", DefaultGOGC)
	}
}

// extraSuffix is an optional string appended to the bridge-kind suffix on
// every generated identifier. Useful for running multiple instances of the
// same bridge variant side-by-side (e.g. `-w docker_sb -extra 3` produces
// identifiers like `..._sb3`, matching `build_sb3/` and `node-pinning-sb3.yaml`).
// The bridge kind itself is unchanged, so BRIDGE_KIND=sb still selects the
// right runtime processor.
var extraSuffix = flag.String("extra", "",
	"Optional suffix extension appended after the bridge kind (e.g. '3' yields identifiers like '..._sb3'). "+
		"BRIDGE_KIND env-var should still be set to just the bridge kind.")

// Registered build targets — pick one with `-w docker_<suffix>`. The suffix
// also tags every generated identifier (service, container, env-var) so a
// build of `docker_sb` produces resources named `..._sb`, matching the
// `node-pinning-sb.yaml` file applied later.
//
//	docker_pb   — path-bridge variant (the previous default).
//	docker_cgpb — call-graph + path-bridge variant.
//	docker_sb   — structural-bridge variant.
//	docker_v    — vanilla (no bridge instrumentation).
//
// All bridge variants currently point at the same `test-config-bridges.yaml`
// collector pipeline (priority routing); vanilla uses `config-vanilla.yaml`.
// Note: the in-service bridge processor type is still selected at compile
// time in `runtime/plugins/otelcol/trace.go` — that's the other hand-edit
// site, not driven by this spec yet.
var (
	DockerPB   = makeVariant("pb", bridgesConfig)
	DockerCGPB = makeVariant("cgpb", bridgesConfig)
	DockerSB   = makeVariant("sb", bridgesConfig)
	DockerV    = makeVariant("v", vanillaConfig)
)

func makeVariant(kind, configPath string) cmdbuilder.SpecOption {
	return cmdbuilder.SpecOption{
		Name:        "docker_" + kind,
		Description: fmt.Sprintf("DSB SocialNetwork, bridge kind %q, collector config %s (use -extra N to tag identifiers as %s_N)", kind, configPath, kind),
		Build: func(spec wiring.WiringSpec) ([]string, error) {
			// Default BRIDGE_KIND for runtime dispatch to the kind being
			// compiled. The dockercompose plugin reads BLUEPRINT_BRIDGE_KIND
			// at instance-declaration time and bakes BRIDGE_KIND into every
			// built service's compose env (which kompose propagates to k8s).
			// LookupEnv (not Getenv) preserves an explicit empty override.
			if _, set := os.LookupEnv("BLUEPRINT_BRIDGE_KIND"); !set {
				os.Setenv("BLUEPRINT_BRIDGE_KIND", kind)
			}
			// `-extra` is read here (post flag.Parse) so each run can vary
			// the suffix without re-registering the variant.
			return makeDockerSpec(spec, kind+*extraSuffix, configPath)
		},
	}
}

// A wiring spec that deploys each service into its own Docker container
// using gRPC for inter-service comms; the Wrk2API service is the HTTP
// frontend. User / socialgraph / urlshorten / usertimeline services use
// MongoDB; user / socialgraph / urlshorten / usertimeline / hometimeline
// services use Redis caches (previously memcached — Redis supports the
// sorted-set ops needed for efficient timeline queries).
//
// `suffix` is appended to every identifier so multiple build variants can
// coexist in one cluster / build tree.
func makeDockerSpec(spec wiring.WiringSpec, suffix, configPath string) ([]string, error) {
	sn := func(name string) string { return name + "_" + suffix }

	var containers []string
	var allServices []string

	jaeger_collector := jaeger.Collector(spec, sn("jaeger"))
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

	urlshorten_service := workflow.Service[socialnetwork.UrlShortenService](spec, sn("urlshorten_service"), urlshorten_db)
	containers = append(containers, applyDockerDefaults(urlshorten_service))
	allServices = append(allServices, urlshorten_service)

	usermention_service := workflow.Service[socialnetwork.UserMentionService](spec, sn("usermention_service"), user_cache, user_db)
	containers = append(containers, applyDockerDefaults(usermention_service))
	allServices = append(allServices, usermention_service)

	post_storage_service := workflow.Service[socialnetwork.PostStorageService](spec, sn("post_storage_service"), post_cache, post_db)
	containers = append(containers, applyDockerDefaults(post_storage_service))
	allServices = append(allServices, post_storage_service)

	media_service := workflow.Service[socialnetwork.MediaService](spec, sn("media_service"))
	containers = append(containers, applyDockerDefaults(media_service))
	allServices = append(allServices, media_service)

	uniqueId_service := workflow.Service[socialnetwork.UniqueIdService](spec, sn("uniqueid_service"))
	containers = append(containers, applyDockerDefaults(uniqueId_service))
	allServices = append(allServices, uniqueId_service)

	userid_service := workflow.Service[socialnetwork.UserIDService](spec, sn("userid_service"), user_cache, user_db)
	containers = append(containers, applyDockerDefaults(userid_service))
	allServices = append(allServices, userid_service)

	socialgraph_service := workflow.Service[socialnetwork.SocialGraphService](spec, sn("socialgraph_service"), social_cache, social_db, userid_service)
	containers = append(containers, applyDockerDefaults(socialgraph_service))
	allServices = append(allServices, socialgraph_service)

	hometimeline_service := workflow.Service[socialnetwork.HomeTimelineService](spec, sn("hometimeline_service"), hometimeline_cache, post_storage_service, socialgraph_service)
	containers = append(containers, applyDockerDefaults(hometimeline_service))
	allServices = append(allServices, hometimeline_service)

	user_service := workflow.Service[socialnetwork.UserService](spec, sn("user_service"), user_cache, user_db, socialgraph_service, "secret")
	containers = append(containers, applyDockerDefaults(user_service))
	allServices = append(allServices, user_service)

	text_service := workflow.Service[socialnetwork.TextService](spec, sn("text_service"), urlshorten_service, usermention_service)
	containers = append(containers, applyDockerDefaults(text_service))
	allServices = append(allServices, text_service)

	usertimeline_service := workflow.Service[socialnetwork.UserTimelineService](spec, sn("usertimeline_service"), usertimeline_cache, usertimeline_db, post_storage_service)
	containers = append(containers, applyDockerDefaults(usertimeline_service))
	allServices = append(allServices, usertimeline_service)

	composepost_service := workflow.Service[socialnetwork.ComposePostService](spec, sn("composepost_service"), post_storage_service, usertimeline_service, user_service, uniqueId_service, media_service, text_service, hometimeline_service)
	containers = append(containers, applyDockerDefaults(composepost_service))
	allServices = append(allServices, composepost_service)

	wrk2api_service := workflow.Service[socialnetwork.Wrk2APIService](spec, sn("wrk2api_service"), user_service, composepost_service, usertimeline_service, hometimeline_service, socialgraph_service)
	containers = append(containers, applyHTTPDefaults(wrk2api_service, trace_collector))
	allServices = append(allServices, wrk2api_service)

	tests := gotests.Test(spec, allServices...)
	containers = append(containers, tests, sn("otelcol"), sn("jaeger"))

	return containers, nil
}
