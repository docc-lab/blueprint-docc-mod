package otelcol

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestOriginalCheckpointsNeverReject(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"v", "pb", "cgpb", "sb"} {
		for _, hasChildren := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/children=%v", kind, hasChildren), func(t *testing.T) {
				tp, buffered := checkpointTestProvider(kind)
				_, span := tp.Tracer("test").Start(checkpointParentContext(kind, 2), "original", trace.WithSpanKind(trace.SpanKindServer))
				span.SetAttributes(attribute.Bool("hasChildren", hasChildren))
				backend.PrepareCheckpoint(tp, span)
				if backend.ReadReverseBaggage(span) != "" {
					t.Fatal("original checkpoint rejected")
				}
				span.End()
				if hp, lp := buffered(); len(hp) != 1 || len(lp) != 0 {
					t.Fatal("original checkpoint lost priority")
				}
			})
		}
	}
}

func TestRejectedLeafDrawsFreshReverseTTL(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		for _, r := range []checkpointRange{{1, 1}, {256, 256}, {2, 6}} {
			t.Run(fmt.Sprintf("%s/%d-%d", kind, r.min, r.max), func(t *testing.T) {
				// A six-span incoming window must survive a different reverse
				// draw unchanged, including its Bloom geometry and original depth.
				rootTP, _ := ttlTestProvider(t, kind, checkpointRange{6, 6})
				_, root := rootTP.Tracer("test").Start(context.Background(), "root")
				parent := ttlChildContext(t, root)
				tp, _ := ttlTestProvider(t, kind, r)
				seen := map[byte]bool{}
				for i := 0; i < 128; i++ {
					_, leaf := tp.Tracer("test").Start(parent, "leaf", trace.WithSpanKind(trace.SpanKindServer))
					want, _ := decodeBR(checkpointSpanAttribute(leaf, AttrBREmit))
					backend.PrepareCheckpoint(tp, leaf)
					cps, err := backend.DecodeReturnedCheckpoints(backend.ReadReverseBaggage(leaf))
					if err != nil || len(cps) != 1 || cps[0].ReverseTTL == nil || !bytes.Equal(cps[0].Truss, want) || cps[0].Depth != 1 {
						t.Fatalf("rejection lost TTL, origin, or original geometry: %+v %v", cps, err)
					}
					ttl := *cps[0].ReverseTTL
					if int(ttl)+1 < r.min || int(ttl)+1 > r.max {
						t.Fatalf("reverse TTL %d outside discovered range %+v", ttl, r)
					}
					seen[ttl] = true
					leaf.End()
				}
				root.End()
				if r.min != r.max && len(seen) < 2 {
					t.Fatal("siblings reused one reverse TTL draw")
				}
			})
		}
	}
}

func TestOriginalTTLLeafNeverRejects(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			tp, snapshot := ttlTestProvider(t, kind, checkpointRange{2, 2})
			_, root := tp.Tracer("test").Start(context.Background(), "root")
			defer root.End()
			_, client := tp.Tracer("test").Start(ttlChildContext(t, root), "client", trace.WithSpanKind(trace.SpanKindClient))
			defer client.End()
			_, leaf := tp.Tracer("test").Start(ttlChildContext(t, client), "leaf", trace.WithSpanKind(trace.SpanKindServer))
			backend.PrepareCheckpoint(tp, leaf)
			if backend.ReadReverseBaggage(leaf) != "" {
				t.Fatal("leaf scheduled by incoming TTL zero rejected")
			}
			leaf.End()
			if hp, lp := snapshot(); len(hp) != 1 || len(lp) != 0 {
				t.Fatal("original TTL leaf lost checkpoint priority")
			}
		})
	}
}

func TestReverseTTLRespectsSyntheticForceLP(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv(SampleRatioEnv, "1")
	tp, buffered := checkpointTestProvider("sb")
	_, span := tp.Tracer("test").Start(checkpointParentContext("sb", 0), "synthetic", trace.WithSpanKind(trace.SpanKindClient))
	input := backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{1}, 130, backend.SegStructuralCheckpoint, []byte{1, 2}, 0)
	span.SetAttributes(attribute.Bool(AttrForceLP, true), attribute.String(backend.ReverseTrussInputKey, input))
	backend.PrepareCheckpoint(tp, span)
	if backend.ReadReverseBaggage(span) != input || checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != "" {
		t.Fatal("synthetic force-LP span consumed or changed a return")
	}
	span.End()
	if hp, lp := buffered(); len(hp) != 0 || len(lp) != 1 {
		t.Fatal("synthetic span became a checkpoint")
	}
}

// Tomislav-RetCtx: exercise the entire SDK return path through nested concurrent
// fanout. Every rejected leaf must appear exactly once at the ancestor selected
// by its initial TTL or the first original checkpoint encountered on that path.
func TestReverseTTLConcurrentTree(t *testing.T) {
	testReversePolicyTree(t, reversePolicy{})
}

func TestReverseProbabilityConcurrentTree(t *testing.T) {
	for _, policy := range []reversePolicy{
		{mode: reversePolicyProbability, probability: 0},
		{mode: reversePolicyProbability, probability: 0.5},
		{mode: reversePolicyProbability, probability: 1},
		{mode: reversePolicyInverseDepth},
		{mode: reversePolicyDepthLinear},
	} {
		t.Run(fmt.Sprintf("%s/%g", policy.mode, policy.probability), func(t *testing.T) {
			testReversePolicyTree(t, policy)
		})
	}
}

