package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

const mixtureJSON = `{"source":"test mixture","type":"discrete","values":[{"bytes":16,"weight":75},{"bytes":32,"weight":20},{"bytes":128,"weight":5}]}`

func testDistribution(t *testing.T, contents string) *sizeDistribution {
	t.Helper()
	var spec distributionSpec
	if err := decodeJSON(bytes.NewBufferString(contents), &spec); err != nil {
		t.Fatal(err)
	}
	d, err := compileDistribution(spec)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDistributionProbabilitiesAndReproducibility(t *testing.T) {
	d := testDistribution(t, mixtureJSON)
	if math.Abs(d.resolved.Mean-24.8) > 1e-12 {
		t.Fatalf("mean %g", d.resolved.Mean)
	}
	observed := map[int]int{}
	const draws = 200000
	for i := 0; i < draws; i++ {
		size, b := d.sample(123, uint64(i))
		observed[size]++
		if d.buckets[b].Min > size || d.buckets[b].Max < size {
			t.Fatal("wrong histogram bucket")
		}
	}
	for _, p := range d.resolved.Probabilities {
		want := float64(draws) * p.Probability
		limit := 6 * math.Sqrt(want*(1-p.Probability))
		if math.Abs(float64(observed[p.Bytes])-want) > limit {
			t.Fatalf("size %d count %d; expected %.1f", p.Bytes, observed[p.Bytes], want)
		}
	}
	// Pin the versioned counter-based sampler, independent of worker RNG state.
	want := []int{32, 16, 16, 128, 16, 16, 16, 32, 16, 128}
	for i, n := range want {
		got, _ := d.sample(0, uint64(i))
		if got != n {
			t.Fatalf("sampler v1 ordinal %d: %d, want %d", i, got, n)
		}
	}
	different := 0
	for i := uint64(0); i < 100; i++ {
		a, _ := d.sample(1, i)
		b, _ := d.sample(2, i)
		if a != b {
			different++
		}
	}
	if different == 0 {
		t.Fatal("seed ignored")
	}
	for _, ordinal := range []uint64{0, 1, math.MaxUint64 - 1, math.MaxUint64} {
		size, _ := d.sample(math.MaxUint64, ordinal)
		if _, ok := observed[size]; !ok {
			t.Fatal("ordinal overflow produced unsupported size")
		}
	}
}

func TestEmpiricalAndDiscreteNormalization(t *testing.T) {
	empirical := testDistribution(t, `{"source":"observations","type":"empirical","samples":[32,16,16,128,16]}`)
	discrete := testDistribution(t, `{"source":"weighted","type":"discrete","values":[{"bytes":128,"weight":1},{"bytes":16,"weight":1},{"bytes":32,"weight":1},{"bytes":16,"weight":2},{"bytes":999999,"weight":0}]}`)
	if !reflect.DeepEqual(empirical.resolved.Probabilities, discrete.resolved.Probabilities) {
		t.Fatal("sample frequencies and duplicate weights differ")
	}
	if discrete.resolved.Max != 128 {
		t.Fatal("zero-weight size inflated the payload buffer")
	}
	for i := uint64(0); i < 1000; i++ {
		a, _ := empirical.sample(42, i)
		b, _ := discrete.sample(42, i)
		if a != b {
			t.Fatal("equivalent distributions yield different draws")
		}
	}
	large := testDistribution(t, `{"source":"large weights","type":"discrete","values":[{"bytes":1,"weight":1e308},{"bytes":2,"weight":1e308}]}`)
	if large.resolved.Mean != 1.5 {
		t.Fatal("large finite weights overflowed")
	}
}

func TestUniformBoundsAndBoundedHistograms(t *testing.T) {
	d := testDistribution(t, `{"source":"uniform","type":"uniform","min":0,"max":15}`)
	counts := make([]int, 16)
	for i := uint64(0); i < 160000; i++ {
		n, b := d.sample(7, i)
		if n < 0 || n > 15 || b != n {
			t.Fatal("uniform bounds/bucket")
		}
		counts[n]++
	}
	for n, count := range counts {
		if math.Abs(float64(count)-10000) > 650 {
			t.Fatalf("uniform size %d frequency %d", n, count)
		}
	}
	zero := testDistribution(t, `{"source":"empty values","type":"uniform","min":0,"max":0}`)
	if n, b := zero.sample(0, 99); n != 0 || b != 0 {
		t.Fatal("degenerate uniform")
	}
	wide := testDistribution(t, `{"source":"wide","type":"uniform","min":0,"max":1048576}`)
	if len(wide.buckets) > maxPayloadBuckets || wide.buckets[len(wide.buckets)-1].Max != 1048576 {
		t.Fatal("unbounded/truncated histogram")
	}
	for i := uint64(0); i < 1000; i++ {
		n, b := wide.sample(9, i)
		if n < wide.buckets[b].Min || n > wide.buckets[b].Max {
			t.Fatal("wide bucket")
		}
	}
	// More empirical point masses than buckets use bounded numeric ranges.
	values := make([]int, 1000)
	for i := range values {
		values[i] = i * 10
	}
	data, _ := json.Marshal(map[string]any{"source": "many points", "type": "empirical", "samples": values})
	many := testDistribution(t, string(data))
	if len(many.buckets) > maxPayloadBuckets {
		t.Fatal("too many empirical buckets")
	}
	for i := uint64(0); i < 1000; i++ {
		n, b := many.sample(9, i)
		if n%10 != 0 || n < many.buckets[b].Min || n > many.buckets[b].Max {
			t.Fatal("empirical grouped bucket")
		}
	}
}

func TestInvalidDistributions(t *testing.T) {
	for _, data := range []string{
		`{}`, `null`,
		`{"type":"uniform","min":0,"max":1}`,
		`{"source":"x","type":"normal"}`,
		`{"source":"x","type":"uniform","min":2,"max":1}`,
		`{"source":"x","type":"uniform","min":0,"max":1048577}`,
		`{"source":"x","type":"uniform","min":null,"max":1}`,
		`{"source":"x","type":"uniform","min":0.5,"max":1}`,
		`{"source":"x","type":"uniform","min":0,"max":1,"values":[]}`,
		`{"source":"x","type":"uniform","min":0,"max":1,"typo":2}`,
		`{"source":"x","type":"empirical","samples":[]}`,
		`{"source":"x","type":"empirical","samples":[null]}`,
		`{"source":"x","type":"empirical","samples":[-1]}`,
		`{"source":"x","type":"empirical","samples":[1.5]}`,
		`{"source":"x","type":"empirical","samples":[1],"min":0}`,
		`{"source":"x","type":"discrete","values":[]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":1}]}`,
		`{"source":"x","type":"discrete","values":[{"weight":1}]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":1,"weight":0}]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":1,"weight":-1}]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":1,"weight":1e309}]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":-1,"weight":1}]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":0,"weight":1},{"bytes":1,"weight":1e-20}]}`,
		`{"source":"x","type":"discrete","values":[{"bytes":0,"weight":1e308},{"bytes":1,"weight":1e-308}]}`,
	} {
		c := testConfig(t, "--bridge", "pb", "--bridge-distribution", tempJSON(t, data))
		if _, err := loadProfile(c); err == nil {
			t.Errorf("accepted invalid distribution: %s", data)
		}
	}
	for _, args := range [][]string{
		{"--bridge-distribution", "x"},
		{"--bridge", "pb", "--bridge-distribution", "x", "--bridge-bytes", "25"},
		{"--bridge", "pb", "--bridge-distribution", "x", "--sizes-file", "x"},
	} {
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Errorf("accepted conflicting flags %v", args)
		}
	}
}

