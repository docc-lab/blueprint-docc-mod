package main

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type batchJob struct {
	first uint64
	count int
}
type phaseResult struct {
	Type              string    `json:"type"`
	Phase             int       `json:"phase"`
	Kind              string    `json:"kind"`
	TargetRate        float64   `json:"target_spans_per_second"`
	TargetSpans       *uint64   `json:"target_spans,omitempty"`
	UnissuedSpans     uint64    `json:"scheduler_unissued_spans"`
	Started           time.Time `json:"started"`
	Ended             time.Time `json:"ended"`
	GenerationSeconds float64   `json:"generation_seconds"`
	DrainSeconds      float64   `json:"drain_seconds"`
	ElapsedSeconds    float64   `json:"elapsed_seconds"`
	AcknowledgedRate  float64   `json:"acknowledged_spans_per_second"`
	Interrupted       bool      `json:"interrupted"`
	counts
	Endpoints []endpointCounts `json:"endpoints"`
}

func waitUntil(ctx context.Context, when time.Time) bool {
	if ctx.Err() != nil {
		return false
	}
	d := time.Until(when)
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return ctx.Err() == nil
	}
}

func doExport(ctx context.Context, c config, client sender, builder *batchBuilder, j batchJob, s *counters) {
	if ctx.Err() != nil {
		s.unsent.Add(uint64(j.count))
		return
	}
	req, cp := builder.build(j.first, j.count)
	s.attempted.Add(uint64(j.count))
	s.requests.Add(1)
	s.checkpoints.Add(cp)
	s.recordPayloads(builder.payloadCounts)
	rpcCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	start := time.Now()
	resp, size, err := client.Export(rpcCtx, req)
	s.rpcNanos.Add(uint64(time.Since(start)))
	cancel()
	s.bytes.Add(uint64(size))
	if err != nil {
		s.failed.Add(uint64(j.count))
		s.recordError(errorClass(err), err)
		return
	}
	if resp == nil {
		s.failed.Add(uint64(j.count))
		s.recordError("invalid_response", fmt.Errorf("nil OTLP response"))
		return
	}
	rejected := resp.GetPartialSuccess().GetRejectedSpans()
	if rejected < 0 || rejected > int64(j.count) {
		s.failed.Add(uint64(j.count))
		s.recordError("invalid_response", fmt.Errorf("invalid rejected_spans %d for %d spans", rejected, j.count))
		return
	}
	s.rejected.Add(uint64(rejected))
	s.acknowledged.Add(uint64(j.count) - uint64(rejected))
	if rejected > 0 {
		s.recordError("partial_success", fmt.Errorf("OTLP rejected %d spans: %s", rejected, resp.GetPartialSuccess().GetErrorMessage()))
	}
}

