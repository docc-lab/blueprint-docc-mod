package otelcol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestReversePolicyConfig(t *testing.T) {
	for _, config := range []map[string]interface{}{
		nil, {"reverse_policy": "ttl"}, {"reverse_policy": "inverse_depth"}, {"reverse_policy": "depth_linear"},
		{"reverse_policy": "probability", "reverse_probability": 0},
		{"reverse_policy": "probability", "reverse_probability": int64(1)},
		{"reverse_policy": "probability", "reverse_probability": 0.25},
		{"reverse_policy": "probability", "reverse_probability": "0.25"},
		{"reverse_policy": "probability", "reverse_probability": json.Number("0.25")},
	} {
		if _, err := parseReversePolicy(config); err != nil {
			t.Fatalf("valid config %v: %v", config, err)
		}
	}
	invalid := []map[string]interface{}{
		{"reverse_policy": ""}, {"reverse_policy": "typo"}, {"reverse_policy": nil}, {"reverse_policy": 1},
		{"reverse_probability": 0.5}, {"reverse_policy": "probability"},
		{"reverse_policy": "ttl", "reverse_probability": nil},
		{"reverse_policy": "inverse_depth", "reverse_probability": 0.5},
		{"reverse_policy": "depth_linear", "reverse_probability": 0.5},
	}
	for _, value := range []interface{}{nil, true, -0.1, 1.1, math.NaN(), math.Inf(1), "NaN", "bad"} {
		invalid = append(invalid, map[string]interface{}{"reverse_policy": "probability", "reverse_probability": value})
	}
	for _, config := range invalid {
		if _, err := parseReversePolicy(config); !errors.Is(err, errInvalidReversePolicy) {
			t.Fatalf("accepted %v: %v", config, err)
		}
	}
}

// Tomislav-RetCtx: normalization concerns conditional probabilities; earlier
// successes and mandatory root absorption determine the final distribution.
func TestReversePolicyDepthProbabilities(t *testing.T) {
	inverse := reversePolicy{mode: reversePolicyInverseDepth}
	linear := reversePolicy{mode: reversePolicyDepthLinear}
	for n := uint64(1); n <= 128; n++ {
		var sumInverse, sumLinear float64
		for d := uint64(0); d < n; d++ {
			pi, pl := inverse.probabilityAt(d, n), linear.probabilityAt(d, n)
			if pi <= 0 || pl <= 0 || pi > 1 || pl > 1 {
				t.Fatalf("invalid p at d=%d n=%d", d, n)
			}
			sumInverse += pi
			sumLinear += pl
			if d > 0 && pl <= linear.probabilityAt(d-1, n) {
				t.Fatal("linear probability does not favor deeper nodes")
			}
		}
		if math.Abs(sumInverse-1) > 1e-12 || math.Abs(sumLinear-1) > 1e-12 {
			t.Fatal("probabilities are not normalized")
		}
		meanDepth := func(policy reversePolicy) float64 {
			survival, mean := 1.0, 0.0
			for d := n - 1; d > 0; d-- {
				p := policy.probabilityAt(d, n)
				mean += survival * p * float64(d)
				survival *= 1 - p
			}
			return mean // Remaining mass is absorbed at depth-zero root.
		}
		if n > 1 && meanDepth(linear) <= meanDepth(inverse) {
			t.Fatal("linear policy failed to increase expected emission depth")
		}
	}
	for _, policy := range []reversePolicy{inverse, linear, {mode: reversePolicyProbability, probability: 0.25}} {
		for _, pair := range [][2]uint64{{0, 0}, {6, 6}, {7, 6}} {
			if policy.probabilityAt(pair[0], pair[1]) != 0 {
				t.Fatal("sampled an invalid ancestor/depth")
			}
		}
		p := policy.probabilityAt(math.MaxUint64-1, math.MaxUint64)
		if p <= 0 || p > 1 || math.IsNaN(p) || math.IsInf(p, 0) {
			t.Fatal("depth arithmetic overflowed")
		}
	}
	if math.Abs(inverse.probabilityAt(5, 6)-1.0/6) > 1e-15 || math.Abs(linear.probabilityAt(5, 6)-2.0/7) > 1e-15 || math.Abs(linear.probabilityAt(1, 6)-2.0/21) > 1e-15 {
		t.Fatal("n=6 policy examples changed")
	}
}