func testReversePolicyTree(t *testing.T, policy reversePolicy) {
	t.Helper()
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			tp, snapshot := ttlTestProvider(t, kind, checkpointRange{2, 6})
			tp.(*checkpointTracerProvider).CheckpointPreparer.(*reverseCheckpointProcessor).reversePolicy = policy
			type location struct {
				parent   trace.SpanID
				original bool
			}
			var mu sync.Mutex
			locations := map[trace.SpanID]location{}
			rejected := map[trace.SpanID]backend.ReturnedCheckpoint{}
			start := func(ctx context.Context, spanKind trace.SpanKind) trace.Span {
				parent := trace.SpanContextFromContext(ctx).SpanID()
				raw, _ := decodeBR(backend.GetBaggageFromContext(ctx)[BaggageBRKey])
				original := !parent.IsValid() || (len(raw) > 0 && raw[0] == 0)
				_, span := tp.Tracer("tree").Start(ctx, "node", trace.WithSpanKind(spanKind))
				mu.Lock()
				locations[span.SpanContext().SpanID()] = location{parent, original}
				mu.Unlock()
				return span
			}
			finish := func(span trace.Span, input string, leaf bool) string {
				span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, input), attribute.Bool("hasChildren", !leaf))
				backend.PrepareCheckpoint(tp, span)
				output := backend.ReadReverseBaggage(span)
				if leaf && output != "" {
					cps, err := backend.DecodeReturnedCheckpoints(output)
					if err != nil || len(cps) != 1 || cps[0].SpanID != span.SpanContext().SpanID() {
						t.Errorf("invalid leaf return: %+v %v", cps, err)
					} else {
						mu.Lock()
						if locations[cps[0].SpanID].original {
							t.Error("original leaf checkpoint rejected")
						}
						rejected[cps[0].SpanID] = cps[0]
						mu.Unlock()
					}
				}
				span.End()
				return output
			}
			var call func(context.Context, int) string
			call = func(ctx context.Context, level int) string {
				server := start(ctx, trace.SpanKindServer)
				if level == 3 {
					return finish(server, "", true)
				}
				ctx = backend.WithRetMerge(ttlChildContext(t, server))
				var wg sync.WaitGroup
				for i := 0; i < 3; i++ {
					wg.Add(1)
					go func(seq int) {
						defer wg.Done()
						client := start(context.WithValue(ctx, "seqNum", seq), trace.SpanKindClient)
						returned := call(ttlChildContext(t, client), level+1)
						backend.AddToMerge(ctx, finish(client, returned, false))
					}(i + 1)
				}
				wg.Wait()
				return finish(server, backend.MergedChildren(ctx), false)
			}
			if returned := call(context.Background(), 0); returned != "" {
				t.Fatal("root leaked pending trusses")
			}
			hp, lp := snapshot()
			if len(hp)+len(lp) != 79 || len(locations) != 79 {
				t.Fatalf("fanout lost spans: HP=%d LP=%d locations=%d", len(hp), len(lp), len(locations))
			}
			seen := map[trace.SpanID]bool{}
			for _, entry := range hp {
				encoded := exportedCheckpoint(t, entry.span)
				if encoded == "" {
					continue
				}
				cps, err := backend.DecodeReturnedCheckpoints(encoded)
				if err != nil {
					t.Fatal(err)
				}
				for _, cp := range cps {
					origin, ok := rejected[cp.SpanID]
					if !ok || seen[cp.SpanID] || (origin.ReverseTTL == nil) != policy.probabilistic() {
						t.Fatalf("duplicated or invented reverse checkpoint: %+v", cp)
					}
					seen[cp.SpanID] = true
					emitter := locations[cp.SpanID].parent
					if origin.ReverseTTL != nil {
						ttl := int(*origin.ReverseTTL)
						for emitter.IsValid() && !locations[emitter].original && ttl > 0 {
							emitter = locations[emitter].parent
							ttl--
						}
					} else {
						// Probability may stop at any ancestor up to the first
						// original checkpoint, and must never escape that boundary.
						for emitter.IsValid() && !bytes.Equal(entry.span.SpanId, emitter[:]) {
							if locations[emitter].original {
								t.Fatal("truss passed an original checkpoint")
							}
							if policy.mode == reversePolicyProbability && policy.probability == 1 {
								t.Fatal("p=1 skipped immediate parent")
							}
							emitter = locations[emitter].parent
						}
						if policy.mode == reversePolicyProbability && policy.probability == 0 && !locations[emitter].original {
							t.Fatal("p=0 emitted before original checkpoint")
						}
					}
					if !emitter.IsValid() || !bytes.Equal(entry.span.SpanId, emitter[:]) || cp.Depth != origin.Depth || !bytes.Equal(cp.Truss, origin.Truss) {
						t.Fatalf("truss %s emitted at wrong ancestor or changed payload", cp.SpanID)
					}
				}
			}
			if len(seen) != len(rejected) {
				t.Fatalf("lost returned trusses: emitted=%d rejected=%d", len(seen), len(rejected))
			}
		})
	}
}