// Tomislav-RetCtx: finite rates use one open-loop scheduler. Full queues drop
// scheduled work visibly instead of silently slowing the offered-rate ramp.
// At rate 0 workers send as fast as synchronous exports permit (no queue).
func runPhase(parent context.Context, c config, p profile, clients []sender, prefix [8]byte, sequence *atomic.Uint64, phase int, kind string, rate float64, duration time.Duration, progress func(phaseResult)) phaseResult {
	// Allocate templates before starting the measurement clock.
	builders := make([][]*batchBuilder, len(clients))
	for i := range clients {
		for worker := 0; worker < c.Workers; worker++ {
			builders[i] = append(builders[i], newBatchBuilder(c, p, prefix))
		}
	}
	var target *uint64
	window := duration
	if rate > 0 {
		quota := uint64(math.Floor(rate * duration.Seconds()))
		if c.Spans > 0 && c.Spans < quota {
			quota = c.Spans
			window = time.Duration(float64(quota) / rate * float64(time.Second))
		}
		target = &quota
	}
	started := time.Now()
	genCtx, stopGeneration := context.WithTimeout(parent, duration)
	defer stopGeneration()
	exportCtx, stopExport := context.WithCancel(context.Background())
	defer stopExport()
	stats := make([]*counters, len(clients))
	queues := make([]chan batchJob, len(clients))
	for i := range clients {
		stats[i] = &counters{}
		stats[i].payload.Histogram = append([]payloadBucket(nil), p.payloadBuckets...)
		if rate > 0 {
			queues[i] = make(chan batchJob, c.Queue)
		}
	}
	var workers sync.WaitGroup
	var allocated atomic.Uint64
	for endpoint, client := range clients {
		for worker := 0; worker < c.Workers; worker++ {
			workers.Add(1)
			go func(ep int, client sender, builder *batchBuilder) {
				defer workers.Done()
				s := stats[ep]
				if rate > 0 {
					for j := range queues[ep] {
						doExport(exportCtx, c, client, builder, j, s)
					}
					return
				}
				for genCtx.Err() == nil {
					n := c.Batch
					if c.Spans > 0 {
						start := allocated.Add(uint64(n)) - uint64(n)
						if start >= c.Spans {
							return
						}
						if uint64(n) > c.Spans-start {
							n = int(c.Spans - start)
						}
					}
					first := sequence.Add(uint64(n)) - uint64(n)
					s.offered.Add(uint64(n))
					doExport(exportCtx, c, client, builder, batchJob{first, n}, s)
				}
			}(endpoint, client, builders[endpoint][worker])
		}
	}
	result := func(recordType string, generationEnd time.Time) phaseResult {
		now := time.Now()
		total, list := snapshots(c.Endpoints, stats)
		if generationEnd.IsZero() {
			generationEnd = now
		}
		elapsed := now.Sub(started).Seconds()
		r := phaseResult{Type: recordType, Phase: phase, Kind: kind, TargetRate: rate, Started: started.UTC(), Ended: now.UTC(), GenerationSeconds: generationEnd.Sub(started).Seconds(), DrainSeconds: now.Sub(generationEnd).Seconds(), ElapsedSeconds: elapsed, Interrupted: parent.Err() != nil, counts: total, Endpoints: list}
		r.TargetSpans = target
		if recordType == "phase" && target != nil && total.Offered < *target {
			r.UnissuedSpans = *target - total.Offered
		}
		if elapsed > 0 {
			r.AcknowledgedRate = float64(total.Acknowledged) / elapsed
		}
		return r
	}
	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		if c.Report <= 0 || progress == nil {
			return
		}
		t := time.NewTicker(c.Report)
		defer t.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-t.C:
				progress(result("progress", time.Time{}))
			}
		}
	}()

	generationDone := make(chan time.Time, 1)
	if rate > 0 {
		go func() {
			defer func() {
				for _, q := range queues {
					close(q)
				}
				generationDone <- time.Now()
			}()
			quota := *target
			batch := min(c.Batch, max(1, int(math.Ceil(rate/10)))) // about 100ms per burst, at least one span
			var issued uint64
			index := 0
			for issued < quota && genCtx.Err() == nil {
				n := int(min(uint64(batch), quota-issued))
				due := started.Add(time.Duration(float64(issued) / rate * float64(time.Second)))
				if !waitUntil(genCtx, due) {
					break
				}
				first := sequence.Add(uint64(n)) - uint64(n)
				ep := index % len(clients)
				index++
				stats[ep].offered.Add(uint64(n))
				select {
				case queues[ep] <- batchJob{first, n}:
				default:
					stats[ep].queueDropped.Add(uint64(n))
				}
				issued += uint64(n)
			}
			waitUntil(genCtx, started.Add(window))
		}()
	}
	workersDone := make(chan struct{})
	go func() { workers.Wait(); close(workersDone) }()
	var generationEnd time.Time
	if rate == 0 {
		// A span limit may finish an unpaced phase earlier than its duration.
		select {
		case <-workersDone:
		case <-genCtx.Done():
		}
		generationEnd = time.Now()
	} else {
		generationEnd = <-generationDone
	}
	// Outstanding RPCs use the drain context, not the generation deadline.
	drainTimer := time.AfterFunc(c.Drain, stopExport)
	<-workersDone
	drainTimer.Stop()
	close(stopProgress)
	<-progressDone
	r := result("phase", generationEnd)
	return r
}
