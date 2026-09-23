package otelcol

import (
	"net/http/httptest"
	"testing"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Tomislav-RetCtx: the refused-trace census dedups per trace, upgrades LP->HP with a second
// record, and serves append-only records with a from offset.
func TestRefusedTraceIDsCensus(t *testing.T) {
	saved := refusedIDs
	refusedIDs = &RefusedTraceIDs{seen: map[[16]byte]uint8{}}
	defer func() { refusedIDs = saved }()
	mk := func(ids ...byte) []*tracepb.ResourceSpans {
		var spans []*tracepb.Span
		for _, b := range ids {
			id := make([]byte, 16)
			id[15] = b
			spans = append(spans, &tracepb.Span{TraceId: id, SpanId: make([]byte, 8)})
		}
		return []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}}}}
	}
	recordRefused(mk(1, 1, 2), false) // LP batch: traces 1 (twice) and 2
	recordRefused(mk(2, 3), true)     // HP batch: trace 2 upgrades, trace 3 new
	recs, total, sum := refusedIDs.snapshot(0)
	if total != 4 || len(recs) != 4 {
		t.Fatalf("records: got %d want 4 (1:any, 2:any, 2:hp, 3:hp)", total)
	}
	if sum["traces_any"] != 3 || sum["traces_hp"] != 2 || sum["spans_all"] != 5 || sum["spans_hp"] != 2 {
		t.Fatalf("summary %v", sum)
	}
	if recs[2].id[15] != 2 || recs[2].flags != refusedFlagHP|refusedFlagAny {
		t.Fatalf("upgrade record %+v", recs[2])
	}
	// second fetch from the offset of the first sees only the delta
	recordRefused(mk(4), true)
	delta, total2, _ := refusedIDs.snapshot(total)
	if total2 != 5 || len(delta) != 1 || delta[0].id[15] != 4 {
		t.Fatalf("delta %v total %d", delta, total2)
	}
	rr := httptest.NewRecorder()
	serveRefused(rr, httptest.NewRequest("GET", "/retctx/refused?from=4", nil))
	if rr.Header().Get("X-Retctx-Records") != "5" || rr.Body.Len() != refusedRecordSize {
		t.Fatalf("http: records header %q body %d", rr.Header().Get("X-Retctx-Records"), rr.Body.Len())
	}
	body := rr.Body.Bytes()
	if body[15] != 4 || body[16] != refusedFlagHP|refusedFlagAny {
		t.Fatalf("http body %x", body)
	}
}