func TestDistributionTableAndFixedCompatibility(t *testing.T) {
	path := tempJSON(t, `{"source":"per-CPD observations","sizes":{"pb":{"2":25,"6":{"type":"uniform","min":4,"max":40}}}}`)
	c := testConfig(t, "--bridge", "pb", "--sizes-file", path, "--cpd", "6")
	p := testProfile(t, c)
	if p.CheckpointBytes != -1 || p.CheckpointDistribution.Min != 4 || p.SizeSource != "per-CPD observations" || p.CheckpointSHA256 != "" || p.PayloadBufferSHA256 == "" {
		t.Fatal("unresolved distribution provenance")
	}
	c.CPD = 2
	p = testProfile(t, c)
	if p.CheckpointBytes != 25 || p.CheckpointDistribution != nil || p.CheckpointSHA256 == "" {
		t.Fatal("fixed table entry changed")
	}
	c.SizesFile = tempJSON(t, `{"source":"invalid","sizes":{"pb":{"2":null}}}`)
	if _, err := loadProfile(c); err == nil {
		t.Fatal("null size accepted as zero")
	}
}

func TestDistributionDryRunAndEmptyWireValue(t *testing.T) {
	path := tempJSON(t, `{"source":"empty payload boundary","type":"uniform","min":0,"max":0}`)
	var stdout, stderr bytes.Buffer
	if code := execute(t.Context(), []string{"--dry-run", "--bridge", "pb", "--bridge-distribution", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("dry-run exit %d: %s", code, stderr.String())
	}
	d := json.NewDecoder(&stdout)
	var m manifest
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Profile.CheckpointBytes != -1 || m.Profile.CheckpointDistribution == nil || m.Profile.CheckpointDistribution.Mean != 0 || m.Profile.PayloadBufferSHA256 == "" {
		t.Fatal("dry-run did not preserve resolved distribution")
	}
	c := testConfig(t, "--bridge", "pb", "--bridge-distribution", path)
	req, _ := newBatchBuilder(c, testProfile(t, c), [8]byte{1}).build(0, 1)
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &collector.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}
	attr := decoded.ResourceSpans[0].ScopeSpans[0].Spans[0].Attributes[0]
	value, ok := attr.Value.Value.(*commonpb.AnyValue_BytesValue)
	if attr.Key != "_br" || !ok || len(value.BytesValue) != 0 {
		t.Fatal("empty checkpoint must retain the bytes_value oneof")
	}
}

