package otelcol

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func checkpointSpanAttribute(span trace.Span, key attribute.Key) string {
	for _, attr := range span.(sdktrace.ReadOnlySpan).Attributes() {
		if attr.Key == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func exportedCheckpoint(t *testing.T, span *tracepb.Span) string {
	t.Helper()
	var checkpoint string
	for _, attr := range span.Attributes {
		if backend.IsReverseBaggageKey(attribute.Key(attr.Key)) {
			t.Fatalf("SDK leaked reverse carrier: %s", attr.Key)
		}
		if attr.Key == backend.ReverseTrussCheckpointKey {
			checkpoint = attr.Value.GetStringValue()
		}
	}
	return checkpoint
}

// Tomislav-RetCtx: each live receiver uses its own trusses' TTLs, while an
// original checkpoint consumes the whole bundle independently of those TTLs.
func TestSDKReceiverDecisionBeforeEnd(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"v", "pb", "cgpb", "sb"} {
		for _, tc := range []struct {
			name          string
			spanKind      trace.SpanKind
			parentDepth   int
			ttl           byte
			root, consume bool
		}{
			{"client_expired", trace.SpanKindClient, 0, 0, false, true},
			{"server_expired", trace.SpanKindServer, 0, 0, false, true},
			{"client_forward", trace.SpanKindClient, 0, 2, false, false},
			{"server_forward", trace.SpanKindServer, 0, 2, false, false},
			{"client_original", trace.SpanKindClient, 2, 255, false, true},
			{"server_original", trace.SpanKindServer, 2, 255, false, true},
			{"root_boundary", trace.SpanKindServer, 0, 255, true, true},
			{"root_service_client", trace.SpanKindClient, 0, 2, true, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				t.Setenv("RT_ROOT", "off")
				if tc.root {
					t.Setenv("RT_ROOT", "on")
				}
				provider, buffered := checkpointTestProvider(kind)
				_, span := provider.Tracer("test").Start(checkpointParentContext(kind, tc.parentDepth), "receiver", trace.WithSpanKind(tc.spanKind))
				input := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1}, 130, backend.SegPathCheckpoint, []byte{9, 8, 7}, tc.ttl)
				span.SetAttributes(attribute.Bool("hasChildren", true), attribute.String(backend.ReverseTrussInputKey, input))
				forwardCarrier := checkpointSpanAttribute(span, AttrBR)
				if checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != "" || backend.ReadReverseBaggage(span) != "" {
					t.Fatal("attaching input already decided checkpointing")
				}
				wantCheckpoint, wantReturn := "", ""
				if tc.consume || kind == "v" {
					wantCheckpoint = input
				} else {
					wantReturn = backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1}, 130, backend.SegPathCheckpoint, []byte{9, 8, 7}, tc.ttl-1)
				}
				for i := 0; i < 3; i++ {
					if !backend.PrepareCheckpoint(provider, span) || !span.IsRecording() {
						t.Fatal("SDK did not prepare live span")
					}
					if checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != wantCheckpoint || backend.ReadReverseBaggage(span) != wantReturn {
						t.Fatal("wrong consume/forward decision or repeated TTL decrement")
					}
				}
				if checkpointSpanAttribute(span, AttrBR) != forwardCarrier {
					t.Fatal("reverse emission reset forward window")
				}
				if hp, lp := buffered(); len(hp)+len(lp) != 0 {
					t.Fatal("preparation exported an unfinished span")
				}
				span.End()
				hp, lp := buffered()
				if len(hp)+len(lp) != 1 || (len(hp) == 1) != (wantCheckpoint != "") {
					t.Fatalf("HP=%d LP=%d", len(hp), len(lp))
				}
				if exportedCheckpoint(t, append(hp, lp...)[0]) != wantCheckpoint {
					t.Fatal("OnEnd lost SDK decision")
				}
			})
		}
	}
}

