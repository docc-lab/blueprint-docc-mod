package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Tomislav-RetCtx: rate is aggregate across all endpoints and workers in this
// process. Paper inputs that were not recovered are explicit options.
type config struct {
	Endpoints          []string      `json:"endpoints"`
	Protocol           string        `json:"protocol"`
	Insecure           bool          `json:"insecure"`
	CAFile             string        `json:"ca_file,omitempty"`
	Rates              []float64     `json:"rates_spans_per_second"`
	Duration           time.Duration `json:"duration_ns"`
	Warmup             time.Duration `json:"warmup_ns"`
	Settle             time.Duration `json:"settle_ns"`
	Spans              uint64        `json:"span_limit_per_phase"`
	Workers            int           `json:"workers_per_endpoint"`
	Queue              int           `json:"queue_batches_per_endpoint"`
	Batch              int           `json:"max_batch_spans"`
	Timeout            time.Duration `json:"rpc_timeout_ns"`
	Drain              time.Duration `json:"drain_timeout_ns"`
	Report             time.Duration `json:"report_interval_ns"`
	Profile            string        `json:"profile"`
	Attributes         string        `json:"attributes_file,omitempty"`
	ResourceAttributes string        `json:"resource_attributes_file,omitempty"`
	Bridge             string        `json:"bridge"`
	CPD                int           `json:"cpd"`
	CheckpointFraction *float64      `json:"checkpoint_fraction,omitempty"`
	BridgeBytes        int           `json:"checkpoint_value_bytes"`
	SizesFile          string        `json:"sizes_file,omitempty"`
	BridgeDistribution string        `json:"bridge_distribution_file,omitempty"`
	Depth              uint64        `json:"depth"`
	Ordinal            uint64        `json:"ordinal"`
	Seed               uint64        `json:"payload_seed"`
	SpanName           string        `json:"span_name"`
	Out                string        `json:"output_directory,omitempty"`
	Metrics            []string      `json:"metrics_urls,omitempty"`
	AllowErrors        bool          `json:"allow_errors"`
	DryRun             bool          `json:"dry_run"`
}

type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

