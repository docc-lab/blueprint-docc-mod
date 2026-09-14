package specs

import (
	"fmt"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/examples/dsb_hotel/cmplx_workload/workloadgen"
	"github.com/blueprint-uservices/blueprint/examples/dsb_hotel/workflow/hotelreservation"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
	"github.com/blueprint-uservices/blueprint/plugins/goproc"
	"github.com/blueprint-uservices/blueprint/plugins/gotests"
	"github.com/blueprint-uservices/blueprint/plugins/grpc"
	"github.com/blueprint-uservices/blueprint/plugins/http"
	"github.com/blueprint-uservices/blueprint/plugins/jaeger"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer"
	"github.com/blueprint-uservices/blueprint/plugins/memcached"
	"github.com/blueprint-uservices/blueprint/plugins/mongodb"
	"github.com/blueprint-uservices/blueprint/plugins/opentelemetry"
	"github.com/blueprint-uservices/blueprint/plugins/otelcol"
	"github.com/blueprint-uservices/blueprint/plugins/workflow"
	"github.com/blueprint-uservices/blueprint/plugins/workload"
)

// Wiring spec that represents the original configuration of the HotelReservation application.
// Each service is deployed in a separate container with all inter-service communication happening via GRPC.
// FrontEnd service provides a http frontend for making requests.
// Tomislav-RetCtx: use the same custom collector/config pipeline as Social Network.
var Original = cmdbuilder.SpecOption{
	Name:        "original",
	Description: "Deploys the original configuration of the DeathStarBench application.",
	Build:       makeOriginalSpec,
}

func makeOriginalSpec(spec wiring.WiringSpec) ([]string, error) {
	configureRuntime("pb")
	return makeHotelSpec(spec, "", false, true, collectorConfig("pb"))
}

func makeHotelSpec(spec wiring.WiringSpec, suffix string, useES, includeWorkload bool, configPath string) ([]string, error) {
	if err := validateOptions(); err != nil {
		return nil, err
	}
	sn := func(name string) string {
		if suffix == "" {
			return name
		}
		return name + "_" + suffix
	}
	var cntrs []string

	var allServices []string
	// Define backends
	var jaegerCollector string
	if useES {
		jaegerCollector = jaeger.CollectorWithElasticsearch(spec, sn("jaeger"), sn("elasticsearch"))
		cntrs = append(cntrs, sn("elasticsearch")+".ctr")
	} else {
		jaegerCollector = jaeger.Collector(spec, sn("jaeger"))
	}
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

	// Define internal services
	user_service := workflow.Service[hotelreservation.UserService](spec, sn("user_service"), user_db)
	user_ctr := applyDefaults(spec, user_service, trace_collector)
	cntrs = append(cntrs, user_ctr)
	allServices = append(allServices, user_service)

	recomd_service := workflow.Service[hotelreservation.RecommendationService](spec, sn("recomd_service"), recommendations_db)
	recomd_ctr := applyDefaults(spec, recomd_service, trace_collector)
	cntrs = append(cntrs, recomd_ctr)
	allServices = append(allServices, recomd_service)

	reserv_service := workflow.Service[hotelreservation.ReservationService](spec, sn("reserv_service"), reserv_cache, reserv_db)
	reserv_ctr := applyDefaults(spec, reserv_service, trace_collector)
	cntrs = append(cntrs, reserv_ctr)
	allServices = append(allServices, reserv_service)

	geo_service := workflow.Service[hotelreservation.GeoService](spec, sn("geo_service"), geo_db)
	geo_ctr := applyDefaults(spec, geo_service, trace_collector)
	cntrs = append(cntrs, geo_ctr)
	allServices = append(allServices, geo_service)

	rate_service := workflow.Service[hotelreservation.RateService](spec, sn("rate_service"), rate_cache, rate_db)
	rate_ctr := applyDefaults(spec, rate_service, trace_collector)
	cntrs = append(cntrs, rate_ctr)
	allServices = append(allServices, rate_service)

	profile_service := workflow.Service[hotelreservation.ProfileService](spec, sn("profile_service"), profile_cache, profile_db)
	profile_ctr := applyDefaults(spec, profile_service, trace_collector)
	cntrs = append(cntrs, profile_ctr)
	allServices = append(allServices, profile_service)

	search_service := workflow.Service[hotelreservation.SearchService](spec, sn("search_service"), geo_service, rate_service)
	search_ctr := applyDefaults(spec, search_service, trace_collector)
	cntrs = append(cntrs, search_ctr)
	allServices = append(allServices, search_service)

	// Define frontend service
	frontend_service := workflow.Service[hotelreservation.FrontEndService](spec, sn("frontend_service"), search_service, profile_service, recomd_service, user_service, reserv_service)
	frontend_ctr := applyHTTPDefaults(spec, frontend_service, trace_collector)
	cntrs = append(cntrs, frontend_ctr)
	allServices = append(allServices, frontend_service)

	if includeWorkload {
		wlgen := workload.Generator[workloadgen.ComplexWorkload](spec, sn("wlgen"), frontend_service)
		cntrs = append(cntrs, wlgen)
	}

	tests := gotests.Test(spec, allServices...)
	cntrs = append(cntrs, tests)

	return cntrs, nil
}

func applyDefaults(spec wiring.WiringSpec, serviceName string, collectorName string) string {
	procName := fmt.Sprintf("%s_proc", serviceName)
	ctrName := fmt.Sprintf("%s_ctr", serviceName)
	opentelemetry.Instrument(spec, serviceName, collectorName)
	grpc.DeployWithTimeout(spec, serviceName, *rpcTimeout)
	goproc.CreateProcess(spec, procName, serviceName)
	return linuxcontainer.CreateContainer(spec, ctrName, procName)
}

func applyHTTPDefaults(spec wiring.WiringSpec, serviceName string, collectorName string) string {
	procName := fmt.Sprintf("%s_proc", serviceName)
	ctrName := fmt.Sprintf("%s_ctr", serviceName)
	opentelemetry.Instrument(spec, serviceName, collectorName)
	http.Deploy(spec, serviceName)
	goproc.CreateProcess(spec, procName, serviceName)
	return linuxcontainer.CreateContainer(spec, ctrName, procName)
}
