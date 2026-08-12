package payloadbench

// Optional pprof/diagnostics endpoint, OFF unless PPROF_ADDR is set.
//
// Why this exists: a CPU profile (perf) shows where cycles are SPENT, not where
// goroutines are BLOCKED. A serialization point that spends its time waiting on a
// channel, mutex or network read is invisible to perf. To identify a per-process
// throughput bottleneck you need Go's own profiles:
//
//	goroutine — where all N goroutines are parked at saturation (the smoking gun)
//	block     — cumulative time blocked on channel/select/mutex/network
//	mutex     — contended mutex holders
//
// Enable per-pod without a rebuild:
//	kubectl set env deploy/<svc> PPROF_ADDR=:6060 \
//	  PPROF_BLOCK_RATE=10000 PPROF_MUTEX_FRACTION=100
// then `kubectl port-forward <pod> 6060:6060` and fetch /debug/pprof/*.
//
// Rates default to OFF (0) because block/mutex profiling perturbs the very
// measurement we care about; enable them only for diagnosis runs, never for a
// throughput or latency campaign.

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on http.DefaultServeMux
	"os"
	"runtime"
	"strconv"
)

func init() {
	addr := os.Getenv("PPROF_ADDR")
	if addr == "" {
		return
	}
	if r, err := strconv.Atoi(os.Getenv("PPROF_BLOCK_RATE")); err == nil && r > 0 {
		// Sample a blocking event every r nanoseconds of blocked time.
		runtime.SetBlockProfileRate(r)
		slog.Info("pprof: block profiling enabled", "rate_ns", r)
	}
	if f, err := strconv.Atoi(os.Getenv("PPROF_MUTEX_FRACTION")); err == nil && f > 0 {
		// Report 1/f of mutex contention events.
		runtime.SetMutexProfileFraction(f)
		slog.Info("pprof: mutex profiling enabled", "fraction", f)
	}
	go func() {
		slog.Info("pprof: serving diagnostics", "addr", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Error("pprof: server exited", "err", err)
		}
	}()
}
