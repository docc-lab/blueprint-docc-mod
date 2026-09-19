package otelcol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func ttlTestProvider(t *testing.T, kind string, r checkpointRange) (trace.TracerProvider, func() (hp, lp []sbBufEntry)) {
	t.Helper()
	oldM, oldK := BloomFilterM, BloomFilterK
	t.Cleanup(func() { BloomFilterM, BloomFilterK = oldM, oldK; backend.SetRTBloomParams(oldM, oldK) })
	setBloomForCPD(r.max)
	var processor sdktrace.SpanProcessor
	var snapshot func() ([]sbBufEntry, []sbBufEntry)
	switch kind {
	case "pb":
		p := &PathBridgeProcessor{checkpointDistance: int64(r.max), checkpointRange: r}
		processor, snapshot = p, func() ([]sbBufEntry, []sbBufEntry) { return p.hpBuf, p.lpBuf }
	case "cgpb":
		p := &CallGraphBridgeProcessor{checkpointDistance: int64(r.max), checkpointRange: r}
		processor, snapshot = p, func() ([]sbBufEntry, []sbBufEntry) { return p.hpBuf, p.lpBuf }
	case "sb":
		p := &StructuralBridgeProcessor{checkpointDistance: int64(r.max), checkpointRange: r}
		processor, snapshot = p, func() ([]sbBufEntry, []sbBufEntry) { return p.hpBuf, p.lpBuf }
	default:
		t.Fatal(kind)
	}
	processor = wrapReverseCheckpointProcessor(processor)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	if preparer, ok := processor.(backend.CheckpointPreparer); ok {
		return &checkpointTracerProvider{tp, preparer}, snapshot
	}
	return tp, snapshot
}

// Copy the carrier through JSON, as the instrumentation wrappers do across RPCs.
func ttlChildContext(t *testing.T, span trace.Span) context.Context {
	t.Helper()
	bag := map[string]string{BaggageBRKey: checkpointSpanAttribute(span, AttrBR)}
	if depth := checkpointSpanAttribute(span, reverseDepthAttribute); depth != "" {
		bag[reverseDepthBaggageKey] = depth
	}
	wire, err := json.Marshal(bag)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), span.SpanContext())
	return backend.SetBaggageInContext(ctx, decoded)
}

func TestCheckpointTTLPaths(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		for _, r := range []checkpointRange{{1, 1}, {2, 2}, {256, 256}, {2, 6}} {
			t.Run(fmt.Sprintf("%s/%d-%d", kind, r.min, r.max), func(t *testing.T) {
				tp, snapshot := ttlTestProvider(t, kind, r)
				ctx := context.Background()
				var incoming byte
				checkpointDepth := 0
				var windowIDs []trace.SpanID
				var anchor trace.SpanID
				var windowDistance int
				for depth := 0; depth <= 2*r.max+2; depth++ {
					_, span := tp.Tracer("ttl").Start(ctx, "span", trace.WithSpanKind(trace.SpanKindServer))
					// Avoid the independent leaf-checkpoint override masking errors.
					span.SetAttributes(attribute.Bool("hasChildren", true))
					raw, ok := decodeBR(checkpointSpanAttribute(span, AttrBR))
					if !ok || len(raw) < 2 {
						t.Fatal("missing TTL-prefixed baggage")
					}
					checkpoint := depth == checkpointDepth
					if checkpoint {
						if int(raw[0])+1 < r.min || int(raw[0])+1 > r.max {
							t.Fatalf("TTL %d outside range", raw[0])
						}
						checkpointDepth = depth + int(raw[0]) + 1
					} else if incoming == 0 || raw[0] != incoming-1 {
						t.Fatalf("depth %d: incoming=%d outgoing=%d", depth, incoming, raw[0])
					}
					{ // Tomislav-RetCtx: the SB payload shares the ranged window core prefix.
						_, nextAnchor, nextDistance, nextBloom, _, valid := unpackCheckpointWindowBR(raw[1:])
						if !valid {
							t.Fatal("missing outgoing window geometry")
						}
						if checkpoint {
							if nextDistance != int(raw[0])+1 || nextAnchor != [8]byte(span.SpanContext().SpanID()) {
								t.Fatal("new window does not match the selected countdown")
							}
							if !bytes.Equal(nextBloom, make([]byte, len(nextBloom))) {
								t.Fatal("checkpoint did not reset the outgoing Bloom")
							}
						} else if nextDistance != windowDistance {
							t.Fatal("ordinary span resized its inherited window")
						}
						if depth == 0 {
							windowDistance = nextDistance
						}
						emit, _ := decodeBR(checkpointSpanAttribute(span, AttrBREmit))
						d, previous, distance, bb, _, valid := unpackCheckpointWindowBR(emit)
						if !valid || d != depth || previous != [8]byte(anchor) || distance != windowDistance {
							t.Fatalf("depth/anchor corrupted at %d", depth)
						}
						m, k := bloom.EstimateParameters(uint(maxInt(distance-1, 1)), DefaultBloomFPRate)
						if len(bb) != int((m+7)/8) {
							t.Fatalf("Bloom is not sized for selected distance %d: %d bytes", distance, len(bb))
						}
						filter := bloom.NewFromBytes(bb, m, k)
						for _, id := range windowIDs {
							if !filter.TestPrehashed(id[:]) {
								t.Fatalf("window lost ancestor at depth %d", depth)
							}
						}
						if checkpoint {
							windowDistance = nextDistance
						}
					}
					if checkpoint {
						anchor = span.SpanContext().SpanID()
						windowIDs = nil
					} else {
						windowIDs = append(windowIDs, span.SpanContext().SpanID())
					}
					ctx = ttlChildContext(t, span)
					hp, lp := snapshot()
					if len(hp)+len(lp) != depth {
						t.Fatal("OnStart exported an unfinished span")
					}
					oldHP := len(hp)
					span.End()
					hp, lp = snapshot()
					if len(hp)+len(lp) != depth+1 || (len(hp) > oldHP) != checkpoint {
						t.Fatalf("wrong OnEnd checkpoint at depth %d", depth)
					}
					var exported *tracepb.Span
					if checkpoint {
						exported = hp[len(hp)-1].span
					} else {
						exported = lp[len(lp)-1].span
					}
					for _, attr := range exported.Attributes {
						if strings.HasPrefix(attr.Key, "__bag.") {
							t.Fatalf("internal TTL carrier leaked: %s", attr.Key)
						}
						if attr.Key == AttrBREmit {
							emit, _ := decodeBR(checkpointSpanAttribute(span, AttrBREmit))
							if !checkpoint || !bytes.Equal(attr.Value.GetBytesValue(), emit) {
								t.Fatal("export truss changed or emitted early")
							}
						}
					}
					incoming = raw[0]
				}
			})
		}
	}
}

