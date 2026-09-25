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
//
// Tomislav-RetCtx (2026-09-24): memory is bounded. Unbounded per-trace state was a leak under
// sustained overload -- every refused trace ID stayed in a global map and slice for the pod's
// life (hotel no-work vanilla frontend: live heap 82 -> 931 MB over four back-to-back ramps
// against GOMEMLIMIT 1GiB, GC-bound by the fifth). The per-trace dedup is now a two-generation
// window (rotated every refusedDedupWindow or refusedDedupCap traces, whichever comes first; a
// trace's spans are refused within milliseconds of each other, so counts are unchanged in
// practice), and nothing else is retained: the per-ID records go to an append-only file only when
// RETCTX_REFUSED_RECORDS_FILE is set (buffered, flushed every second; for offline unions), or to
// memory only with RETCTX_REFUSED_RECORDS=on (tests / legacy fetch). The campaign reads the
// counters, which stay cumulative and exact.
//
// Tomislav-RetCtx (user 2026-09-24): the per-trace census is OFF by default ("no census stuff during these
// runs"): a refused batch then only adds its span counts (spans_all / spans_hp: one lock per batch, no map, no
// log). RETCTX_REFUSED_CENSUS=on restores the per-trace dedup and the traces_* counters for loss experiments.
package otelcol

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

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

// Dedup window bounds: at most 2*refusedDedupCap trace IDs are held at any time.
const (
	refusedDedupWindow = 5 * time.Second
	refusedDedupCap    = 1 << 16
)

// RefusedTraceIDs counts refused traces with a bounded per-trace dedup window and, optionally,
// an append-only log of (trace ID, flags).
type RefusedTraceIDs struct {
	mu          sync.Mutex
	census      bool               // per-trace dedup + traces_* counters (RETCTX_REFUSED_CENSUS=on)
	cur, prev   map[[16]byte]uint8 // two-generation dedup window
	rotated     time.Time
	keepRecords bool
	records     []refusedRecord
	file        *bufio.Writer // RETCTX_REFUSED_RECORDS_FILE: records appended here instead of memory
	tracesHP    int
	tracesAny   int
	spansHP     int64
	spansAll    int64
}

func newRefusedTraceIDs(keepRecords bool) *RefusedTraceIDs {
	return &RefusedTraceIDs{census: true, cur: map[[16]byte]uint8{}, prev: map[[16]byte]uint8{}, rotated: time.Now(), keepRecords: keepRecords}
}

var refusedIDs = func() *RefusedTraceIDs {
	r := newRefusedTraceIDs(os.Getenv("RETCTX_REFUSED_RECORDS") == "on")
	r.census = os.Getenv("RETCTX_REFUSED_CENSUS") == "on"
	if !r.census {
		r.keepRecords = false
		return r
	}
	if path := os.Getenv("RETCTX_REFUSED_RECORDS_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			slog.Warn("retctx refused-trace record file not opened", "path", path, "error", err)
			return r
		}
		r.file = bufio.NewWriterSize(f, 1<<16)
		go func() { // periodic flush; the buffer bounds memory, the file holds the log
			for range time.Tick(time.Second) {
				r.mu.Lock()
				_ = r.file.Flush()
				r.mu.Unlock()
			}
		}()
	}
	return r
}()

// rotate retires the older generation once the window has elapsed or the current one is full.
func (r *RefusedTraceIDs) rotate(now time.Time) {
	if now.Sub(r.rotated) < refusedDedupWindow && len(r.cur) < refusedDedupCap {
		return
	}
	r.prev, r.cur, r.rotated = r.cur, make(map[[16]byte]uint8, len(r.cur)/2), now
}

// recordRefused notes every trace ID in a dropped batch. isHP says the batch carried
// high-priority spans (for vanilla every span is trace-critical, so callers pass true).
func recordRefused(events []*tracepb.ResourceSpans, isHP bool) {
	flag := refusedFlagAny
	if isHP {
		flag |= refusedFlagHP
	}
	r := refusedIDs
	if !r.census { // counters only
		var n int64
		for _, rs := range events {
			if rs != nil {
				for _, ss := range rs.ScopeSpans {
					n += int64(len(ss.Spans))
				}
			}
		}
		r.mu.Lock()
		r.spansAll += n
		if isHP {
			r.spansHP += n
		}
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rotate(time.Now())
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
				have := r.cur[id] | r.prev[id]
				if have&flag == flag {
					if _, ok := r.cur[id]; !ok {
						r.cur[id] = have // keep a recurring trace in the live generation
					}
					continue // nothing new for this trace
				}
				if have == 0 {
					r.tracesAny++
				}
				if isHP && have&refusedFlagHP == 0 {
					r.tracesHP++
				}
				r.cur[id] = have | flag
				if r.keepRecords {
					r.records = append(r.records, refusedRecord{id: id, flags: flag})
				}
				if r.file != nil {
					_, _ = r.file.Write(id[:])
					_ = r.file.WriteByte(flag)
				}
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
		"spans_hp": r.spansHP, "spans_all": r.spansAll, "dedup_traces": int64(len(r.cur) + len(r.prev)),
		"census": map[bool]int64{false: 0, true: 1}[r.census],
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