func TestDistributionWireLengthsAndWorkerIndependence(t *testing.T) {
	c := testConfig(t, "--bridge", "sb", "--cpd", "2", "--batch-size", "37", "--bridge-distribution", tempJSON(t, mixtureJSON))
	p := testProfile(t, c)
	const total = 3700
	want := make([]int, total)
	for i := range want {
		want[i] = -1
	}
	read := func(b *batchBuilder, first uint64, n int, out []int) {
		req, _ := b.build(first, n)
		wire, err := proto.Marshal(req)
		if err != nil {
			t.Error(err)
			return
		}
		decoded := &collector.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(wire, decoded); err != nil {
			t.Error(err)
			return
		}
		for _, span := range decoded.ResourceSpans[0].ScopeSpans[0].Spans {
			id := int(binary.BigEndian.Uint64(span.SpanId)) - 1
			value, ok := span.Attributes[0].Value.Value.(*commonpb.AnyValue_BytesValue)
			if !ok {
				t.Error("not bytes_value")
				return
			}
			if id%2 == 0 {
				out[id] = len(value.BytesValue)
			} else if span.Attributes[0].Key != "_o" || len(value.BytesValue) != 2 {
				t.Error("ordinary SB metadata changed")
			}
		}
	}
	serial := newBatchBuilder(c, p, [8]byte{1})
	for first := 0; first < total; first += 37 {
		read(serial, uint64(first), 37, want)
	}
	actual := make([]int, total)
	for i := range actual {
		actual[i] = -1
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			b := newBatchBuilder(c, p, [8]byte{9})
			for batch := 99 - worker; batch >= 0; batch -= 4 {
				read(b, uint64(batch*37), 37, actual)
			}
		}(worker)
	}
	wg.Wait()
	if !reflect.DeepEqual(want, actual) {
		t.Fatal("worker assignment/order changed sizes")
	}
	for i, n := range want {
		if i%2 == 0 {
			expected, _ := p.distribution.sample(c.Seed, uint64(i/2))
			if n != expected {
				t.Fatal("wire was padded to maximum or sampled at wrong ordinal")
			}
		}
	}
	// Refill must not allocate per span/checkpoint in the load-generation path.
	if allocations := testing.AllocsPerRun(100, func() { serial.build(0, 37) }); allocations != 0 {
		t.Fatalf("batch generation allocates %.1f objects", allocations)
	}
}

func TestDistributionExportsMatchPayloadAccounting(t *testing.T) {
	for _, bridge := range []string{"pb", "cgpb", "sb"} {
		t.Run(bridge, func(t *testing.T) {
			a, b := &captureServer{}, &captureServer{}
			// A span cap completes the sample before the deadline, avoiding a
			// sub-millisecond final dispatch margin under race-detector overhead.
			c := testConfig(t, "--endpoint", grpcFixture(t, a), "--endpoint", grpcFixture(t, b), "--insecure", "--bridge", bridge, "--bridge-distribution", tempJSON(t, mixtureJSON), "--rates", "10000", "--duration", "2s", "--spans", "1000", "--cpd", "3", "--workers", "3", "--batch-size", "31", "--queue", "64", "--report-interval", "1ms")
			var sequence atomic.Uint64
			p := testProfile(t, c)
			result := runPhase(t.Context(), c, p, clientsFor(t, c), [8]byte{1}, &sequence, 1, "measure", 10000, c.Duration, func(r phaseResult) {
				_, err := json.Marshal(r)
				if err != nil {
					t.Error(err)
				}
			})
			if err := result.counts.validate(); err != nil {
				t.Fatal(err)
			}
			if result.Acknowledged != 1000 || result.Payload == nil || result.Payload.Count != 334 {
				t.Fatalf("wrong payload accounting: %+v", result)
			}
			hist := map[int]uint64{}
			var sum uint64
			for _, server := range []*captureServer{a, b} {
				server.mu.Lock()
				for _, req := range server.requests {
					for _, span := range req.ResourceSpans[0].ScopeSpans[0].Spans {
						for _, attr := range span.Attributes {
							if attr.Key == "_br" {
								n := len(attr.Value.GetBytesValue())
								hist[n]++
								sum += uint64(n)
							}
						}
					}
				}
				server.mu.Unlock()
			}
			if result.Payload.Bytes != sum || math.Abs(result.Payload.Mean-float64(sum)/334) > 1e-12 {
				t.Fatal("byte sum/mean does not match exported values")
			}
			for _, bucket := range result.Payload.Histogram {
				if bucket.Min != bucket.Max || hist[bucket.Min] != bucket.Count {
					t.Fatal("histogram does not match wire sizes")
				}
			}
		})
	}
}
