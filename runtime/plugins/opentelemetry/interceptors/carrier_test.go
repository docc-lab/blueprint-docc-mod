package interceptors

// The compact carrier (carrier.go) must hand the server exactly what the wrappers' JSON carrier
// does: for any span context and baggage, decodeCarrier(encodeCarrier(x)) must equal
// backend.GetSpanContext(backend.EncodeTraceCarrier(x)).
//
// Run with:  cd runtime && go test ./plugins/opentelemetry/interceptors/ -run Carrier

import (
	"encoding/base64"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/trace"
)

// randomString returns a string of up to max bytes, including characters JSON has to escape.
func randomString(r *rand.Rand, max int) string {
	const alphabet = "abcXYZ019_.-\"\\/ \t\nüé€{}[]:,"
	var b strings.Builder
	for i, n := 0, r.Intn(max+1); i < n; i++ {
		b.WriteString(string([]rune(alphabet)[r.Intn(len([]rune(alphabet)))]))
	}
	return b.String()
}

func TestCarrierMatchesJSON(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	states := []string{"", "vendor=value", "a=1,b=2", "rojo=00f067aa0ba902b7,congo=t61rcWkgMzE"}
	for i := 0; i < 5000; i++ {
		var tid trace.TraceID
		var sid trace.SpanID
		r.Read(tid[:])
		r.Read(sid[:])
		ts, err := trace.ParseTraceState(states[r.Intn(len(states))])
		if err != nil {
			t.Fatal(err)
		}
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: tid, SpanID: sid, TraceFlags: trace.TraceFlags(r.Intn(256)), TraceState: ts,
		})
		baggage := map[string]string{}
		for j, n := 0, r.Intn(6); j < n; j++ {
			baggage[randomString(r, 12)] = randomString(r, 40)
		}
		if r.Intn(4) == 0 {
			baggage["__seq"] = "3" // the key the bridges use for the child ordinal
		}
		if r.Intn(2) == 0 {
			// The bridges' `_br` is raw bytes (runtime/plugins/otelcol/pack.go, propBR), usually
			// not valid UTF-8: the JSON path sends it as base64, the compact carrier as it is.
			br := make([]byte, r.Intn(40))
			r.Read(br)
			baggage["_br"] = string(br)
		}

		wantCfg, wantBag, wantErr := backend.GetSpanContext(backend.EncodeTraceCarrier(sc, baggage))
		gotCfg, gotBag, full, gotErr := decodeCarrier(encodeCarrier(sc, baggage))
		if (wantErr != nil) != (gotErr != nil) || !full {
			t.Fatalf("case %d: errors differ: JSON %v, compact %v (full=%v)", i, wantErr, gotErr, full)
		}
		// TraceState holds a slice (not comparable with ==), so compare its string form and the
		// other fields one by one.
		if wantCfg.TraceState.String() != gotCfg.TraceState.String() {
			t.Fatalf("case %d: trace state %q vs %q", i, wantCfg.TraceState, gotCfg.TraceState)
		}
		if wantCfg.TraceID != gotCfg.TraceID || wantCfg.SpanID != gotCfg.SpanID ||
			wantCfg.TraceFlags != gotCfg.TraceFlags || wantCfg.Remote != gotCfg.Remote {
			t.Fatalf("case %d: span context config\n JSON:    %+v\n compact: %+v", i, wantCfg, gotCfg)
		}
		if !reflect.DeepEqual(wantBag, gotBag) {
			t.Fatalf("case %d: baggage\n JSON:    %q\n compact: %q", i, wantBag, gotBag)
		}
	}
}

// An empty baggage map must come back as an empty, non-nil map, like the JSON's "baggage":{}.
func TestCarrierEmptyBaggage(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1})
	_, wantBag, _ := backend.GetSpanContext(backend.EncodeTraceCarrier(sc, map[string]string{}))
	_, gotBag, _, _ := decodeCarrier(encodeCarrier(sc, map[string]string{}))
	if gotBag == nil || len(gotBag) != 0 || (wantBag == nil) != (gotBag == nil) {
		t.Errorf("empty baggage: JSON gives %#v, compact gives %#v", wantBag, gotBag)
	}
}

// The unsampled form carries only the two IDs.
func TestCarrierUnsampled(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{9, 8, 7}, SpanID: trace.SpanID{6, 5}})
	cfg, bag, full, err := decodeCarrier(encodeUnsampledCarrier(sc))
	if err != nil || full || bag != nil || cfg.TraceID != sc.TraceID() || cfg.SpanID != sc.SpanID() || cfg.TraceFlags != 0 {
		t.Errorf("unsampled: cfg=%+v bag=%v full=%v err=%v", cfg, bag, full, err)
	}
	if n := len(encodeUnsampledCarrier(sc)); n != 34 { // 25 bytes as unpadded base64
		t.Errorf("unsampled carrier is %d characters, want 34", n)
	}
}

// Malformed carriers must fail cleanly (no panic) with an empty config, so the server starts a new
// trace, as it does with wrapper JSON that does not parse.  Covers broken base64 text and broken
// binary records (sent as valid base64).
func TestCarrierMalformed(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1})
	good := encodeCarrier(sc, map[string]string{"k": "v", "__seq": "1"})
	raw, err := carrierEncoding.DecodeString(good)
	if err != nil {
		t.Fatal(err)
	}
	enc := func(b []byte) string { return carrierEncoding.EncodeToString(b) }
	cases := map[string]string{
		"empty":            "",
		"not base64":       "%%%%" + good[4:],
		"padded base64":    good + "==",
		"one byte":         enc(raw[:1]),
		"unknown version":  enc(append([]byte{0x07}, raw[1:]...)),
		"no flags":         enc(raw[:25]),
		"truncated":        enc(raw[:len(raw)-1]),
		"huge length":      enc(append(append([]byte(nil), raw[:26]...), 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01)),
		"huge entry count": enc(append(append([]byte(nil), raw[:27]...), 0xff, 0xff, 0x03)),
		"trailing bytes":   enc(append(append([]byte(nil), raw...), 0x00)),
		"RawURL base64":    base64.RawURLEncoding.EncodeToString(raw), // the alphabet before header.go
	}
	for name, bad := range cases {
		cfg, _, _, err := decodeCarrier(bad)
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if cfg.TraceID.IsValid() || cfg.SpanID.IsValid() {
			t.Errorf("%s: expected an empty config, got %+v", name, cfg)
		}
	}
}

// How much smaller the compact carrier is, for the record (go test -v -run CarrierSize).
func TestCarrierSize(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: 1})
	for _, bag := range []map[string]string{{}, {"__seq": "3"}} {
		t.Logf("baggage %v: JSON %d bytes, compact %d characters", bag,
			len(backend.EncodeTraceCarrier(sc, bag)), len(encodeCarrier(sc, bag)))
	}
}