// Tomislav-RetCtx: depth_cubic must equal the bridges repo's formula (bridge/reverse.go,
// reverseAcceptance "depth_cubic") for every valid (receiver, origin) pair, be normalized over
// the n ancestors, never exceed 1, and absorb closer to the leaf than inverse_depth.
func TestReversePolicyDepthCubic(t *testing.T) {
	cubic := reversePolicy{mode: reversePolicyDepthCubic}
	inverse := reversePolicy{mode: reversePolicyInverseDepth}
	bridges := func(receiverDepth, originDepth int) float64 { // verbatim from bridge/reverse.go
		if originDepth <= 0 || receiverDepth < 0 || receiverDepth >= originDepth {
			return 0
		}
		d, n := float64(receiverDepth)+1, float64(originDepth)
		return 4 * d * d * d / (n * n * (n + 1) * (n + 1))
	}
	for n := uint64(1); n <= 128; n++ {
		var sum float64
		for d := uint64(0); d < n; d++ {
			p := cubic.probabilityAt(d, n)
			if want := bridges(int(d), int(n)); math.Abs(p-want) > 1e-15 {
				t.Fatalf("d=%d n=%d: got %v want %v", d, n, p, want)
			}
			if p <= 0 || p > 1 {
				t.Fatalf("invalid p=%v at d=%d n=%d", p, d, n)
			}
			if d > 0 && p <= cubic.probabilityAt(d-1, n) {
				t.Fatal("cubic probability does not favor deeper receivers")
			}
			sum += p
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Fatalf("n=%d not normalized: %v", n, sum)
		}
		if first, want := cubic.probabilityAt(n-1, n), 4*float64(n)/((float64(n)+1)*(float64(n)+1)); math.Abs(first-want) > 1e-15 {
			t.Fatalf("n=%d first-hop %v want 4n/(n+1)^2=%v", n, first, want)
		}
		if n > 1 && cubic.probabilityAt(n-1, n) <= inverse.probabilityAt(n-1, n) {
			t.Fatal("cubic does not absorb at the parent more often than inverse_depth")
		}
	}
	// the worked example for a depth-4 leaf (SN and HotelReservation leaves)
	for d, want := range map[uint64]float64{3: 0.64, 2: 0.27, 1: 0.08, 0: 0.01} {
		if math.Abs(cubic.probabilityAt(d, 4)-want) > 1e-15 {
			t.Fatalf("n=4 d=%d: %v want %v", d, cubic.probabilityAt(d, 4), want)
		}
	}
	for _, pair := range [][2]uint64{{0, 0}, {6, 6}, {7, 6}} {
		if cubic.probabilityAt(pair[0], pair[1]) != 0 {
			t.Fatal("sampled an invalid ancestor/depth")
		}
	}
	if p := cubic.probabilityAt(math.MaxUint64-1, math.MaxUint64); p <= 0 || p > 1 || math.IsNaN(p) || math.IsInf(p, 0) {
		t.Fatal("depth arithmetic overflowed")
	}
	if _, err := parseReversePolicy(map[string]interface{}{"reverse_policy": "depth_cubic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseReversePolicy(map[string]interface{}{"reverse_policy": "depth_cubic", "reverse_probability": 0.5}); err == nil {
		t.Fatal("depth_cubic accepted a reverse_probability")
	}
}

func probabilityTestTruss(id byte, depth uint64) string {
	return backend.EncodeCheckpointRetCtx(trace.SpanID{id}, depth, backend.SegCallGraphCheckpoint, []byte{id, 0, 0xff})
}

func returnedIDs(t *testing.T, encoded string) []byte {
	t.Helper()
	if encoded == "" {
		return nil
	}
	cps, err := backend.DecodeReturnedCheckpoints(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var ids []byte
	for _, cp := range cps {
		ids = append(ids, cp.SpanID[0])
	}
	return ids
}

func TestReversePolicyIndependentTrialsAndMixedTTL(t *testing.T) {
	var input string
	for _, id := range []byte{1, 2, 3} {
		input = backend.MergeRetCtx(input, probabilityTestTruss(id, 6))
	}
	input = backend.MergeRetCtx(input, backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{4}, 6, backend.SegPathCheckpoint, []byte{4}, 0))
	input = backend.MergeRetCtx(input, backend.EncodeCheckpointRetCtxWithTTL(trace.SpanID{5}, 6, backend.SegPathCheckpoint, []byte{5}, 2))
	policy := reversePolicy{mode: reversePolicyProbability, probability: 0.5}
	draws := 0
	emitted, pending := policy.route(input, false, 1, func() float64 {
		values := []float64{0.49, 0.5, 0.1}
		if draws >= len(values) {
			t.Fatal("TTL segment consumed a probability draw")
		}
		value := values[draws]
		draws++
		return value
	})
	if draws != 3 || !reflect.DeepEqual(returnedIDs(t, emitted), []byte{1, 3, 4}) || !reflect.DeepEqual(returnedIDs(t, pending), []byte{2, 5}) {
		t.Fatal("bundle was sampled once instead of independently per truss")
	}
	cps, _ := backend.DecodeReturnedCheckpoints(pending)
	if cps[0].ReverseTTL != nil || cps[1].ReverseTTL == nil || *cps[1].ReverseTTL != 1 {
		t.Fatal("probability routing changed explicit TTL behavior")
	}
	for _, output := range []string{emitted, pending} {
		cps, _ := backend.DecodeReturnedCheckpoints(output)
		for _, cp := range cps {
			if cp.Depth != 6 || cp.Truss[0] != cp.SpanID[0] {
				t.Fatal("origin or truss changed")
			}
		}
	}
	noDraw := func() float64 { t.Fatal("original checkpoint or TTL policy drew a probability"); return 0 }
	if got, rest := policy.route(input, true, 1, noDraw); got != input || rest != "" {
		t.Fatal("original checkpoint did not absorb all returns")
	}
	if got, rest := (reversePolicy{}).route(probabilityTestTruss(1, 6), false, 1, noDraw); got != "" || rest == "" {
		t.Fatal("default TTL policy changed")
	}
}

func TestReverseProbabilityUsesEachTrussDepth(t *testing.T) {
	input := backend.MergeRetCtx(probabilityTestTruss(1, 2), probabilityTestTruss(2, 6))
	policy := reversePolicy{mode: reversePolicyInverseDepth}
	emitted, pending := policy.route(input, false, 1, func() float64 { return 0.2 })
	if !reflect.DeepEqual(returnedIDs(t, emitted), []byte{1}) || !reflect.DeepEqual(returnedIDs(t, pending), []byte{2}) {
		t.Fatal("used one sibling's n for the whole bundle")
	}
	policy.mode = reversePolicyDepthLinear
	for _, depth := range []uint64{1, 5} {
		emitted, _ = policy.route(probabilityTestTruss(1, 6), false, depth, func() float64 { return 0.2 })
		if (emitted != "") != (depth == 5) {
			t.Fatal("linear policy does not distinguish receiver depth")
		}
	}
}

func TestSDKProbabilityBeforeEnd(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		for _, probability := range []float64{0, 1} {
			for _, spanKind := range []trace.SpanKind{trace.SpanKindClient, trace.SpanKindServer} {
				t.Run(fmt.Sprintf("%s/%g/%s", kind, probability, spanKind), func(t *testing.T) {
					tp, buffered := checkpointTestProvider(kind)
					tp.(*checkpointTracerProvider).CheckpointPreparer.(*reverseCheckpointProcessor).reversePolicy = reversePolicy{mode: reversePolicyProbability, probability: probability}
					_, span := tp.Tracer("test").Start(checkpointParentContext(kind, 0), "receiver", trace.WithSpanKind(spanKind))
					input := probabilityTestTruss(1, 6)
					span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, input), attribute.Bool("hasChildren", true))
					carrier := checkpointSpanAttribute(span, AttrBR)
					var onward string
					for i := 0; i < 3; i++ {
						onward, _ = backend.PrepareCheckpoint(tp, span, input)
					}
					if checkpointSpanAttribute(span, AttrBR) != carrier || !span.IsRecording() {
						t.Fatal("preparation changed forward propagation or ended span")
					}
					if probability == 1 {
						if onward != "" || checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != input {
							t.Fatal("p=1 did not consume")
						}
					} else if onward != input || checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey) != "" {
						t.Fatal("p=0 did not forward")
					}
					if hp, lp := buffered(); len(hp)+len(lp) != 0 {
						t.Fatal("preparation exported unfinished span")
					}
					span.End()
					hp, lp := buffered()
					if len(hp)+len(lp) != 1 || (len(hp) == 1) != (probability == 1) {
						t.Fatal("OnEnd lost probability classification")
					}
					if probability == 1 && exportedCheckpoint(t, hp[0]) != input {
						t.Fatal("export lost returned truss")
					}
				})
			}
		}
		for _, mode := range []string{reversePolicyProbability, reversePolicyInverseDepth, reversePolicyDepthLinear} {
			t.Run(kind+"/leaf/"+mode, func(t *testing.T) {
				tp, buffered := checkpointTestProvider(kind)
				tp.(*checkpointTracerProvider).CheckpointPreparer.(*reverseCheckpointProcessor).reversePolicy = reversePolicy{mode: mode, probability: 1}
				_, leaf := tp.Tracer("test").Start(checkpointParentContext(kind, 0), "leaf", trace.WithSpanKind(trace.SpanKindServer))
				carried, _ := backend.PrepareCheckpoint(tp, leaf, "")
				cps, err := backend.DecodeReturnedCheckpoints(carried)
				if err != nil || len(cps) != 1 || cps[0].ReverseTTL != nil || cps[0].SpanID != leaf.SpanContext().SpanID() || cps[0].Depth != 1 {
					t.Fatalf("probability leaf did not return its own TTL-free truss: %+v %v", cps, err)
				}
				leaf.End()
				if hp, lp := buffered(); len(hp) != 0 || len(lp) != 1 {
					t.Fatal("leaf refusal dropped span or retained checkpoint")
				}
			})
		}
	}
}

