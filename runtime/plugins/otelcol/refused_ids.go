// Tomislav-RetCtx: census of the trace IDs whose spans the collector refused.
//
// Every SDK-side processor drops a batch when the agent collector answers with an error
// (memory_limiter / priority processor refusal; OTLP_RETRY=off). At that moment the batch is
// still in hand, so this records, per trace ID, whether a high-priority span (a checkpoint or a
// promoted reverse-truss carrier) and/or any span of that trace was lost. The campaign runner
// fetches the records over HTTP (:9464 by default) before and after each measured point and
// takes the union across all services: the exact set of traces that lost a checkpoint (the
// bridges' reconstruction-ineligible set) and the exact set that lost any span (vanilla's
// incomplete set), with no trace sampling involved. Everything downstream of the agents is
// verified loss-free from collector counters (send_failed, enqueue_failed), so this census is
// complete. Records are append-only so a "from" offset gives the delta between two fetches.
package otelcol

import (
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"

	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

const (
	refusedFlagHP     uint8 = 1  // a high-priority (checkpoint / carrier) span of this trace was refused
	refusedFlagAny    uint8 = 2  // any span of this trace was refused
	refusedRecordSize       = 17 // 16-byte trace ID + 1 flag byte
)

type refusedRecord struct {
	id    [16]byte
	flags uint8
}

// RefusedTraceIDs is an append-only log of (trace ID, flags) with per-trace dedup.
type RefusedTraceIDs struct {
	mu        sync.Mutex
	seen      map[[16]byte]uint8
	records   []refusedRecord
	tracesHP  int
	tracesAny int
	spansHP   int64
	spansAll  int64
}

var refusedIDs = &RefusedTraceIDs{seen: map[[16]byte]uint8{}}

// recordRefused notes every trace ID in a dropped batch. isHP says the batch carried
// high-priority spans (for vanilla every span is trace-critical, so callers pass true).
func recordRefused(events []*tracepb.ResourceSpans, isHP bool) {
	flag := refusedFlagAny
	if isHP {
		flag |= refusedFlagHP
	}
	r := refusedIDs
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rs := range events {
		if rs == nil {
			continue
		}
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				if len(s.TraceId) != 16 {
					continue
				}
				r.spansAll++
				if isHP {
					r.spansHP++
				}
				var id [16]byte
				copy(id[:], s.TraceId)
				have := r.seen[id]
				if have&flag == flag {
					continue // nothing new for this trace
				}
				if have == 0 {
					r.tracesAny++
				}
				if isHP && have&refusedFlagHP == 0 {
					r.tracesHP++
				}
				r.seen[id] = have | flag
				r.records = append(r.records, refusedRecord{id: id, flags: flag})
			}
		}
	}
}

func (r *RefusedTraceIDs) snapshot(from int) (recs []refusedRecord, total int, summary map[string]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total = len(r.records)
	if from < 0 {
		from = 0
	}
	if from > total {
		from = total
	}
	recs = r.records[from:total] // append-only: the backing array keeps these elements valid
	summary = map[string]int64{
		"records": int64(total), "traces_hp": int64(r.tracesHP), "traces_any": int64(r.tracesAny),
		"spans_hp": r.spansHP, "spans_all": r.spansAll,
	}
	return
}

func serveRefused(w http.ResponseWriter, req *http.Request) {
	from, _ := strconv.Atoi(req.URL.Query().Get("from"))
	recs, total, summary := refusedIDs.snapshot(from)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Retctx-Records", strconv.Itoa(total))
	w.Header().Set("X-Retctx-From", strconv.Itoa(total-len(recs)))
	for k, v := range summary {
		w.Header().Set("X-Retctx-"+k, strconv.FormatInt(v, 10))
	}
	buf := make([]byte, 0, len(recs)*refusedRecordSize)
	for _, rec := range recs {
		buf = append(buf, rec.id[:]...)
		buf = append(buf, rec.flags)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(buf)))
	_, _ = w.Write(buf)
}

func serveRefusedSummary(w http.ResponseWriter, _ *http.Request) {
	_, _, summary := refusedIDs.snapshot(0)
	summary["record_size"] = refusedRecordSize
	summary["flag_hp"] = int64(refusedFlagHP)
	summary["flag_any"] = int64(refusedFlagAny)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

var _ = binary.LittleEndian // records are raw bytes; kept for readers that want an explicit order

func init() {
	port := os.Getenv("RETCTX_REFUSED_PORT")
	if port == "" {
		port = "9464"
	}
	if port == "0" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/retctx/refused", serveRefused)
	mux.HandleFunc("/retctx/refused/summary", serveRefusedSummary)
	go func() {
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			slog.Warn("retctx refused-trace census endpoint not started", "port", port, "error", err)
		}
	}()
}
