package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
)

var version = "development"

type manifest struct {
	Type          string    `json:"type"`
	Version       string    `json:"version"`
	GoVersion     string    `json:"go_version"`
	CPUs          int       `json:"visible_cpus"`
	GoMaxProcs    int       `json:"gomaxprocs"`
	Created       time.Time `json:"created"`
	RunID         string    `json:"run_id"`
	Args          []string  `json:"args"`
	Config        config    `json:"config"`
	Profile       profile   `json:"resolved_profile"`
	ProfileSHA256 string    `json:"profile_sha256"`
}

type jsonLog struct {
	mu      sync.Mutex
	encoder *json.Encoder
	err     error
	cancel  context.CancelFunc
}

func (l *jsonLog) write(v any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return
	}
	l.err = l.encoder.Encode(v)
	if l.err != nil {
		l.cancel()
	}
}

type metricSnapshot struct {
	Endpoint string    `json:"endpoint"`
	URL      string    `json:"url"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	File     string    `json:"file,omitempty"`
	SHA256   string    `json:"sha256,omitempty"`
	Error    string    `json:"error,omitempty"`
}

func scrapeMetrics(ctx context.Context, c config, phase int, position string) []metricSnapshot {
	results := make([]metricSnapshot, len(c.Metrics))
	tlsConfig, tlsErr := tlsOptions(c.CAFile)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var wg sync.WaitGroup
	for i, u := range c.Metrics {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			s := metricSnapshot{Endpoint: c.Endpoints[i], URL: u, Started: time.Now().UTC()}
			defer func() { s.Finished = time.Now().UTC(); results[i] = s }()
			if tlsErr != nil {
				s.Error = tlsErr.Error()
				return
			}
			reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
			if err != nil {
				s.Error = err.Error()
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				s.Error = err.Error()
				return
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
			if err != nil {
				s.Error = err.Error()
				return
			}
			if resp.StatusCode != http.StatusOK {
				s.Error = fmt.Sprintf("metrics HTTP status %d", resp.StatusCode)
				return
			}
			if len(data) > 8<<20 {
				s.Error = "metrics response exceeds 8 MiB"
				return
			}
			s.File = fmt.Sprintf("%03d-%s-%02d.prom", phase, position, i)
			if err = os.WriteFile(filepath.Join(c.Out, s.File), data, 0644); err != nil {
				s.Error = err.Error()
				return
			}
			digest := sha256.Sum256(data)
			s.SHA256 = hex.EncodeToString(digest[:])
		}(i, u)
	}
	wg.Wait()
	return results
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	c, err := parseConfig(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "spanload:", err)
		return 2
	}
	p, err := loadProfile(c)
	if err != nil {
		fmt.Fprintln(stderr, "spanload:", err)
		return 2
	}
	if _, err = tlsOptions(c.CAFile); err != nil {
		fmt.Fprintln(stderr, "spanload:", err)
		return 2
	}
	var prefix [8]byte
	if _, err = rand.Read(prefix[:]); err != nil {
		fmt.Fprintln(stderr, "spanload:", err)
		return 1
	}
	prefix[0] |= 1
	profileJSON, _ := json.Marshal(p)
	digest := sha256.Sum256(profileJSON)
	m := manifest{Type: "manifest", Version: version, GoVersion: runtime.Version(), CPUs: runtime.NumCPU(), GoMaxProcs: runtime.GOMAXPROCS(0), Created: time.Now().UTC(), RunID: hex.EncodeToString(prefix[:]), Args: args, Config: c, Profile: p, ProfileSHA256: hex.EncodeToString(digest[:])}
	if c.Out != "" {
		if err = os.MkdirAll(filepath.Dir(c.Out), 0755); err == nil {
			err = os.Mkdir(c.Out, 0755)
		}
		if err != nil {
			fmt.Fprintln(stderr, "spanload: output directory must be new:", err)
			return 2
		}
		data, _ := json.MarshalIndent(m, "", "  ")
		if err = os.WriteFile(filepath.Join(c.Out, "manifest.json"), append(data, '\n'), 0644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		f, err := os.OpenFile(filepath.Join(c.Out, "results.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer f.Close()
		stdout = io.MultiWriter(stdout, f)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	log := &jsonLog{encoder: json.NewEncoder(stdout), cancel: cancel}
	log.write(m)
	if c.DryRun {
		request, _ := newBatchBuilder(c, p, prefix).build(0, min(2, c.Batch))
		data, err := protojson.Marshal(request)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		log.write(map[string]any{"type": "sample", "otlp": json.RawMessage(data)})
		if log.err != nil {
			fmt.Fprintln(stderr, log.err)
			return 1
		}
		return 0
	}
	clients := make([]sender, 0, len(c.Endpoints))
	defer func() {
		for _, client := range clients {
			client.Close()
		}
	}()
	for _, endpoint := range c.Endpoints {
		client, err := newSender(c, endpoint)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		clients = append(clients, client)
	}
	var sequence atomic.Uint64
	phase := 0
	problems := false
	for _, rate := range c.Rates {
		for _, kind := range []string{"warmup", "measure"} {
			duration := c.Duration
			if kind == "warmup" {
				duration = c.Warmup
			}
			if duration == 0 {
				continue
			}
			if runCtx.Err() != nil {
				break
			}
			phase++
			if len(c.Metrics) > 0 {
				values := scrapeMetrics(runCtx, c, phase, "before")
				log.write(map[string]any{"type": "collector_metrics", "phase": phase, "position": "before", "snapshots": values})
				for _, v := range values {
					problems = problems || v.Error != ""
				}
			}
			fmt.Fprintf(stderr, "spanload: %s phase %d, aggregate %.0f spans/s, %d endpoints, %s\n", kind, phase, rate, len(clients), duration)
			result := runPhase(runCtx, c, p, clients, prefix, &sequence, phase, kind, rate, duration, func(r phaseResult) { log.write(r) })
			log.write(result)
			if err = result.counts.validate(); err != nil {
				fmt.Fprintln(stderr, err)
				problems = true
			}
			problems = problems || result.counts.problems() || result.UnissuedSpans > 0
			if c.Settle > 0 {
				waitUntil(runCtx, time.Now().Add(c.Settle))
			}
			if len(c.Metrics) > 0 && runCtx.Err() == nil {
				values := scrapeMetrics(runCtx, c, phase, "after")
				log.write(map[string]any{"type": "collector_metrics", "phase": phase, "position": "after", "snapshots": values})
				for _, v := range values {
					problems = problems || v.Error != ""
				}
			}
			fmt.Fprintf(stderr, "spanload: phase %d acknowledged %d, rejected %d, failed %d, queue-dropped %d, unsent %d, scheduler-unissued %d; %.0f acknowledged spans/s including drain\n", phase, result.Acknowledged, result.Rejected, result.Failed, result.QueueDropped, result.Unsent, result.UnissuedSpans, result.AcknowledgedRate)
		}
		if runCtx.Err() != nil {
			break
		}
	}
	log.write(map[string]any{"type": "complete", "phases": phase, "problems": problems, "interrupted": runCtx.Err() != nil})
	if log.err != nil {
		fmt.Fprintln(stderr, log.err)
		return 1
	}
	if ctx.Err() != nil {
		return 130
	}
	if problems && !c.AllowErrors {
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
