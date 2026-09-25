package otelcol

// Tomislav-RetCtx: reverse_passthrough. Off (default), a scheduled checkpoint terminates every
// returned truss regardless of policy; on, it routes returned trusses by the policy like any
// other non-root span. The root always terminates.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestReversePassthroughConfig(t *testing.T) {
	for _, tc := range []struct {
		config map[string]interface{}
		want   bool
	}{
		{nil, false},
		{map[string]interface{}{"reverse_policy": "depth_cubic"}, false},
		{map[string]interface{}{"reverse_policy": "depth_cubic", "reverse_passthrough": false}, false},
		{map[string]interface{}{"reverse_policy": "depth_cubic", "reverse_passthrough": true}, true},
		{map[string]interface{}{"reverse_passthrough": true}, true},
	} {
		policy, err := parseReversePolicy(tc.config)
		if err != nil || policy.passthrough != tc.want {
			t.Fatalf("%v: passthrough=%v err=%v", tc.config, policy.passthrough, err)
		}
	}
	for _, value := range []interface{}{nil, "true", 1, 0.0} {
		if _, err := parseReversePolicy(map[string]interface{}{"reverse_passthrough": value}); !errors.Is(err, errInvalidReversePolicy) {
			t.Fatalf("accepted reverse_passthrough=%v: %v", value, err)
		}
	}
}

func TestReversePassthroughTerminates(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		p := reversePolicy{mode: reversePolicyDepthCubic, passthrough: passthrough}
		if !p.terminates(false, true) || !p.terminates(true, true) {
			t.Fatal("root must always terminate")
		}
		if p.terminates(false, false) {
			t.Fatal("ordinary span must not terminate")
		}
		if p.terminates(true, false) == passthrough {
			t.Fatalf("scheduled checkpoint with passthrough=%v", passthrough)
		}
	}
}

func TestSDKScheduledCheckpointPassthrough(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "on")
	t.Setenv("RT_ROOT", "off")
	t.Setenv("RT_LEAF_REJECT", "1")
	t.Setenv(SampleRatioEnv, "1")
	for _, kind := range []string{"pb", "cgpb", "sb"} {
		for _, tc := range []struct {
			passthrough bool
			probability float64
			consumed    bool
		}{
			{false, 0, true}, // default: the scheduled checkpoint absorbs whatever the policy says
			{true, 0, false}, // passthrough: a failed trial forwards the truss upstream
			{true, 1, true},  // passthrough: an accepted trial still emits here
		} {
			t.Run(fmt.Sprintf("%s/passthrough=%v/p=%g", kind, tc.passthrough, tc.probability), func(t *testing.T) {
				tp, buffered := checkpointTestProvider(kind)
				tp.(*checkpointTracerProvider).CheckpointPreparer.(*reverseCheckpointProcessor).reversePolicy =
					reversePolicy{mode: reversePolicyProbability, probability: tc.probability, passthrough: tc.passthrough}
				// parent depth 2, cpd 3: this span is a scheduled (original) checkpoint
				_, span := tp.Tracer("test").Start(checkpointParentContext(kind, 2), "original", trace.WithSpanKind(trace.SpanKindServer))
				input := probabilityTestTruss(1, 6)
				span.SetAttributes(attribute.String(backend.ReverseTrussInputKey, input), attribute.Bool("hasChildren", true))
				onward, _ := backend.PrepareCheckpoint(tp, span, input)
				emitted := checkpointSpanAttribute(span, backend.ReverseTrussCheckpointKey)
				if tc.consumed && (onward != "" || emitted != input) {
					t.Fatalf("expected the truss emitted here: onward=%q emitted=%q", onward, emitted)
				}
				if !tc.consumed && (onward != input || emitted != "") {
					t.Fatalf("expected the truss forwarded: onward=%q emitted=%q", onward, emitted)
				}
				span.End()
				// the span is a checkpoint either way (its own forward role is unchanged)
				if hp, lp := buffered(); len(hp) != 1 || len(lp) != 0 {
					t.Fatalf("scheduled checkpoint lost priority: HP=%d LP=%d", len(hp), len(lp))
				}
			})
		}
	}
}
