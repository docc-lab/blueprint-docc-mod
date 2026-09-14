package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Tomislav-RetCtx: acknowledged means the OTLP response accepted the input;
// only collector exporter metrics can establish downstream export throughput.
type counters struct {
	offered      atomic.Uint64
	queueDropped atomic.Uint64
	unsent       atomic.Uint64
	attempted    atomic.Uint64
	acknowledged atomic.Uint64
	rejected     atomic.Uint64
	failed       atomic.Uint64
	checkpoints  atomic.Uint64
	requests     atomic.Uint64
	bytes        atomic.Uint64
	rpcNanos     atomic.Uint64
	mu           sync.Mutex
	errors       map[string]uint64
	firstError   string
	payload      payloadStats
}

type payloadStats struct {
	Count     uint64          `json:"count"`
	Bytes     uint64          `json:"total_bytes"`
	Min       int             `json:"min_bytes"`
	Max       int             `json:"max_bytes"`
	Mean      float64         `json:"mean_bytes"`
	Histogram []payloadBucket `json:"histogram"`
}

type counts struct {
	Offered      uint64            `json:"offered_spans"`
	QueueDropped uint64            `json:"queue_dropped_spans"`
	Unsent       uint64            `json:"unsent_spans"`
	Attempted    uint64            `json:"attempted_spans"`
	Acknowledged uint64            `json:"acknowledged_spans"`
	Rejected     uint64            `json:"rejected_spans"`
	Failed       uint64            `json:"failed_spans"`
	Checkpoints  uint64            `json:"attempted_checkpoint_spans"`
	Requests     uint64            `json:"export_requests"`
	ProtoBytes   uint64            `json:"attempted_protobuf_bytes"`
	RPCSeconds   float64           `json:"sum_rpc_seconds"`
	Errors       map[string]uint64 `json:"error_counts,omitempty"`
	FirstError   string            `json:"first_error,omitempty"`
	Payload      *payloadStats     `json:"attempted_checkpoint_payload,omitempty"`
}

// Tomislav-RetCtx: merge once per batch, with bounded, precomputed buckets.
// Size accounting tracks attempted checkpoint values, including failed RPCs;
// a partial-success response does not identify which individual sizes survived.
func (s *counters) recordPayloads(batch batchPayloadCounts) {
	if batch.Count == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.payload.Count == 0 {
		s.payload.Min, s.payload.Max = batch.Min, batch.Max
	}
	s.payload.Min = min(s.payload.Min, batch.Min)
	s.payload.Max = max(s.payload.Max, batch.Max)
	s.payload.Count += batch.Count
	s.payload.Bytes += batch.Bytes
	for i, n := range batch.Buckets {
		s.payload.Histogram[i].Count += n
	}
}

func (s *counters) recordError(class string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.errors == nil {
		s.errors = make(map[string]uint64)
	}
	s.errors[class]++
	if s.firstError == "" {
		s.firstError = fmt.Sprintf("%.1024s", err.Error())
	}
}

func (s *counters) snapshot() counts {
	r := counts{Offered: s.offered.Load(), QueueDropped: s.queueDropped.Load(), Unsent: s.unsent.Load(), Attempted: s.attempted.Load(), Acknowledged: s.acknowledged.Load(), Rejected: s.rejected.Load(), Failed: s.failed.Load(), Checkpoints: s.checkpoints.Load(), Requests: s.requests.Load(), ProtoBytes: s.bytes.Load(), RPCSeconds: float64(s.rpcNanos.Load()) / float64(time.Second)}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.errors {
		if r.Errors == nil {
			r.Errors = make(map[string]uint64)
		}
		r.Errors[k] = v
	}
	r.FirstError = s.firstError
	if s.payload.Count > 0 {
		value := s.payload
		value.Mean = float64(value.Bytes) / float64(value.Count)
		value.Histogram = append([]payloadBucket(nil), s.payload.Histogram...)
		r.Payload = &value
	}
	return r
}

func (s counts) problems() bool {
	return s.QueueDropped+s.Unsent+s.Rejected+s.Failed > 0 || len(s.Errors) > 0
}
func (s counts) validate() error {
	if s.Offered != s.Attempted+s.QueueDropped+s.Unsent {
		return fmt.Errorf("accounting: offered %d != attempted + queue drops + unsent (%d)", s.Offered, s.Attempted+s.QueueDropped+s.Unsent)
	}
	if s.Attempted != s.Acknowledged+s.Rejected+s.Failed {
		return fmt.Errorf("accounting: attempted %d != acknowledged + rejected + failed (%d)", s.Attempted, s.Acknowledged+s.Rejected+s.Failed)
	}
	if s.Checkpoints > 0 {
		if s.Payload == nil || s.Payload.Count != s.Checkpoints {
			return fmt.Errorf("accounting: checkpoint size count must equal %d attempted checkpoints", s.Checkpoints)
		}
		var count uint64
		for _, b := range s.Payload.Histogram {
			count += b.Count
		}
		if count != s.Checkpoints {
			return fmt.Errorf("accounting: histogram contains %d checkpoints, want %d", count, s.Checkpoints)
		}
	}
	return nil
}

type endpointCounts struct {
	Endpoint string `json:"endpoint"`
	counts
}

func snapshots(endpoints []string, values []*counters) (counts, []endpointCounts) {
	var total counts
	list := make([]endpointCounts, len(values))
	for i, v := range values {
		s := v.snapshot()
		list[i] = endpointCounts{endpoints[i], s}
		total.Offered += s.Offered
		total.QueueDropped += s.QueueDropped
		total.Unsent += s.Unsent
		total.Attempted += s.Attempted
		total.Acknowledged += s.Acknowledged
		total.Rejected += s.Rejected
		total.Failed += s.Failed
		total.Checkpoints += s.Checkpoints
		total.Requests += s.Requests
		total.ProtoBytes += s.ProtoBytes
		total.RPCSeconds += s.RPCSeconds
		if s.Payload != nil {
			if total.Payload == nil {
				value := *s.Payload
				value.Histogram = append([]payloadBucket(nil), s.Payload.Histogram...)
				total.Payload = &value
			} else {
				total.Payload.Count += s.Payload.Count
				total.Payload.Bytes += s.Payload.Bytes
				total.Payload.Min = min(total.Payload.Min, s.Payload.Min)
				total.Payload.Max = max(total.Payload.Max, s.Payload.Max)
				for i, b := range s.Payload.Histogram {
					total.Payload.Histogram[i].Count += b.Count
				}
			}
		}
		for k, v := range s.Errors {
			if total.Errors == nil {
				total.Errors = make(map[string]uint64)
			}
			total.Errors[k] += v
		}
		if total.FirstError == "" {
			total.FirstError = s.FirstError
		}
	}
	if total.Payload != nil {
		total.Payload.Mean = float64(total.Payload.Bytes) / float64(total.Payload.Count)
	}
	return total, list
}