func TestCheckpointTTLConcurrentFanout(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			tp, snapshot := ttlTestProvider(t, kind, checkpointRange{3, 6})
			_, root := tp.Tracer("ttl").Start(context.Background(), "root")
			parent := ttlChildContext(t, root)
			original := backend.GetBaggageFromContext(parent)[BaggageBRKey]
			raw, _ := decodeBR(original)
			var wg sync.WaitGroup
			for i := 1; i <= 64; i++ {
				wg.Add(1)
				go func(seq int) {
					defer wg.Done()
					ctx := context.WithValue(parent, "seqNum", seq)
					for hop := 1; hop <= 2; hop++ {
						_, child := tp.Tracer("ttl").Start(ctx, "child")
						childRaw, _ := decodeBR(checkpointSpanAttribute(child, AttrBR))
						if len(childRaw) < 2 || int(childRaw[0]) != int(raw[0])-hop {
							t.Errorf("sibling TTL was shared, consumed twice, or redrawn: %v", childRaw)
						}
						ctx = ttlChildContext(t, child)
						child.End()
					}
				}(i)
			}
			wg.Wait()
			root.End()
			if backend.GetBaggageFromContext(parent)[BaggageBRKey] != original {
				t.Fatal("fanout mutated parent's baggage")
			}
			hp, lp := snapshot()
			if len(hp) != 1 || len(lp) != 128 {
				t.Fatalf("HP=%d LP=%d", len(hp), len(lp))
			}
		})
	}
}

