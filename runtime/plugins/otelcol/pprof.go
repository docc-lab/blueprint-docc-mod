package otelcol

// Tomislav-RetCtx: opt-in CPU profiling for a deployed service.
//
// Why this exists: the cluster says the response path costs the application
// ~500 us/req of marginal CPU (a linear fit over offered 500..5000 gives
// 3183 us/req with REVERSE_TRUSS=off against 3683 with it on, R^2 > 0.97), but
// no microbenchmark of the reverse code reproduces that. Measured per span, the
// reverse wrapper is under 1 us, and even charging every Attributes() call its
// full 452 ns only reaches ~60 us/req. Something in the deployed path costs an
// order of magnitude more than the sum of its parts, and attributing it needs a
// profile of a real service under load rather than another hypothesis.
//
// There is no perf on the cluster nodes and the generated services expose no
// debug endpoint, so this adds one. It is inert unless BRIDGES_PPROF is set, so
// a measurement run is bit-identical to a normal one:
//
//	BRIDGES_PPROF=:6060   listen on :6060 in every service process
//
// Then, against a pod:
//
//	kubectl -n dsb-sn port-forward <pod> 6060:6060
//	go tool pprof -seconds 30 http://localhost:6060/debug/pprof/profile
//
// Importing net/http/pprof for its side effect registers the handlers on
// http.DefaultServeMux. That is a package-level effect, which is why the
// listener itself stays behind the env var: without it nothing is served and no
// port is opened.

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
)

// PprofEnv names the listen address for the debug server, e.g. ":6060".
const PprofEnv = "BRIDGES_PPROF"

func init() {
	address := os.Getenv(PprofEnv)
	if address == "" {
		return
	}
	go func() {
		// Own mux would miss the pprof handlers, which register on the default
		// one; this process is a measurement target, not a public service.
		server := &http.Server{Addr: address, Handler: http.DefaultServeMux}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("pprof listener stopped", "addr", address, "err", err)
		}
	}()
	slog.Info("pprof listener started", "addr", address)
}
