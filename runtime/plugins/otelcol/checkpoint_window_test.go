package otelcol

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestCheckpointWindowCodec(t *testing.T) {
	for distance, width := range map[int]int{1: 3, 2: 3, 3: 5, 4: 8, 6: 12, 256: 612} {
		t.Run(fmt.Sprint(distance), func(t *testing.T) {
			anchor := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
			bits := bytes.Repeat([]byte{0x5a}, width)
			ha := haAppendEntry(nil, "0102030405060708", 130)
			packed := packCheckpointWindowBR(256, anchor, distance, bits, ha)
			d, a, cpd, bb, hh, ok := unpackCheckpointWindowBR(packed)
			if !ok || d != 256 || a != anchor || cpd != distance || !bytes.Equal(bb, bits) || !bytes.Equal(hh, ha) {
				t.Fatal("window descriptor did not preserve variable Bloom/HA boundaries")
			}
			// A descriptor without its complete filter must not decode as a parent.
			for n := 0; n < len(packed)-len(ha); n++ {
				if _, _, _, _, _, ok := unpackCheckpointWindowBR(packed[:n]); ok {
					t.Fatalf("accepted truncated window at byte %d", n)
				}
			}
		})
	}
}

func TestCheckpointWindowChangesGeometryAtCheckpoint(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	for _, kind := range []string{"pb", "cgpb"} {
		for _, pair := range [][2]int{{6, 2}, {2, 6}, {4, 3}, {256, 1}} {
			t.Run(fmt.Sprintf("%s/%d-to-%d", kind, pair[0], pair[1]), func(t *testing.T) {
				previousDistance, nextDistance := pair[0], pair[1]
				tp, snapshot := ttlTestProvider(t, kind, checkpointRange{nextDistance, nextDistance})
				m, k := bloom.EstimateParameters(uint(maxInt(previousDistance-1, 1)), DefaultBloomFPRate)
				filter := bloom.New(m, k)
				var parentID trace.SpanID
				for i := 1; i < previousDistance; i++ {
					parentID = trace.SpanID{byte(i), 1}
					filter.AddPrehashed(parentID[:])
				}
				anchor := [8]byte{255, 2}
				var ha []byte
				if kind == "cgpb" {
					ha = haAppendEntry(nil, "0102030405060708", 1)
				}
				inbound := packCheckpointWindowBR(previousDistance-1, anchor, previousDistance, filter.Bytes(), ha)
				ctx := trace.ContextWithRemoteSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
					TraceID: trace.TraceID{1}, SpanID: parentID, TraceFlags: trace.FlagsSampled,
				}))
				ctx = backend.SetBaggageInContext(ctx, map[string]string{BaggageBRKey: encodeBR(append([]byte{0}, inbound...))})
				ctx = context.WithValue(ctx, "seqNum", 1)
				_, span := tp.Tracer("window").Start(ctx, "checkpoint", trace.WithSpanKind(trace.SpanKindServer))
				span.SetAttributes(attribute.Bool("hasChildren", true))
				emit, _ := decodeBR(checkpointSpanAttribute(span, AttrBREmit))
				d, a, cpd, bb, hh, ok := unpackCheckpointWindowBR(emit)
				if !ok || d != previousDistance || a != anchor || cpd != previousDistance || !bytes.Equal(bb, filter.Bytes()) || !bytes.Equal(hh, ha) {
					t.Fatal("new draw corrupted the completed incoming window")
				}
				outgoing, _ := decodeBR(checkpointSpanAttribute(span, AttrBR))
				d, a, cpd, bb, hh, ok = unpackCheckpointWindowBR(outgoing[1:])
				nextM, _ := bloom.EstimateParameters(uint(maxInt(nextDistance-1, 1)), DefaultBloomFPRate)
				if !ok || d != previousDistance || a != [8]byte(span.SpanContext().SpanID()) || cpd != nextDistance || int(outgoing[0])+1 != nextDistance || len(bb) != int((nextM+7)/8) || !bytes.Equal(bb, make([]byte, len(bb))) || len(hh) != 0 {
					t.Fatal("new window was not reset and sized for its own draw")
				}
				if hp, lp := snapshot(); len(hp)+len(lp) != 0 {
					t.Fatal("unfinished checkpoint was exported")
				}
				span.End()
				if hp, lp := snapshot(); len(hp) != 1 || len(lp) != 0 {
					t.Fatal("checkpoint role was not retained through OnEnd")
				}
			})
		}
	}
}