func parseConfig(args []string, output io.Writer) (config, error) {
	c := config{}
	f := flag.NewFlagSet("spanload", flag.ContinueOnError)
	f.SetOutput(output)
	var endpoints, metrics stringsFlag
	var rates string
	f.Var(&endpoints, "endpoint", "OTLP target; repeat to distribute load (default 127.0.0.1:4317 for grpc, http://127.0.0.1:4318/v1/traces for http)")
	f.StringVar(&c.Protocol, "protocol", "grpc", "OTLP transport: grpc or http (binary protobuf)")
	f.BoolVar(&c.Insecure, "insecure", false, "Use plaintext gRPC (HTTP uses the URL scheme)")
	f.StringVar(&c.CAFile, "ca-file", "", "Additional PEM CA certificates for TLS")
	f.StringVar(&rates, "rates", "10000", "Comma-separated aggregate spans/s for successive steps; 0 means unpaced")
	f.DurationVar(&c.Duration, "duration", 30*time.Second, "Generation duration per phase")
	f.DurationVar(&c.Warmup, "warmup", 0, "Warmup duration before EACH measured rate (separate counters)")
	f.DurationVar(&c.Settle, "settle", 0, "Wait after draining before the final collector metrics snapshot")
	f.Uint64Var(&c.Spans, "spans", 0, "Optional scheduled span limit per phase (0: duration only)")
	f.IntVar(&c.Workers, "workers", 4, "Concurrent RPC workers per endpoint; does not multiply --rates")
	f.IntVar(&c.Queue, "queue", 8, "Bounded pending batches per endpoint in paced mode")
	f.IntVar(&c.Batch, "batch-size", 512, "Maximum spans per export request")
	f.DurationVar(&c.Timeout, "timeout", 5*time.Second, "Timeout per export RPC")
	f.DurationVar(&c.Drain, "drain-timeout", 10*time.Second, "Maximum time to finish pending work after generation stops")
	f.DurationVar(&c.Report, "report-interval", 5*time.Second, "JSON progress interval (0: phase summaries only)")
	f.StringVar(&c.Profile, "profile", "zero", "Baseline span attributes: zero, semconv10-example, or custom")
	f.StringVar(&c.Attributes, "attributes-file", "", "Typed JSON attribute array; requires --profile custom")
	f.StringVar(&c.ResourceAttributes, "resource-attributes-file", "", "Optional typed JSON resource attributes (default none)")
	f.StringVar(&c.Bridge, "bridge", "none", "Synthetic metadata: none, pb, cgpb, or sb")
	f.IntVar(&c.CPD, "cpd", 6, "One checkpoint per N generated span positions (1..256)")
	f.Func("checkpoint-fraction", "Expected fraction of spans carrying _br (0..1); seeded independent draws, alternative to --cpd", func(value string) error {
		fraction, err := strconv.ParseFloat(value, 64)
		if err == nil {
			c.CheckpointFraction = &fraction
		}
		return err
	})
	f.IntVar(&c.BridgeBytes, "bridge-bytes", -1, "Fixed _br byte-value size; alternative to --sizes-file or --bridge-distribution")
	f.StringVar(&c.SizesFile, "sizes-file", "", "JSON {source: ..., sizes: {pb: {2: N, ...}, ...}}; entries are byte counts or distribution objects")
	f.StringVar(&c.BridgeDistribution, "bridge-distribution", "", "JSON checkpoint size distribution: discrete weights, empirical samples, or inclusive uniform bounds")
	f.Uint64Var(&c.Depth, "depth", 6, "Synthetic absolute depth encoded as a uvarint in _d/_o")
	f.Uint64Var(&c.Ordinal, "ordinal", 1, "Synthetic sibling ordinal encoded before depth in SB _o")
	f.Uint64Var(&c.Seed, "payload-seed", 1, "Seed for reproducible checkpoint sizes and opaque payload bytes; IDs remain unique per run")
	f.StringVar(&c.SpanName, "span-name", "", "Optional operation name; omitted by default")
	f.StringVar(&c.Out, "out", "", "Create a NEW directory for manifest, JSONL, and raw metrics (JSONL also goes to stdout)")
	f.Var(&metrics, "metrics-url", "Collector Prometheus URL; repeat once per endpoint in the same order; requires --out")
	f.BoolVar(&c.AllowErrors, "allow-errors", false, "Exit successfully despite reported RPC errors/rejections, unsent spans, or scrape errors")
	f.BoolVar(&c.DryRun, "dry-run", false, "Validate and print manifest/sample protobuf JSON without opening a network connection")
	f.Usage = func() {
		fmt.Fprintln(output, "spanload: direct OTLP collector load generator (Tomislav-RetCtx)")
		fmt.Fprintln(output, "Usage: spanload --endpoint HOST:4317 --insecure --rates 10000,50000 [options]")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		return c, err
	}
	if len(f.Args()) != 0 {
		return c, fmt.Errorf("unexpected positional arguments: %v", f.Args())
	}
	c.Endpoints, c.Metrics = endpoints, metrics
	if c.Protocol != "grpc" && c.Protocol != "http" {
		return c, errors.New("--protocol must be grpc or http")
	}
	if len(c.Endpoints) == 0 {
		if c.Protocol == "grpc" {
			c.Endpoints = []string{"127.0.0.1:4317"}
		} else {
			c.Endpoints = []string{"http://127.0.0.1:4318/v1/traces"}
		}
	}
	seen := map[string]bool{}
	for _, ep := range c.Endpoints {
		if seen[ep] {
			return c, fmt.Errorf("duplicate endpoint %q", ep)
		}
		seen[ep] = true
		if c.Protocol == "grpc" {
			if _, _, err := net.SplitHostPort(ep); err != nil {
				return c, fmt.Errorf("gRPC endpoint must be host:port: %q", ep)
			}
		} else if err := validateURL(ep); err != nil {
			return c, err
		}
	}
	if c.Protocol == "http" && c.Insecure {
		return c, errors.New("HTTP transport uses http:// or https://; omit --insecure")
	}
	if c.Insecure && c.CAFile != "" {
		return c, errors.New("--ca-file cannot be used with plaintext gRPC")
	}
	for _, value := range strings.Split(rates, ",") {
		r, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(r) || math.IsInf(r, 0) || r < 0 || r > 1e9 {
			return c, fmt.Errorf("invalid aggregate span rate %q (range 0..1e9)", value)
		}
		c.Rates = append(c.Rates, r)
	}
	if c.Duration <= 0 || c.Timeout <= 0 || c.Drain <= 0 || c.Warmup < 0 || c.Settle < 0 || c.Report < 0 {
		return c, errors.New("duration, timeout and drain-timeout must be positive; warmup, settle and report-interval must be nonnegative")
	}
	for _, d := range []time.Duration{c.Duration, c.Warmup} {
		for _, r := range c.Rates {
			if r*d.Seconds() >= float64(math.MaxUint64) {
				return c, errors.New("rate times duration exceeds the span counter range")
			}
		}
	}
	if c.Workers < 1 || c.Workers > 1024 || c.Queue < 0 || c.Queue > 65536 || c.Batch < 1 || c.Batch > 65536 {
		return c, errors.New("workers must be 1..1024, queue 0..65536, batch-size 1..65536")
	}
	if c.CPD < 1 || c.CPD > 256 {
		return c, errors.New("--cpd must be in 1..256")
	}
	if c.CheckpointFraction != nil {
		fraction := *c.CheckpointFraction
		if math.IsNaN(fraction) || math.IsInf(fraction, 0) || fraction < 0 || fraction > 1 || c.Bridge == "none" {
			return c, errors.New("--checkpoint-fraction requires a bridge and a finite fraction in 0..1")
		}
		cpdSet := false
		f.Visit(func(option *flag.Flag) { cpdSet = cpdSet || option.Name == "cpd" })
		if cpdSet {
			return c, errors.New("use one of --cpd or --checkpoint-fraction")
		}
	}
	if c.Bridge != "none" && c.Bridge != "pb" && c.Bridge != "cgpb" && c.Bridge != "sb" {
		return c, errors.New("--bridge must be none, pb, cgpb, or sb")
	}
	if c.BridgeBytes < -1 || c.BridgeBytes > 1<<20 {
		return c, errors.New("--bridge-bytes must be 0..1048576")
	}
	if c.Bridge == "none" && (c.BridgeBytes != -1 || c.SizesFile != "" || c.BridgeDistribution != "") {
		return c, errors.New("bridge size options require --bridge pb, cgpb, or sb")
	}
	sizeOptions := 0
	if c.BridgeBytes != -1 {
		sizeOptions++
	}
	if c.SizesFile != "" {
		sizeOptions++
	}
	if c.BridgeDistribution != "" {
		sizeOptions++
	}
	if sizeOptions > 1 {
		return c, errors.New("use one of --bridge-bytes, --sizes-file, or --bridge-distribution")
	}
	if c.Profile != "zero" && c.Profile != "semconv10-example" && c.Profile != "custom" {
		return c, errors.New("--profile must be zero, semconv10-example, or custom")
	}
	if (c.Profile == "custom") != (c.Attributes != "") {
		return c, errors.New("--profile custom and --attributes-file must be used together")
	}
	if len(c.Metrics) != 0 && (len(c.Metrics) != len(c.Endpoints) || c.Out == "") {
		return c, errors.New("provide one --metrics-url per endpoint and an --out directory")
	}
	for _, u := range c.Metrics {
		if err := validateURL(u); err != nil {
			return c, err
		}
	}
	return c, nil
}

func validateURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("expected an http(s) URL without userinfo or fragment: %q", value)
	}
	return nil
}
