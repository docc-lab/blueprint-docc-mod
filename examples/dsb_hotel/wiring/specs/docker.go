package specs

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/cmdbuilder"
)

var (
	extraSuffix           = flag.String("extra", "", "Suffix appended to the variant's resource names")
	collectorImage        = flag.String("collector-image", "10.10.1.1:30000/otelcontribcol:latest", "Custom collector base image")
	customCollectorConfig = flag.String("collector-config", "", "Override the variant's collector YAML")
	rpcTimeout            = flag.String("rpc-timeout", "5s", "Positive timeout for each inter-service gRPC call")
	withWorkload          = flag.Bool("with-workload", false, "Also generate the complex workload driver (original always includes it)")

	DockerPB     = makeVariant("pb", false)
	DockerCGPB   = makeVariant("cgpb", false)
	DockerSB     = makeVariant("sb", false)
	DockerV      = makeVariant("v", false)
	DockerPBES   = makeVariant("pb", true)
	DockerCGPBES = makeVariant("cgpb", true)
	DockerSBES   = makeVariant("sb", true)
	DockerVES    = makeVariant("v", true)
	DockerRCES   = makeVariant("rc", true)
	// Tomislav-RetCtx: no-tracing baseline (vanilla collector config; collector pods idle).
	DockerNTES = makeVariantNT()
)

func makeVariantNT() cmdbuilder.SpecOption {
	return cmdbuilder.SpecOption{
		Name:        "docker_nt_es",
		Description: "Hotel Reservation with NO tracing (OTel SDK not instrumented); collector/Jaeger/ES deployed idle",
		Build: func(spec wiring.WiringSpec) ([]string, error) {
			configureRuntime("nt")
			return makeHotelSpec(spec, "hotel_nt_es"+*extraSuffix, true, *withWorkload, collectorConfig("v"), false)
		},
	}
}

func makeVariant(kind string, useES bool) cmdbuilder.SpecOption {
	variant := kind
	if useES {
		variant += "_es"
	}
	return cmdbuilder.SpecOption{
		Name:        "docker_" + variant,
		Description: fmt.Sprintf("Hotel Reservation with %s tracing through otelcol; Elasticsearch=%t", kind, useES),
		Build: func(spec wiring.WiringSpec) ([]string, error) {
			configureRuntime(kind)
			return makeHotelSpec(spec, "hotel_"+variant+*extraSuffix, useES, *withWorkload, collectorConfig(kind), true)
		},
	}
}

func configureRuntime(kind string) {
	// Match the generated wrapper and SDK processor, even if a previous build
	// left another variant in the environment.
	os.Setenv("OT_BRIDGE", kind)
	os.Setenv("BLUEPRINT_BRIDGE_KIND", kind)
	for key, value := range map[string]string{
		"BLUEPRINT_GC_INTERVAL_SEC": "0.1",
		"BLUEPRINT_GOGC":            "off",
		"BLUEPRINT_OTLP_RETRY":      "off",
	} {
		if _, set := os.LookupEnv(key); !set {
			os.Setenv(key, value)
		}
	}
}

func collectorConfig(kind string) string {
	if *customCollectorConfig != "" {
		return *customCollectorConfig
	}
	_, source, _, _ := runtime.Caller(0)
	filename := "collector-bridges.yaml"
	if kind == "v" {
		filename = "collector-vanilla.yaml"
	}
	return filepath.Join(filepath.Dir(source), filename)
}

func validateOptions() error {
	if !regexp.MustCompile(`^[a-z0-9_]*$`).MatchString(*extraSuffix) || len(*extraSuffix) > 16 {
		return fmt.Errorf("-extra must contain at most 16 lowercase letters, digits, or underscores")
	}
	if strings.TrimSpace(*collectorImage) == "" {
		return fmt.Errorf("-collector-image cannot be empty")
	}
	if duration, err := time.ParseDuration(*rpcTimeout); err != nil || duration <= 0 {
		return fmt.Errorf("-rpc-timeout must be a positive Go duration")
	}
	return nil
}
