package specs

// Tomislav-RetCtx: ZERO-WORK Hotel Reservation (user 2026-09-24: no-work hotel sweep). Identical
// topology, backends (wired, never touched), gRPC/HTTP deployment, instrumentation and Go runtime to
// makeHotelSpec, but the services come from workflow/hotelnw, which keeps every interface and
// inter-service call and removes database, cache and compute work. Pick with -s docker_<kind>_es_nw.
// Same construction as examples/dsb_sn/wiring/specs/docker_nw.go. No workload generator or gotests
// (both are written against the hotelreservation package).

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/dsb_hotel/workflow/hotelnw"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/plugins/memcached"
	"github.com/blueprint-uservices/blueprint/plugins/mongodb"
	"github.com/blueprint-uservices/blueprint/plugins/otelcol"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
)

var (
	DockerVESNW    = makeVariantNW("v", true)
	DockerPBESNW   = makeVariantNW("pb", true)
	DockerCGPBESNW = makeVariantNW("cgpb", true)
	DockerSBESNW   = makeVariantNW("sb", true)
	DockerNTESNW   = makeVariantNW("nt", false)
)

func makeVariantNW(kind string, instrument bool) cmdbuilder.SpecOption {
	return cmdbuilder.SpecOption{
		Name:        "docker_" + kind + "_es_nw",
		Description: fmt.Sprintf("ZERO-WORK Hotel Reservation (hotelnw: no DB/cache/compute), %s; collector/Jaeger/ES deployed", kind),
		Build: func(spec wiring.WiringSpec) ([]string, error) {
			configureRuntime(kind)
			config := collectorConfig(kind)
			if kind == "nt" {
				config = collectorConfig("v")
			}
			return makeHotelNWSpec(spec, "hotel_"+kind+"_es_nw"+*extraSuffix, config, instrument)
		},
	}
}

func makeHotelNWSpec(spec wiring.WiringSpec, suffix string, configPath string, instrument bool) ([]string, error) {
	if err := validateOptions(); err != nil {
		return nil, err
	}
	sn := func(name string) string { return name + "_" + suffix }
	var cntrs []string

	jaegerCollector := jaeger.CollectorWithElasticsearch(spec, sn("jaeger"), sn("elasticsearch"))
	cntrs = append(cntrs, sn("elasticsearch")+".ctr")
	trace_collector := otelcol.CollectorWithConfig(spec, sn("otelcol"), jaegerCollector, configPath, *collectorImage, 8080, "jaeger")
	cntrs = append(cntrs, trace_collector, jaegerCollector)
	user_db := mongodb.Container(spec, sn("user_db"))
	recommendations_db := mongodb.Container(spec, sn("recomd_db"))
	reserv_db := mongodb.Container(spec, sn("reserv_db"))
	geo_db := mongodb.Container(spec, sn("geo_db"))
	rate_db := mongodb.Container(spec, sn("rate_db"))
	profile_db := mongodb.Container(spec, sn("profile_db"))

	reserv_cache := memcached.Container(spec, sn("reserv_cache"))
	rate_cache := memcached.Container(spec, sn("rate_cache"))
	profile_cache := memcached.Container(spec, sn("profile_cache"))

	user_service := workflow.Service[hotelnw.UserService](spec, sn("user_service"), user_db)
	cntrs = append(cntrs, applyDefaults(spec, user_service, trace_collector, instrument))

	recomd_service := workflow.Service[hotelnw.RecommendationService](spec, sn("recomd_service"), recommendations_db)
	cntrs = append(cntrs, applyDefaults(spec, recomd_service, trace_collector, instrument))

	reserv_service := workflow.Service[hotelnw.ReservationService](spec, sn("reserv_service"), reserv_cache, reserv_db)
	cntrs = append(cntrs, applyDefaults(spec, reserv_service, trace_collector, instrument))

	geo_service := workflow.Service[hotelnw.GeoService](spec, sn("geo_service"), geo_db)
	cntrs = append(cntrs, applyDefaults(spec, geo_service, trace_collector, instrument))

	rate_service := workflow.Service[hotelnw.RateService](spec, sn("rate_service"), rate_cache, rate_db)
	cntrs = append(cntrs, applyDefaults(spec, rate_service, trace_collector, instrument))

	profile_service := workflow.Service[hotelnw.ProfileService](spec, sn("profile_service"), profile_cache, profile_db)
	cntrs = append(cntrs, applyDefaults(spec, profile_service, trace_collector, instrument))

	search_service := workflow.Service[hotelnw.SearchService](spec, sn("search_service"), geo_service, rate_service)
	cntrs = append(cntrs, applyDefaults(spec, search_service, trace_collector, instrument))

	frontend_service := workflow.Service[hotelnw.FrontEndService](spec, sn("frontend_service"), search_service, profile_service, recomd_service, user_service, reserv_service)
	cntrs = append(cntrs, applyHTTPDefaults(spec, frontend_service, trace_collector, instrument))

	return cntrs, nil
}