func TestSDKCoalescesExpiredTrussesAndForwardsOthers(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			provider, buffered := checkpointTestProvider(kind)
			var input string
			for i, ttl := range []byte{0, 3, 0} {
				input = backend.MergeRetCtx(input, backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{byte(i + 1)}, 130+uint64(i), backend.SegCallGraphCheckpoint, []byte{byte(i), 0xff}, ttl))
			}
			_, span := provider.Tracer("test").Start(checkpointParentContext(kind, 0), "fan-in", trace.WithSpanKind(trace.SpanKindServer))
			span.SetAttributes(attribute.Bool("hasChildren", true), attribute.String(backend.ReverseTrussInputKey, input))
			ownTruss := checkpointSpanAttribute(span, AttrBREmit)
			backend.PrepareCheckpoint(provider, span)
			emitted := checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey)
			pending := backend.ReadReverseBaggage(span)
			cps, err := backend.DecodeReturnedCheckpoints(emitted)
			if err != nil || len(cps) != 2 || cps[0].SpanID != (trace.SpanID{1}) || cps[1].SpanID != (trace.SpanID{3}) {
				t.Fatalf("wrong expired set: %+v %v", cps, err)
			}
			cps, err = backend.DecodeReturnedCheckpoints(pending)
			if err != nil || len(cps) != 1 || cps[0].SpanID != (trace.SpanID{2}) || cps[0].ReverseTTL == nil || *cps[0].ReverseTTL != 2 {
				t.Fatalf("new checkpoint absorbed or reset a nonexpired sibling: %+v %v", cps, err)
			}
			backend.PrepareCheckpoint(provider, span)
			if backend.ReadReverseBaggage(span) != pending || checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != emitted {
				t.Fatal("repeat preparation consumed pending siblings")
			}
			span.End()
			hp, lp := buffered()
			if len(hp) != 1 || len(lp) != 0 || exportedCheckpoint(t, hp[0]) != emitted {
				t.Fatal("expiry did not produce one checkpoint span")
			}
			var ownBytes []byte
			for _, attr := range hp[0].Attributes {
				if attr.Key == AttrBREmit {
					ownBytes = attr.Value.GetBytesValue()
				}
			}
			want, _ := decodeBR(ownTruss)
			if len(ownBytes) == 0 || !bytes.Equal(ownBytes, want) {
				t.Fatal("emission omitted or changed its own checkpoint data")
			}
		})
	}
}

func TestSDKReverseTTLCountsBothSpanKinds(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			provider, snapshot := ttlTestProvider(t, kind, checkpointRange{6, 6})
			ctx := context.Background()
			var spans []trace.Span
			for depth := 0; depth <= 3; depth++ {
				spanKind := trace.SpanKindServer
				if depth%2 == 1 {
					spanKind = trace.SpanKindClient
				}
				_, span := provider.Tracer("test").Start(ctx, "path", trace.WithSpanKind(spanKind))
				span.SetAttributes(attribute.Bool("hasChildren", true))
				spans = append(spans, span)
				ctx = ttlChildContext(t, span)
			}
			input := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1}, 4, backend.SegPathCheckpoint, []byte{1, 2}, 1)
			spans[3].SetAttributes(attribute.String(backend.ReverseTrussInputKey, input))
			backend.PrepareCheckpoint(provider, spans[3])
			pending := backend.ReadReverseBaggage(spans[3])
			cps, err := backend.DecodeReturnedCheckpoints(pending)
			if err != nil || len(cps) != 1 || cps[0].ReverseTTL == nil || *cps[0].ReverseTTL != 0 {
				t.Fatal("client failed to decrement once")
			}
			spans[2].SetAttributes(attribute.String(backend.ReverseTrussInputKey, pending))
			backend.PrepareCheckpoint(provider, spans[2])
			if backend.ReadReverseBaggage(spans[2]) != "" || checkpointSpanAttribute(spans[2], backend.ReverseTrussCheckpointKey) != pending {
				t.Fatal("server failed to emit expired truss")
			}
			for _, span := range spans {
				span.End()
			}
			if hp, lp := snapshot(); len(hp) != 2 || len(lp) != 2 {
				t.Fatalf("expected root + reverse checkpoint: HP=%d LP=%d", len(hp), len(lp))
			}
		})
	}
}

func TestSDKReceiverConcurrentRequests(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	// The removed provider-wide counter must not couple unrelated requests.
	t.Setenv("RT_POLICY", "1")
	t.Setenv("RT_DEPTH", "1000000")
	t.Setenv(SampleRatioEnv, "1")
	provider, buffered := checkpointTestProvider("pb")
	const requests = 64
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ttl := byte(i % 4)
			input := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{byte(i + 1)}, 130+uint64(i), backend.SegPathCheckpoint, []byte{byte(i)}, ttl)
			_, span := provider.Tracer("test").Start(checkpointParentContext("pb", 0), "receiver", trace.WithSpanKind(trace.SpanKindClient))
			span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, input))
			backend.PrepareCheckpoint(provider, span)
			backend.PrepareCheckpoint(provider, span)
			if ttl == 0 {
				if checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != input || backend.ReadReverseBaggage(span) != "" {
					t.Errorf("request %d did not emit expired truss", i)
				}
			} else {
				cps, err := backend.DecodeReturnedCheckpoints(backend.ReadReverseBaggage(span))
				if err != nil || len(cps) != 1 || cps[0].SpanID != (trace.SpanID{byte(i + 1)}) || cps[0].ReverseTTL == nil || *cps[0].ReverseTTL != ttl-1 {
					t.Errorf("request %d mixed or aged another truss", i)
				}
			}
			span.End()
		}(i)
	}
	wg.Wait()
	hp, lp := buffered()
	if len(hp) != requests/4 || len(lp) != 3*requests/4 {
		t.Fatalf("concurrent TTLs HP=%d LP=%d", len(hp), len(lp))
	}
}
