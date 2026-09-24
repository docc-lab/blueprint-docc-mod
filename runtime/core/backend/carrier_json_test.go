package backend

// Tomislav-RetCtx: the reflection-free carrier encoder must produce exactly the bytes the
// encoding/json path produced, for every input the wrappers can hand it.

import (
	"encoding/json"
	"math/rand"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func reflectCarrier(sc trace.SpanContext, baggage map[string]string) string {
	tc, _ := sc.MarshalJSON()
	combined := map[string]interface{}{"trace_ctx": json.RawMessage(tc), "baggage": baggage}
	out, _ := json.Marshal(combined)
	return string(out)
}

var carrierStrings = []string{"", "plain", "AQEb6UigeAaGCLcCAihtDi7T5W5UAwISkIQkJCEB", "a+b/c=", `quote"back\slash`,
	"<html>&amp;", "tab\tnl\ncr\rbs\bff\f", "\x00\x01\x1f\x7f", "héllo wörld", "  ", "bad\xff\xfeutf8",
	"emoji 😀", "\xed\xa0\x80surrogate"}

func TestEncodeTraceCarrierMatchesReflection(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	ts, _ := trace.ParseTraceState("vendor=a<b,other=x")
	for i := 0; i < 2000; i++ {
		var tid trace.TraceID
		var sid trace.SpanID
		rng.Read(tid[:])
		rng.Read(sid[:])
		cfg := trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.TraceFlags(rng.Intn(256)), Remote: rng.Intn(2) == 1}
		if i%3 == 0 {
			cfg.TraceState = ts
		}
		sc := trace.NewSpanContext(cfg)
		var baggage map[string]string
		if i%7 != 0 {
			baggage = map[string]string{}
			for j := 0; j < rng.Intn(6); j++ {
				baggage[carrierStrings[rng.Intn(len(carrierStrings))]+string(rune('a'+j))] = carrierStrings[rng.Intn(len(carrierStrings))]
			}
		}
		want := reflectCarrier(sc, baggage)
		if got := EncodeTraceCarrier(sc, baggage); got != want {
			t.Fatalf("EncodeTraceCarrier\n got %s\nwant %s", got, want)
		}
		tc, _ := sc.MarshalJSON()
		if got, err := AddBaggageToTraceContext(string(tc), baggage); err != nil || got != want {
			t.Fatalf("AddBaggageToTraceContext\n got %s\nwant %s (%v)", got, want, err)
		}
		// and it still round-trips through the receiver
		cfg2, bag2, err := GetSpanContext(want)
		if err != nil || cfg2.TraceID != tid || cfg2.SpanID != sid || len(bag2) != len(baggage) {
			t.Fatalf("round trip: %v %v %v", err, cfg2, bag2)
		}
	}
	// a non-compact trace context takes the reflection path and is compacted as before
	got, _ := AddBaggageToTraceContext(`{ "TraceID": "x" }`, map[string]string{"k": "v"})
	if got != `{"baggage":{"k":"v"},"trace_ctx":{"TraceID":"x"}}` {
		t.Fatalf("slow path changed: %s", got)
	}
}

func BenchmarkCarrierReflect(b *testing.B) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1})
	bag := map[string]string{"__seq": "2", "_br": "AQEb6UigeAaGCLcCAihtDi7T5W5UAwISkIQkJCEB"}
	for i := 0; i < b.N; i++ {
		_ = reflectCarrier(sc, bag)
	}
}

func BenchmarkCarrierDirect(b *testing.B) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1})
	bag := map[string]string{"__seq": "2", "_br": "AQEb6UigeAaGCLcCAihtDi7T5W5UAwISkIQkJCEB"}
	for i := 0; i < b.N; i++ {
		_ = EncodeTraceCarrier(sc, bag)
	}
}

func TestParseCarrierFastMatchesUnmarshal(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	ts, _ := trace.ParseTraceState("vendor=a,other=x")
	check := func(s string) {
		t.Helper()
		var want traceCtxWithBaggage
		werr := json.Unmarshal([]byte(s), &want)
		got, ok := parseCarrierFast(s)
		if !ok {
			return // falls back to json.Unmarshal: identical by construction
		}
		if werr != nil {
			t.Fatalf("fast path accepted what json rejects: %q", s)
		}
		if got.TraceCtx != want.TraceCtx || len(got.Baggage) != len(want.Baggage) || (got.Baggage == nil) != (want.Baggage == nil) {
			t.Fatalf("mismatch for %q:\n got %+v\nwant %+v", s, got, want)
		}
		for k, v := range want.Baggage {
			if got.Baggage[k] != v {
				t.Fatalf("baggage %q mismatch for %q", k, s)
			}
		}
	}
	fastHits := 0
	for i := 0; i < 2000; i++ {
		var tid trace.TraceID
		var sid trace.SpanID
		rng.Read(tid[:])
		rng.Read(sid[:])
		cfg := trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.TraceFlags(rng.Intn(256)), Remote: rng.Intn(2) == 1}
		if i%3 == 0 {
			cfg.TraceState = ts
		}
		var baggage map[string]string
		if i%7 != 0 {
			baggage = map[string]string{}
			for j := 0; j < rng.Intn(6); j++ {
				baggage[carrierStrings[rng.Intn(len(carrierStrings))]+string(rune('a'+j))] = carrierStrings[rng.Intn(len(carrierStrings))]
			}
		}
		s := EncodeTraceCarrier(trace.NewSpanContext(cfg), baggage)
		check(s)
		if _, ok := parseCarrierFast(s); ok {
			fastHits++
		}
		// corruptions must never be accepted with a different meaning
		for _, cut := range []int{1, len(s) / 2, len(s) - 1} {
			check(s[:cut])
		}
		check(s + " ")
		check(s[:len(s)-1] + `,"x":1}`)
	}
	if fastHits < 500 {
		t.Fatalf("fast path rarely taken: %d", fastHits)
	}
	for _, s := range []string{``, `{}`, `{"trace_ctx":{"TraceID":"a","SpanID":"b","TraceFlags":"01","TraceState":"","Remote":false},"baggage":{}}`,
		`{"baggage":{"a":"b"},"trace_ctx":{"TraceID":"a","SpanID":"b","TraceFlags":"01","TraceState":"","Remote":false}}`} {
		check(s)
	}
}

func BenchmarkCarrierParseReflect(b *testing.B) {
	s := EncodeTraceCarrier(trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1}),
		map[string]string{"__seq": "2", "_br": "AQEb6UigeAaGCLcCAihtDi7T5W5UAwISkIQkJCEB"})
	for i := 0; i < b.N; i++ {
		var t traceCtxWithBaggage
		_ = json.Unmarshal([]byte(s), &t)
	}
}

func BenchmarkCarrierParseFast(b *testing.B) {
	s := EncodeTraceCarrier(trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1}),
		map[string]string{"__seq": "2", "_br": "AQEb6UigeAaGCLcCAihtDi7T5W5UAwISkIQkJCEB"})
	for i := 0; i < b.N; i++ {
		_, _ = parseCarrierFast(s)
	}
}