func TestReversePolicyDiscovery(t *testing.T) {
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		for _, mode := range []string{"ttl", "probability", "inverse_depth", "depth_linear", "invalid"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				config := map[string]interface{}{"cpd_min": 2, "cpd_max": 6, "reverse_policy": mode}
				if mode == "probability" {
					config["reverse_probability"] = 0.25
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/getFullConfig" {
						t.Error("wrong config discovery path")
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"config": config})
				}))
				defer server.Close()
				host, portString, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
				port, _ := strconv.Atoi(portString)
				var policy reversePolicy
				var err error
				switch kind {
				case "pb":
					p := &PathBridgeProcessor{agentEndpoint: host + ":4317", configDiscoveryPort: port, httpClient: server.Client()}
					err = p.fetchFullConfig(context.Background())
					policy = p.reversePolicy
				case "cgpb":
					p := &CallGraphBridgeProcessor{agentEndpoint: host + ":4317", configDiscoveryPort: port, httpClient: server.Client()}
					err = p.fetchFullConfig(context.Background())
					policy = p.reversePolicy
				case "sb":
					p := &StructuralBridgeProcessor{agentEndpoint: host + ":4317", configDiscoveryPort: port, httpClient: server.Client()}
					err = p.fetchFullConfig(context.Background())
					policy = p.reversePolicy
				}
				if mode == "invalid" {
					if !errors.Is(err, errInvalidReversePolicy) {
						t.Fatalf("invalid policy silently accepted: %v", err)
					}
				} else if err != nil || policy.mode != mode || (mode == "probability" && policy.probability != 0.25) {
					t.Fatalf("discovered policy=%+v error=%v", policy, err)
				}
			})
		}
	}
}
