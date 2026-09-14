package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

//go:embed profiles/semconv10-example.json
var semconvExample []byte

type attributeSpec struct {
	Key   string          `json:"key"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type payloadTable struct {
	Source string                                `json:"source"`
	Sizes  map[string]map[string]json.RawMessage `json:"sizes"`
}

type profile struct {
	Attributes             []attributeSpec       `json:"span_attributes"`
	ResourceAttributes     []attributeSpec       `json:"resource_attributes"`
	SizeSource             string                `json:"checkpoint_size_source,omitempty"`
	CheckpointBytes        int                   `json:"checkpoint_bytes"`
	CheckpointSHA256       string                `json:"checkpoint_sha256,omitempty"`
	CheckpointDistribution *resolvedDistribution `json:"checkpoint_distribution,omitempty"`
	PayloadBufferSHA256    string                `json:"checkpoint_payload_buffer_sha256,omitempty"`
	OrdinaryBytes          int                   `json:"ordinary_bytes"`
	attrs                  []*commonpb.KeyValue
	resource               []*commonpb.KeyValue
	checkpoint             *commonpb.KeyValue
	ordinary               *commonpb.KeyValue
	distribution           *sizeDistribution
	payload                []byte
	payloadBuckets         []payloadBucket
}

func readJSON(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("JSON profile exceeds 4 MiB")
	}
	return decodeJSON(bytes.NewReader(data), target)
}

func decodeJSON(r io.Reader, target any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}

func loadProfile(c config) (profile, error) {
	p := profile{Attributes: []attributeSpec{}, ResourceAttributes: []attributeSpec{}}
	var err error
	switch c.Profile {
	case "semconv10-example":
		err = decodeJSON(bytes.NewReader(semconvExample), &p.Attributes)
	case "custom":
		err = readJSON(c.Attributes, &p.Attributes)
	}
	if err != nil {
		return p, fmt.Errorf("span attributes: %w", err)
	}
	if c.ResourceAttributes != "" {
		if err = readJSON(c.ResourceAttributes, &p.ResourceAttributes); err != nil {
			return p, fmt.Errorf("resource attributes: %w", err)
		}
	}
	if p.attrs, err = encodeAttributes(p.Attributes, true); err != nil {
		return p, err
	}
	if p.resource, err = encodeAttributes(p.ResourceAttributes, false); err != nil {
		return p, err
	}
	if c.Bridge == "none" {
		return p, nil
	}
	size := c.BridgeBytes
	p.SizeSource = "explicit --bridge-bytes"
	var distribution *distributionSpec
	if c.BridgeDistribution != "" {
		distribution = &distributionSpec{}
		if err = readJSON(c.BridgeDistribution, distribution); err != nil {
			return p, fmt.Errorf("checkpoint distribution: %w", err)
		}
	}
	if c.SizesFile != "" {
		var table payloadTable
		if err = readJSON(c.SizesFile, &table); err != nil {
			return p, fmt.Errorf("payload sizes: %w", err)
		}
		if table.Source == "" {
			return p, errors.New("payload sizes JSON requires a source description")
		}
		raw, ok := table.Sizes[c.Bridge][strconv.Itoa(c.CPD)]
		if !ok {
			return p, fmt.Errorf("payload sizes lack %s CPD %d", c.Bridge, c.CPD)
		}
		p.SizeSource = table.Source
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			distribution = &distributionSpec{}
			if err = decodeJSON(bytes.NewReader(raw), distribution); err != nil {
				return p, fmt.Errorf("payload size distribution: %w", err)
			}
			if distribution.Source == "" {
				distribution.Source = table.Source
			}
		} else {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return p, errors.New("checkpoint size cannot be null")
			}
			if err = json.Unmarshal(raw, &size); err != nil {
				return p, fmt.Errorf("checkpoint size must be an integer or distribution: %w", err)
			}
		}
	}
	if distribution != nil {
		if p.distribution, err = compileDistribution(*distribution); err != nil {
			return p, err
		}
		p.CheckpointDistribution = &p.distribution.resolved
		p.CheckpointBytes = -1 // variable; resolved support is in checkpoint_distribution
		p.SizeSource = p.distribution.resolved.Source
		size = p.distribution.resolved.Max
		p.payloadBuckets = p.distribution.buckets
	} else {
		if !validPayloadSize(size) {
			return p, errors.New("a bridge requires a size in 0..1048576 or a distribution via --bridge-bytes, --sizes-file, or --bridge-distribution")
		}
		p.CheckpointBytes = size
		p.payloadBuckets = []payloadBucket{{Min: size, Max: size}}
	}
	// Tomislav-RetCtx: these are opaque size proxies, not reconstructable AMQ
	// trusses. Use bytes_value so collector/exporter costs include the real type.
	rng := rand.New(rand.NewPCG(c.Seed, c.Seed^0x9e3779b97f4a7c15))
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(rng.Uint32())
	}
	digest := sha256.Sum256(payload)
	if p.distribution == nil {
		p.CheckpointSHA256 = hex.EncodeToString(digest[:])
	} else {
		p.PayloadBufferSHA256 = hex.EncodeToString(digest[:])
	}
	p.payload = payload
	p.checkpoint = bytesAttribute("_br", payload)
	key := "_d"
	var ordinary []byte
	if c.Bridge == "sb" {
		key = "_o"
		ordinary = binary.AppendUvarint(ordinary, c.Ordinal)
	}
	ordinary = binary.AppendUvarint(ordinary, c.Depth)
	p.OrdinaryBytes = len(ordinary)
	p.ordinary = bytesAttribute(key, ordinary)
	return p, nil
}

func bytesAttribute(key string, value []byte) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: value}}}
}

func encodeAttributes(specs []attributeSpec, reserveBridge bool) ([]*commonpb.KeyValue, error) {
	if len(specs) > 1024 {
		return nil, errors.New("at most 1024 attributes are supported per profile")
	}
	result := make([]*commonpb.KeyValue, 0, len(specs))
	seen := map[string]bool{}
	for _, s := range specs {
		if s.Key == "" || seen[s.Key] {
			return nil, fmt.Errorf("empty or duplicate attribute key %q", s.Key)
		}
		seen[s.Key] = true
		if reserveBridge && (s.Key == "_br" || s.Key == "_d" || s.Key == "_o") {
			return nil, fmt.Errorf("attribute %q is reserved for bridge metadata", s.Key)
		}
		if len(s.Value) == 0 || bytes.Equal(bytes.TrimSpace(s.Value), []byte("null")) {
			return nil, fmt.Errorf("attribute %q needs a non-null value", s.Key)
		}
		v := &commonpb.AnyValue{}
		var err error
		switch s.Type {
		case "string":
			var x string
			err = json.Unmarshal(s.Value, &x)
			v.Value = &commonpb.AnyValue_StringValue{StringValue: x}
		case "int":
			var x int64
			err = json.Unmarshal(s.Value, &x)
			v.Value = &commonpb.AnyValue_IntValue{IntValue: x}
		case "double":
			var x float64
			err = json.Unmarshal(s.Value, &x)
			if math.IsNaN(x) || math.IsInf(x, 0) {
				err = errors.New("non-finite double")
			}
			v.Value = &commonpb.AnyValue_DoubleValue{DoubleValue: x}
		case "bool":
			var x bool
			err = json.Unmarshal(s.Value, &x)
			v.Value = &commonpb.AnyValue_BoolValue{BoolValue: x}
		case "bytes":
			var encoded string
			err = json.Unmarshal(s.Value, &encoded)
			var x []byte
			if err == nil {
				x, err = base64.StdEncoding.DecodeString(encoded)
			}
			v.Value = &commonpb.AnyValue_BytesValue{BytesValue: x}
		default:
			err = fmt.Errorf("unknown type %q", s.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", s.Key, err)
		}
		result = append(result, &commonpb.KeyValue{Key: s.Key, Value: v})
	}
	return result, nil
}

type batchBuilder struct {
	c                config
	p                profile
	prefix           [8]byte
	request          *collector.ExportTraceServiceRequest
	spans            []*tracepb.Span
	checkpointAttrs  [][]*commonpb.KeyValue
	ordinaryAttrs    [][]*commonpb.KeyValue
	checkpointValues []*commonpb.AnyValue_BytesValue
	payloadCounts    batchPayloadCounts
}

type batchPayloadCounts struct {
	Count   uint64
	Bytes   uint64
	Min     int
	Max     int
	Buckets []uint64
}

// Tomislav-RetCtx: each worker reuses its protobuf objects/ID buffers after its
// synchronous RPC completes. No tracing SDK, hidden tags, events or links.
func newBatchBuilder(c config, p profile, prefix [8]byte) *batchBuilder {
	b := &batchBuilder{c: c, p: p, prefix: prefix}
	b.spans = make([]*tracepb.Span, c.Batch)
	b.checkpointAttrs = make([][]*commonpb.KeyValue, c.Batch)
	b.ordinaryAttrs = make([][]*commonpb.KeyValue, c.Batch)
	b.checkpointValues = make([]*commonpb.AnyValue_BytesValue, c.Batch)
	b.payloadCounts.Buckets = make([]uint64, len(p.payloadBuckets))
	for i := range b.spans {
		b.spans[i] = &tracepb.Span{TraceId: make([]byte, 16), SpanId: make([]byte, 8), ParentSpanId: make([]byte, 8), Name: c.SpanName}
		copy(b.spans[i].TraceId, prefix[:])
		binary.BigEndian.PutUint64(b.spans[i].ParentSpanId, math.MaxUint64)
		b.checkpointAttrs[i] = append([]*commonpb.KeyValue{}, p.attrs...)
		b.ordinaryAttrs[i] = append([]*commonpb.KeyValue{}, p.attrs...)
		if p.checkpoint != nil {
			checkpoint := p.checkpoint
			if p.distribution != nil {
				// Each slot owns its mutable length; the byte reservoir is immutable
				// and shared. Marshal sees exactly the sampled prefix, never max size.
				checkpoint = bytesAttribute("_br", nil)
				b.checkpointValues[i] = checkpoint.Value.Value.(*commonpb.AnyValue_BytesValue)
			}
			b.checkpointAttrs[i] = append(b.checkpointAttrs[i], checkpoint)
			b.ordinaryAttrs[i] = append(b.ordinaryAttrs[i], p.ordinary)
		}
	}
	rs := &tracepb.ResourceSpans{ScopeSpans: []*tracepb.ScopeSpans{{Spans: b.spans}}}
	if len(p.resource) > 0 {
		rs.Resource = &resourcepb.Resource{Attributes: p.resource}
	}
	b.request = &collector.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{rs}}
	return b
}

func (b *batchBuilder) build(first uint64, count int) (*collector.ExportTraceServiceRequest, uint64) {
	now := uint64(time.Now().UnixNano())
	var checkpoints uint64
	b.payloadCounts.Count, b.payloadCounts.Bytes = 0, 0
	b.payloadCounts.Min, b.payloadCounts.Max = 0, 0
	clear(b.payloadCounts.Buckets)
	for i, s := range b.spans[:count] {
		id := first + uint64(i) + 1
		binary.BigEndian.PutUint64(s.TraceId[8:], id)
		binary.BigEndian.PutUint64(s.SpanId, id)
		s.StartTimeUnixNano, s.EndTimeUnixNano = now, now+1000
		checkpoint, ordinal := false, uint64(0)
		if b.p.checkpoint != nil {
			checkpoint, ordinal = b.c.checkpointAt(id - 1)
		}
		if checkpoint {
			size, bucket := b.p.CheckpointBytes, 0
			if b.p.distribution != nil {
				size, bucket = b.p.distribution.sample(b.c.Seed, ordinal)
				b.checkpointValues[i].BytesValue = b.p.payload[:size]
			}
			if b.payloadCounts.Count == 0 {
				b.payloadCounts.Min, b.payloadCounts.Max = size, size
			}
			b.payloadCounts.Min = min(b.payloadCounts.Min, size)
			b.payloadCounts.Max = max(b.payloadCounts.Max, size)
			b.payloadCounts.Count++
			b.payloadCounts.Bytes += uint64(size)
			b.payloadCounts.Buckets[bucket]++
			s.Attributes = b.checkpointAttrs[i]
			checkpoints++
		} else {
			s.Attributes = b.ordinaryAttrs[i]
		}
	}
	b.request.ResourceSpans[0].ScopeSpans[0].Spans = b.spans[:count]
	return b.request, checkpoints
}
