package otelcol

import (
	"bufio"
	"encoding/binary"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Tomislav-RetCtx: the refused-trace census dedups per trace, upgrades LP->HP with a second
// record, and serves append-only records with a from offset.
func TestRefusedTraceIDsCensus(t *testing.T) {
	saved := refusedIDs
	refusedIDs = newRefusedTraceIDs(true)
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

// Tomislav-RetCtx (2026-09-24): memory stays bounded under sustained refusal; counts stay exact
// within the dedup window; the per-ID log is off by default.
func TestRefusedTraceIDsBounded(t *testing.T) {
	saved := refusedIDs
	refusedIDs = newRefusedTraceIDs(false)
	defer func() { refusedIDs = saved }()
	batch := func(from, n int) []*tracepb.ResourceSpans {
		spans := make([]*tracepb.Span, 0, n)
		for i := from; i < from+n; i++ {
			id := make([]byte, 16)
			binary.BigEndian.PutUint64(id[8:], uint64(i))
			spans = append(spans, &tracepb.Span{TraceId: id, SpanId: make([]byte, 8)})
		}
		return []*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}}}}
	}
	const total = 3 * refusedDedupCap
	for i := 0; i < total; i += 4096 {
		recordRefused(batch(i, 4096), true)
		if held := len(refusedIDs.cur) + len(refusedIDs.prev); held > 2*refusedDedupCap {
			t.Fatalf("dedup holds %d trace IDs (bound %d)", held, 2*refusedDedupCap)
		}
	}
	_, records, sum := refusedIDs.snapshot(0)
	if records != 0 || sum["traces_any"] != total || sum["traces_hp"] != total || sum["spans_all"] != total {
		t.Fatalf("records %d summary %v", records, sum)
	}
	// a trace refused again inside the window is not counted twice, even across one rotation
	recordRefused(batch(total-1, 1), true)
	refusedIDs.rotated = time.Now().Add(-2 * refusedDedupWindow)
	recordRefused(batch(total-1, 1), true)
	if _, _, sum := refusedIDs.snapshot(0); sum["traces_any"] != total || sum["spans_all"] != total+2 {
		t.Fatalf("dedup across rotation: %v", sum)
	}
}

// Tomislav-RetCtx: with a record file, records are appended there (17 bytes each), not kept in memory.
func TestRefusedTraceIDsFile(t *testing.T) {
	saved := refusedIDs
	defer func() { refusedIDs = saved }()
	path := t.TempDir() + "/refused.bin"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	refusedIDs = newRefusedTraceIDs(false)
	refusedIDs.file = bufio.NewWriter(f)
	id := make([]byte, 16)
	id[15] = 7
	recordRefused([]*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{{TraceId: id, SpanId: make([]byte, 8)}}}}}}, true)
	_ = refusedIDs.file.Flush()
	b, _ := os.ReadFile(path)
	if len(b) != refusedRecordSize || b[15] != 7 || b[16] != refusedFlagHP|refusedFlagAny || len(refusedIDs.records) != 0 {
		t.Fatalf("file %x records %d", b, len(refusedIDs.records))
	}
}

// Tomislav-RetCtx: census off (the default): span counters only, no per-trace state, no records.
func TestRefusedTraceIDsCensusOff(t *testing.T) {
	saved := refusedIDs
	defer func() { refusedIDs = saved }()
	refusedIDs = newRefusedTraceIDs(false)
	refusedIDs.census = false
	id := make([]byte, 16)
	spans := []*tracepb.Span{{TraceId: id, SpanId: make([]byte, 8)}, {TraceId: id, SpanId: make([]byte, 8)}}
	recordRefused([]*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}}}}, true)
	recordRefused([]*tracepb.ResourceSpans{{ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}}}}, false)
	_, records, sum := refusedIDs.snapshot(0)
	if records != 0 || len(refusedIDs.cur)+len(refusedIDs.prev) != 0 || sum["spans_all"] != 4 || sum["spans_hp"] != 2 || sum["traces_any"] != 0 || sum["census"] != 0 {
		t.Fatalf("census off: records %d dedup %d summary %v", records, len(refusedIDs.cur)+len(refusedIDs.prev), sum)
	}
}