func TestCheckpointWindowReverseFanIn(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	for _, kind := range []string{"pb", "cgpb"} {
		t.Run(kind, func(t *testing.T) {
			var merged string
			for _, distance := range []int{2, 6} {
				tp, _ := ttlTestProvider(t, kind, checkpointRange{distance, distance})
				_, root := tp.Tracer("window").Start(context.Background(), "root")
				_, leaf := tp.Tracer("window").Start(ttlChildContext(t, root), "early leaf", trace.WithSpanKind(trace.SpanKindServer))
				carried, _ := backend.PrepareCheckpoint(tp, leaf, "")
				merged = backend.MergeRetCtx(merged, carried)
				leaf.End()
				root.End()
			}
			checkpoints, err := backend.DecodeReturnedCheckpoints(merged)
			if err != nil || len(checkpoints) != 2 {
				t.Fatalf("reverse merge: %v, %v", checkpoints, err)
			}
			for i, distance := range []int{2, 6} {
				checkpoint := checkpoints[i]
				d, _, cpd, bb, _, ok := unpackCheckpointWindowBR(checkpoint.Truss)
				m, _ := bloom.EstimateParameters(uint(distance-1), DefaultBloomFPRate)
				if !ok || d != 1 || checkpoint.Depth != 1 || cpd != distance || len(bb) != int((m+7)/8) {
					t.Fatal("early leaf/fan-in lost the intended window geometry")
				}
			}
		})
	}
}

func TestCheckpointWindowConcurrentTransitions(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	for _, kind := range []string{"pb", "cgpb"} {
		t.Run(kind, func(t *testing.T) {
			tp, snapshot := ttlTestProvider(t, kind, checkpointRange{2, 6})
			// A deployment-wide size must have no effect on ranged windows.
			setBloomForCPD(256)
			_, root := tp.Tracer("window").Start(context.Background(), "root")
			parent := ttlChildContext(t, root)
			carrier := backend.GetBaggageFromContext(parent)[BaggageBRKey]
			rootRaw, _ := decodeBR(carrier)
			_, _, rootDistance, _, _, _ := unpackCheckpointWindowBR(rootRaw[1:])
			var expectedHP atomic.Int64
			expectedHP.Store(1)
			var draws [5]atomic.Int64
			var wg sync.WaitGroup
			for branch := 0; branch < 64; branch++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ctx, incoming, distance := parent, rootRaw[0], rootDistance
					for hop := 1; hop <= 30; hop++ {
						_, child := tp.Tracer("window").Start(ctx, "descendant")
						raw, _ := decodeBR(checkpointSpanAttribute(child, AttrBR))
						d, _, cpd, bb, _, ok := unpackCheckpointWindowBR(raw[1:])
						if !ok || d != hop || cpd < 2 || cpd > 6 {
							t.Error("invalid concurrent window")
							child.End()
							return
						}
						if incoming == 0 {
							expectedHP.Add(1)
							draws[cpd-2].Add(1)
							if cpd != int(raw[0])+1 {
								t.Error("checkpoint draw differs from outgoing geometry")
							}
						} else if raw[0] != incoming-1 || cpd != distance {
							t.Error("a concurrent sibling changed the countdown or geometry")
						}
						m, _ := bloom.EstimateParameters(uint(cpd-1), DefaultBloomFPRate)
						if len(bb) != int((m+7)/8) {
							t.Error("filter uses a size unrelated to this window")
						}
						ctx, incoming, distance = ttlChildContext(t, child), raw[0], cpd
						child.End()
					}
				}()
			}
			wg.Wait()
			root.End()
			hp, lp := snapshot()
			if int64(len(hp)) != expectedHP.Load() || len(hp)+len(lp) != 1921 {
				t.Fatal("concurrent window transitions corrupted checkpoint classification")
			}
			if backend.GetBaggageFromContext(parent)[BaggageBRKey] != carrier {
				t.Fatal("a descendant mutated the parent's carrier")
			}
			for i := range draws {
				if draws[i].Load() == 0 {
					t.Fatalf("concurrent paths did not exercise distance %d", i+2)
				}
			}
		})
	}
}
