// fakecollector stands in for the OpenTelemetry collector when running Blueprint services locally.
//
// It does two things:
//
//  1. Receives OTLP/gRPC trace exports (what the services' span processors send) and writes every
//     span, one JSON object per line, to the -out file.  Nothing is dropped or sampled.
//  2. Serves GET /getFullConfig, the endpoint the bridge processors (pb, cgpb, sb) call at startup
//     to learn their checkpoint policy.  It answers {"config": <the JSON given in -config>}, the shape
//     the processors expect (runtime/plugins/otelcol/defs.go, ConfigResponse).
//
// -otlp takes a comma-separated list of listen addresses, because vanilla exports to the collector
// address it was given, while the bridge processors always export to port 4317 of that host (the
// agent's OTLP port on Kubernetes).
//
// Usage:
//
//	go run . -otlp :12354,:4317 -config-addr :8080 -out spans.jsonl \
//	    -config '{"cpd_min":2,"cpd_max":6,"reverse_policy":"inverse_depth"}'
//
// Each output line looks like:
//
//	{"service":"text_service_v_es_nw_proc","scope":"TextService","trace_id":"…","span_id":"…",
//	 "parent_span_id":"…","name":"TextServiceServer_ComposeText","kind":"SERVER",
//	 "start_ns":…,"end_ns":…,"attributes":{"childCount":"int:2",…},"events":[…],
//	 "status":"UNSET","priority":"hp"}
//
// Attribute values are written as "<type>:<value>" so that a string "2" and an int 2 do not compare
// equal by accident.  "priority" is the bridges-priority gRPC header of the export call, if any.
//
// With -discard it only counts the spans it receives (latency runs: decoding is unavoidable, but
// turning every span into JSON would take CPU away from the services under test).
//
// The process exits cleanly on SIGINT/SIGTERM after flushing the output file.
package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// One recorded span, flattened for easy comparison.
type record struct {
	Service      string            `json:"service"`
	Scope        string            `json:"scope"`
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	StartNs      uint64            `json:"start_ns"`
	EndNs        uint64            `json:"end_ns"`
	Attributes   map[string]string `json:"attributes"`
	Events       []event           `json:"events,omitempty"`
	Status       string            `json:"status"`
	Priority     string            `json:"priority,omitempty"`
}

type event struct {
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// The OTLP trace service: every Export call appends its spans to the output file.
type server struct {
	collectortrace.UnimplementedTraceServiceServer
	mu      sync.Mutex
	out     *bufio.Writer
	spans   int
	discard bool // -discard: only count spans (for latency runs, where writing JSON would cost CPU)
}

func (s *server) Export(ctx context.Context, req *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	if s.discard {
		n := 0
		for _, rs := range req.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				n += len(ss.Spans)
			}
		}
		s.mu.Lock()
		s.spans += n
		s.mu.Unlock()
		return &collectortrace.ExportTraceServiceResponse{}, nil
	}

	priority := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("bridges-priority"); len(v) > 0 {
			priority = v[0]
		}
	}

	var lines [][]byte
	for _, rs := range req.ResourceSpans {
		service := ""
		for _, kv := range rs.GetResource().GetAttributes() {
			if kv.Key == "service.name" {
				service = kv.Value.GetStringValue()
			}
		}
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				r := record{
					Service:      service,
					Scope:        ss.GetScope().GetName(),
					TraceID:      hex.EncodeToString(sp.TraceId),
					SpanID:       hex.EncodeToString(sp.SpanId),
					ParentSpanID: hex.EncodeToString(sp.ParentSpanId),
					Name:         sp.Name,
					Kind:         strings.TrimPrefix(sp.Kind.String(), "SPAN_KIND_"),
					StartNs:      sp.StartTimeUnixNano,
					EndNs:        sp.EndTimeUnixNano,
					Attributes:   attrs(sp.Attributes),
					Status:       strings.TrimPrefix(sp.GetStatus().GetCode().String(), "STATUS_CODE_"),
					Priority:     priority,
				}
				for _, ev := range sp.Events {
					r.Events = append(r.Events, event{Name: ev.Name, Attributes: attrs(ev.Attributes)})
				}
				line, err := json.Marshal(r)
				if err != nil {
					return nil, err
				}
				lines = append(lines, line)
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range lines {
		s.out.Write(line)
		s.out.WriteByte('\n')
	}
	s.spans += len(lines)
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

// Flattens OTLP attributes into "type:value" strings.
func attrs(kvs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = value(kv.Value)
	}
	return m
}

func value(v *commonpb.AnyValue) string {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return "string:" + x.StringValue
	case *commonpb.AnyValue_IntValue:
		return fmt.Sprintf("int:%d", x.IntValue)
	case *commonpb.AnyValue_BoolValue:
		return fmt.Sprintf("bool:%t", x.BoolValue)
	case *commonpb.AnyValue_DoubleValue:
		return fmt.Sprintf("double:%g", x.DoubleValue)
	case *commonpb.AnyValue_BytesValue:
		return "bytes:" + hex.EncodeToString(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		var parts []string
		for _, e := range x.ArrayValue.Values {
			parts = append(parts, value(e))
		}
		return "array:[" + strings.Join(parts, ",") + "]"
	case *commonpb.AnyValue_KvlistValue:
		var parts []string
		for _, kv := range x.KvlistValue.Values {
			parts = append(parts, kv.Key+"="+value(kv.Value))
		}
		sort.Strings(parts)
		return "kvlist:{" + strings.Join(parts, ",") + "}"
	default:
		return "empty:"
	}
}

func main() {
	otlpAddrs := flag.String("otlp", ":4317", "comma-separated listen addresses for the OTLP/gRPC trace receiver")
	configAddr := flag.String("config-addr", ":8080", "address for the /getFullConfig HTTP endpoint (empty = off)")
	config := flag.String("config", "{}", "JSON body returned by /getFullConfig")
	outPath := flag.String("out", "spans.jsonl", "file to write spans to (one JSON object per line)")
	discard := flag.Bool("discard", false, "only count spans, write nothing (the -out file stays empty)")
	flag.Parse()

	// Fail early on a malformed config rather than confusing the processors later.
	var check map[string]any
	if err := json.Unmarshal([]byte(*config), &check); err != nil {
		log.Fatalf("-config is not a JSON object: %v", err)
	}

	f, err := os.Create(*outPath)
	if err != nil {
		log.Fatal(err)
	}
	srv := &server{out: bufio.NewWriterSize(f, 1<<20), discard: *discard}

	g := grpc.NewServer(grpc.MaxRecvMsgSize(64 << 20))
	collectortrace.RegisterTraceServiceServer(g, srv)
	for _, addr := range strings.Split(*otlpAddrs, ",") {
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			if err := g.Serve(lis); err != nil {
				log.Fatal(err)
			}
		}()
	}
	log.Printf("OTLP receiver on %s, writing spans to %s", *otlpAddrs, *outPath)

	if *configAddr != "" {
		mux := http.NewServeMux()
		body := []byte(`{"config":` + *config + `}`)
		mux.HandleFunc("/getFullConfig", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
		})
		go func() {
			if err := http.ListenAndServe(*configAddr, mux); err != nil {
				log.Fatal(err)
			}
		}()
		log.Printf("config endpoint on %s/getFullConfig: %s", *configAddr, *config)
	}

	// Run until told to stop, then flush everything we received.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	g.GracefulStop()
	srv.mu.Lock()
	srv.out.Flush()
	f.Close()
	log.Printf("stopped after %d spans", srv.spans)
	srv.mu.Unlock()
}