func TestCheckpointRangeDiscovery(t *testing.T) {
	oldM, oldK := BloomFilterM, BloomFilterK
	defer func() { BloomFilterM, BloomFilterK = oldM, oldK; backend.SetRTBloomParams(oldM, oldK) }()
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		for _, valid := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/valid=%v", kind, valid), func(t *testing.T) {
				low := 2.0
				if !valid {
					low = 2.5
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.URL.Path != "/getFullConfig" {
						t.Errorf("wrong discovery path: %s", req.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"config": map[string]interface{}{"cpd_min": low, "cpd_max": 6}})
				}))
				defer server.Close()
				host, portString, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
				port, _ := strconv.Atoi(portString)
				var got checkpointRange
				var distance int64
				var err error
				switch kind {
				case "pb":
					p := &PathBridgeProcessor{agentEndpoint: host + ":4317", configDiscoveryPort: port, httpClient: server.Client()}
					err = p.fetchFullConfig(context.Background())
					got, distance = p.checkpointRange, p.checkpointDistance
				case "cgpb":
					p := &CallGraphBridgeProcessor{agentEndpoint: host + ":4317", configDiscoveryPort: port, httpClient: server.Client()}
					err = p.fetchFullConfig(context.Background())
					got, distance = p.checkpointRange, p.checkpointDistance
				case "sb":
					p := &StructuralBridgeProcessor{agentEndpoint: host + ":4317", configDiscoveryPort: port, httpClient: server.Client()}
					err = p.fetchFullConfig(context.Background())
					got, distance = p.checkpointRange, p.checkpointDistance
				}
				expectedDistance := int64(0)
				if kind == "sb" {
					expectedDistance = 6
				}
				if valid && (err != nil || got != (checkpointRange{2, 6}) || distance != expectedDistance) {
					t.Fatalf("discovery: %v, %v, %d", got, err, distance)
				}
				if !valid && !errors.Is(err, errInvalidCheckpointRange) {
					t.Fatalf("bad range silently accepted: %v", err)
				}
			})
		}
	}
}

func TestCheckpointRangeBounds(t *testing.T) {
	for _, pair := range [][2]interface{}{{nil, 6}, {2, nil}, {0, 2}, {-1, 2}, {3, 2}, {1, 257}, {1.5, 2}, {math.NaN(), 2}, {true, 2}, {1, math.Inf(1)}} {
		if _, err := parseCheckpointRange(map[string]interface{}{"cpd_min": pair[0], "cpd_max": pair[1]}); !errors.Is(err, errInvalidCheckpointRange) {
			t.Fatalf("accepted %v", pair)
		}
	}
	for _, config := range []map[string]interface{}{nil, {"cpd": 6}} {
		if r, err := parseCheckpointRange(config); err != nil || r.enabled() {
			t.Fatal("legacy configuration changed")
		}
	}
	r := checkpointRange{1, 256}
	seen := map[byte]bool{}
	for i := 0; i < 4096; i++ {
		cp, ttl := r.next(0, false)
		if !cp {
			t.Fatal("root is not a checkpoint")
		}
		seen[ttl] = true
	}
	if len(seen) < 200 {
		t.Fatalf("distance draws are not spread across the configured range: %d values", len(seen))
	}
	for _, raw := range [][]byte{nil, {0}, {255}} {
		if _, payload := r.unwrap(raw); len(payload) != 0 {
			t.Fatal("accepted truncated carrier")
		}
	}
}

func TestCheckpointTTLReverseLocation(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			tp, snapshot := ttlTestProvider(t, kind, checkpointRange{2, 2})
			ctx := context.Background()
			var spans []trace.Span
			for i := 0; i <= 3; i++ {
				_, span := tp.Tracer("ttl").Start(ctx, "span", trace.WithSpanKind(trace.SpanKindServer))
				span.SetAttributes(attribute.Bool("hasChildren", i < 3))
				spans = append(spans, span)
				ctx = ttlChildContext(t, span)
			}
			// Tomislav-RetCtx: depth 2 is an original TTL checkpoint and cannot
			// reject. Only the early leaf at depth 3 returns a new reverse TTL.
			backend.PrepareCheckpoint(tp, spans[2])
			if backend.ReadReverseBaggage(spans[2]) != "" {
				t.Fatal("original TTL checkpoint rejected")
			}
			cp := spans[3]
			truss, _ := decodeBR(checkpointSpanAttribute(cp, AttrBREmit))
			if !backend.PrepareCheckpoint(tp, cp) {
				t.Fatal("SDK did not prepare TTL checkpoint")
			}
			returned, err := backend.DecodeReturnedCheckpoints(backend.ReadReverseBaggage(cp))
			if err != nil || len(returned) != 1 || returned[0].Depth != 3 || returned[0].SpanID != cp.SpanContext().SpanID() || !bytes.Equal(returned[0].Truss, truss) || returned[0].ReverseTTL == nil || *returned[0].ReverseTTL != 1 {
				t.Fatalf("TTL corrupted reverse checkpoint origin or payload: %+v, %v", returned, err)
			}
			cp.End()
			hp, lp := snapshot()
			if len(hp) != 0 || len(lp) != 1 {
				t.Fatal("declined TTL checkpoint was exported as HP or dropped")
			}
			spans[1].End()
			spans[2].End()
			spans[0].End()
		})
	}
}
