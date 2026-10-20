package otelcol

// Tomislav-RetCtx: the direct OTLP encoder (otlp_wire.go) is held to the object trees the
// processors used to build and marshal with protobuf, byte for byte.  The reference below is that
// old code: buildSpanProto, the convert* functions and appendBridgeWire of vanilla_processor.go,
// pb_processor.go, cgpb_processor.go and sb_processor.go as of commit ecf78ae4, with the four
// processors' strip lists under one switch.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/mem"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// span decodes a buffered span back into its object form, for the tests that inspect the
// processors' buffers.
func (e wireSpan) span() *tracepb.Span {
	var s tracepb.Span
	if err := proto.Unmarshal(e.body, &s); err != nil {
		panic(err)
	}
	return &s
}

// ---------------------------------------------------------------------------------------------
// The reference: the old object-tree code.

func refSpanProto(s sdktrace.ReadOnlySpan, attrs []attribute.KeyValue, w *bridgeWire, highPriority bool, policy attrPolicy) *tracepb.Span {
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()
	spanProto := &tracepb.Span{
		TraceId:           traceID[:],
		SpanId:            spanID[:],
		StartTimeUnixNano: uint64(s.StartTime().UnixNano()),
		EndTimeUnixNano:   uint64(s.EndTime().UnixNano()),
		Name:              s.Name(),
		Kind:              refSpanKind(s.SpanKind()),
		Status:            &tracepb.Status{Code: tracepb.Status_StatusCode(s.Status().Code), Message: s.Status().Description},
		Attributes:        refBridgeWire(refAttributes(attrs, highPriority, policy), w, highPriority),
		Events:            refEvents(s.Events(), policy),
		Links:             refLinks(s.Links(), policy),
	}
	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:]
	}
	return spanProto
}

func refSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
	switch kind {
	case trace.SpanKindInternal:
		return tracepb.Span_SPAN_KIND_INTERNAL
	case trace.SpanKindServer:
		return tracepb.Span_SPAN_KIND_SERVER
	case trace.SpanKindClient:
		return tracepb.Span_SPAN_KIND_CLIENT
	case trace.SpanKindProducer:
		return tracepb.Span_SPAN_KIND_PRODUCER
	case trace.SpanKindConsumer:
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

func refAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: string(kv.Key), Value: anyValueProto(kv.Value)}
}

func refBridgeBytesKV(key, b64 string) *commonpb.KeyValue {
	raw, _ := decodeBR(b64)
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: raw}}}
}

func refAttributes(attrs []attribute.KeyValue, highPriority bool, policy attrPolicy) []*commonpb.KeyValue {
	if policy == policyVanilla {
		if len(attrs) == 0 {
			return nil
		}
		out := make([]*commonpb.KeyValue, 0, len(attrs))
		for _, attr := range attrs {
			if backend.IsReverseBaggageKey(attr.Key) || attr.Key == reverseDepthAttribute {
				continue
			}
			out = append(out, refAttribute(attr))
		}
		return out
	}
	out := make([]*commonpb.KeyValue, 0, len(attrs))
	for _, attr := range attrs {
		if strings.HasPrefix(string(attr.Key), "__bag.") {
			continue
		}
		switch policy {
		case policyPath:
			switch attr.Key {
			case "depth", "hasChildren":
				continue
			}
		case policyCallGraph:
			switch attr.Key {
			case "depth", "hasChildren", "childCount":
				continue
			}
		case policyStructural:
			switch attr.Key {
			case "depth", "hasChildren", "childCount", "eventCount", "remEndEvents":
				continue
			}
		}
		if attr.Key == AttrBREmit {
			if !highPriority {
				continue
			}
			out = append(out, refBridgeBytesKV(string(AttrBREmit), attr.Value.AsString()))
			continue
		}
		if policy == policyStructural {
			if attr.Key == AttrO || attr.Key == AttrD {
				if highPriority {
					continue
				}
				out = append(out, refBridgeBytesKV(string(attr.Key), attr.Value.AsString()))
				continue
			}
			if attr.Key == AttrOC || attr.Key == AttrDepth {
				continue
			}
		} else {
			if attr.Key == AttrD {
				if highPriority {
					continue
				}
				out = append(out, refBridgeBytesKV(string(AttrD), attr.Value.AsString()))
				continue
			}
			switch attr.Key {
			case AncestryKey, AncestryModeKey, AncestryExtraKey:
				continue
			}
		}
		out = append(out, refAttribute(attr))
	}
	return out
}

func refBridgeWire(out []*commonpb.KeyValue, w *bridgeWire, highPriority bool) []*commonpb.KeyValue {
	if w == nil {
		return out
	}
	bytesKV := func(key string, raw []byte) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BytesValue{BytesValue: raw}}}
	}
	varint := func(n int) []byte { return varintEncode(n) }
	if w.rev.rc != "" {
		out = append(out, &commonpb.KeyValue{Key: backend.ReverseTrussCheckpointKey,
			Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: w.rev.rc}}})
	}
	if highPriority {
		return append(out, bytesKV(AttrBREmit, w.emit))
	}
	if w.structural {
		out = append(out, bytesKV(AttrO, varint(w.ordinal)))
	}
	return append(out, bytesKV(AttrD, varint(w.depth)))
}

func refEvents(events []sdktrace.Event, policy attrPolicy) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]*tracepb.Span_Event, len(events))
	for i, event := range events {
		out[i] = &tracepb.Span_Event{TimeUnixNano: uint64(event.Time.UnixNano()), Name: event.Name, Attributes: refAttributes(event.Attributes, true, policy)}
	}
	return out
}

func refLinks(links []sdktrace.Link, policy attrPolicy) []*tracepb.Span_Link {
	if len(links) == 0 {
		return nil
	}
	out := make([]*tracepb.Span_Link, len(links))
	for i, link := range links {
		traceID := link.SpanContext.TraceID()
		spanID := link.SpanContext.SpanID()
		out[i] = &tracepb.Span_Link{TraceId: traceID[:], SpanId: spanID[:], Attributes: refAttributes(link.Attributes, true, policy)}
	}
	return out
}

func refResource(res *resource.Resource) *resourcepb.Resource {
	if res == nil {
		return &resourcepb.Resource{}
	}
	iter := res.Iter()
	if iter.Len() == 0 {
		return &resourcepb.Resource{}
	}
	attrs := make([]*commonpb.KeyValue, 0, iter.Len())
	for iter.Next() {
		attrs = append(attrs, refAttribute(iter.Attribute()))
	}
	return &resourcepb.Resource{Attributes: attrs}
}

// ---------------------------------------------------------------------------------------------
// Random spans.

func randomSpanContext(r *rand.Rand, valid bool) trace.SpanContext {
	var tid trace.TraceID
	var sid trace.SpanID
	if valid {
		for tid == (trace.TraceID{}) {
			for i := range tid {
				tid[i] = byte(r.IntN(256))
			}
		}
		for sid == (trace.SpanID{}) {
			for i := range sid {
				sid[i] = byte(r.IntN(256))
			}
		}
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.TraceFlags(r.IntN(2))})
}

func randomString(r *rand.Rand, n int) string {
	// Valid UTF-8 only: proto.Marshal refuses a string field that is not (the old export would have
	// dropped the whole batch); the byte encoder does not check, which is the one deliberate
	// difference (TestEncodeSpanInvalidUTF8).  Runes, not bytes, so multi-byte characters stay whole.
	alphabet := []rune("abcXYZ019 _-/.\x00\u00e9\u2028")
	b := make([]rune, r.IntN(n+1))
	for i := range b {
		b[i] = alphabet[r.IntN(len(alphabet))]
	}
	return string(b)
}

func randomValue(r *rand.Rand) attribute.Value {
	switch r.IntN(10) {
	case 0:
		return attribute.StringValue("")
	case 1:
		return attribute.StringValue(randomString(r, 40))
	case 2:
		return attribute.IntValue(r.IntN(3) - 1)
	case 3:
		return attribute.Int64Value(int64(r.Uint64()))
	case 4:
		return attribute.Float64Value([]float64{0, -1.5, 3.25e300, 1}[r.IntN(4)])
	case 5:
		return attribute.BoolValue(r.IntN(2) == 1)
	case 6:
		v := make([]string, r.IntN(3))
		for i := range v {
			v[i] = randomString(r, 8)
		}
		return attribute.StringSliceValue(v)
	case 7:
		v := make([]int64, r.IntN(3))
		for i := range v {
			v[i] = int64(r.IntN(200) - 100)
		}
		return attribute.Int64SliceValue(v)
	case 8:
		v := make([]float64, r.IntN(3))
		for i := range v {
			v[i] = r.Float64() * 100
		}
		return attribute.Float64SliceValue(v)
	default:
		v := make([]bool, r.IntN(3))
		for i := range v {
			v[i] = r.IntN(2) == 1
		}
		return attribute.BoolSliceValue(v)
	}
}

// randomAttrs mixes ordinary attributes with every key the strip lists know, so that each branch of
// every policy is exercised.
func randomAttrs(r *rand.Rand) []attribute.KeyValue {
	special := []string{"__bag._br", "__bag.prio", "__bag.force_lp", backend.ReverseBaggageKey, backend.ReverseTrussInputKey,
		reverseDepthAttribute, "depth", "hasChildren", "childCount", "eventCount", "remEndEvents", AttrBREmit, AttrD, AttrO,
		AttrOC, AttrDepth, AncestryKey, AncestryModeKey, AncestryExtraKey, "padding", backend.ReverseTrussCheckpointKey, "", "k"}
	n := r.IntN(8)
	attrs := make([]attribute.KeyValue, 0, n)
	for i := 0; i < n; i++ {
		var key string
		if r.IntN(3) == 0 {
			key = special[r.IntN(len(special))]
		} else {
			key = randomString(r, 12)
		}
		var value attribute.Value
		switch key {
		case AttrBREmit, AttrD, AttrO:
			raw := make([]byte, r.IntN(20))
			for j := range raw {
				raw[j] = byte(r.IntN(256))
			}
			if r.IntN(5) == 0 {
				value = attribute.StringValue("not base64!") // malformed: decodes to empty bytes
			} else {
				value = attribute.StringValue(base64.RawURLEncoding.EncodeToString(raw))
			}
		default:
			value = randomValue(r)
		}
		attrs = append(attrs, attribute.KeyValue{Key: attribute.Key(key), Value: value})
	}
	return attrs
}

func randomWire(r *rand.Rand) *bridgeWire {
	if r.IntN(4) == 0 {
		return nil
	}
	w := &bridgeWire{depth: r.IntN(300), priority: r.IntN(2) == 1, structural: r.IntN(2) == 1, ordinal: r.IntN(130)}
	w.emit = make([]byte, r.IntN(60))
	for i := range w.emit {
		w.emit[i] = byte(r.IntN(256))
	}
	if r.IntN(3) == 0 {
		w.rev.rc = randomString(r, 30)
	}
	return w
}

func randomStub(r *rand.Rand) tracetest.SpanStub {
	start := time.Unix(1_700_000_000+int64(r.IntN(1000)), int64(r.IntN(1e9)))
	stub := tracetest.SpanStub{
		Name:                 randomString(r, 24),
		SpanContext:          randomSpanContext(r, true),
		SpanKind:             trace.SpanKind(r.IntN(7)),
		StartTime:            start,
		EndTime:              start.Add(time.Duration(r.IntN(1e6))),
		Attributes:           randomAttrs(r),
		Status:               sdktrace.Status{Code: codes.Code(r.IntN(3)), Description: randomString(r, 10)},
		InstrumentationScope: instrumentation.Scope{Name: []string{"TextService", "ComposePost"}[r.IntN(2)], Version: []string{"", "1"}[r.IntN(2)]},
	}
	if r.IntN(2) == 0 {
		stub.Parent = randomSpanContext(r, true)
	}
	if r.IntN(10) == 0 {
		stub.StartTime, stub.EndTime = time.Time{}, time.Time{} // zero times: omitted fields
	}
	for i := r.IntN(3); i > 0; i-- {
		stub.Events = append(stub.Events, sdktrace.Event{Name: randomString(r, 10), Time: start.Add(time.Duration(r.IntN(1000))), Attributes: randomAttrs(r)})
	}
	for i := r.IntN(3); i > 0; i-- {
		stub.Links = append(stub.Links, sdktrace.Link{SpanContext: randomSpanContext(r, r.IntN(4) != 0), Attributes: randomAttrs(r)})
	}
	return stub
}

// ---------------------------------------------------------------------------------------------
// The tests.

func TestEncodeSpanMatchesProtoMarshal(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	policies := []attrPolicy{policyVanilla, policyPath, policyCallGraph, policyStructural}
	for i := 0; i < 20000; i++ {
		stub := randomStub(r)
		s := stub.Snapshot()
		policy := policies[r.IntN(len(policies))]
		var w *bridgeWire
		if policy != policyVanilla {
			w = randomWire(r)
		}
		hp := r.IntN(2) == 1
		attrs := s.Attributes()
		want, err := proto.Marshal(refSpanProto(s, attrs, w, hp, policy))
		if err != nil {
			t.Fatal(err)
		}
		got := encodeSpan(s, attrs, w, hp, policy)
		if string(got.body) != string(want) {
			t.Fatalf("case %d (policy %d, hp %v): encoded span differs\n got %x\nwant %x\nstub %+v", i, policy, hp, got.body, want, stub)
		}
		if got.traceID != s.SpanContext().TraceID() || got.scope != internScope(s.InstrumentationScope()) {
			t.Fatalf("case %d: wrong trace ID or scope on the entry", i)
		}
		// And it decodes to the same message.
		var back tracepb.Span
		if err := proto.Unmarshal(got.body, &back); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}

func TestBuildBatchMatchesProtoMarshal(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, res := range []*resource.Resource{nil, resource.Empty(), resource.NewSchemaless(attribute.String("service.name", "text"), attribute.Int("n", 3))} {
		for _, scope := range []instrumentation.Scope{{Name: "TextService"}, {Name: "ComposePost", Version: "2"}, {}} {
			for _, n := range []int{0, 1, 2, 17, 600} {
				spans := make([]wireSpan, 0, n)
				var ref []*tracepb.Span
				for i := 0; i < n; i++ {
					stub := randomStub(r)
					stub.InstrumentationScope = scope
					s := stub.Snapshot()
					w := randomWire(r)
					hp := r.IntN(2) == 1
					spans = append(spans, encodeSpan(s, s.Attributes(), w, hp, policyStructural))
					ref = append(ref, refSpanProto(s, s.Attributes(), w, hp, policyStructural))
				}
				want, err := proto.Marshal(&coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
					Resource:   refResource(res),
					ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: scope.Name, Version: scope.Version}, Spans: ref}},
				}}})
				if err != nil {
					t.Fatal(err)
				}
				batch := buildBatch(resourceField(res), scopeField(scope), spans)
				if string(batch.req) != string(want) {
					t.Fatalf("batch of %d differs (len %d vs %d)", n, len(batch.req), len(want))
				}
				if batch.n != int64(n) || len(batch.spans) != n || len(batch.traceIDs()) != n {
					t.Fatalf("batch of %d: n=%d spans=%d", n, batch.n, len(batch.spans))
				}
				for i, id := range batch.traceIDs() {
					if id != spans[i].traceID {
						t.Fatalf("batch of %d: id %d", n, i)
					}
				}
			}
		}
	}
	// batches keeps every span, groups by scope identity in place (a stable sort, so each scope's
	// spans stay in their buffered order), chunks at max, and leaves a one-scope snapshot as it is.
	var mixed, single []wireSpan
	for i := 0; i < 50; i++ {
		stub := randomStub(r)
		mixed = append(mixed, encodeSpan(stub.Snapshot(), nil, nil, false, policyVanilla))
		stub.InstrumentationScope = instrumentation.Scope{Name: "OneService"}
		single = append(single, encodeSpan(stub.Snapshot(), nil, nil, false, policyVanilla))
	}
	before := append([]wireSpan(nil), mixed...)
	for _, snap := range [][]wireSpan{mixed, single} {
		total, nb := 0, 0
		batches(resourceField(nil), snap, 7, func(b *wireBatch) {
			nb++
			if len(b.spans) == 0 || len(b.spans) > 7 || b.n != int64(len(b.spans)) {
				t.Fatalf("batch of %d spans (n=%d)", len(b.spans), b.n)
			}
			total += len(b.spans)
			for _, sp := range b.spans {
				if sp.scope != b.spans[0].scope {
					t.Fatal("span in the wrong scope batch")
				}
			}
			// The batch is the run's encoding: the ScopeSpans.scope field is the spans' scope.
			if !bytes.Contains(b.req, b.spans[0].scope.field) {
				t.Fatal("batch without its scope field")
			}
			b.release()
		})
		if total != len(snap) {
			t.Fatalf("batches lost spans: %d of %d", total, len(snap))
		}
		if nb < (len(snap)+6)/7 {
			t.Fatalf("too few batches: %d", nb)
		}
	}
	// In place and stable: every span is still there, grouped, and within a scope in the old order.
	seen := map[*wireScope]int{}
	for i := range mixed {
		if i > 0 && mixed[i].scope != mixed[i-1].scope {
			if _, dup := seen[mixed[i].scope]; dup {
				t.Fatal("mixed: a scope's spans are not one run")
			}
		}
		seen[mixed[i].scope]++
	}
	if len(seen) < 2 {
		t.Fatal("mixed scopes: expected several runs")
	}
	for _, scope := range []*wireScope{mixed[0].scope, mixed[len(mixed)-1].scope} {
		var was, is []string
		for _, sp := range before {
			if sp.scope == scope {
				was = append(was, string(sp.body))
			}
		}
		for _, sp := range mixed {
			if sp.scope == scope {
				is = append(is, string(sp.body))
			}
		}
		if len(was) != len(is) {
			t.Fatalf("scope %d: %d spans before, %d after", scope.order, len(was), len(is))
		}
		for i := range was {
			if was[i] != is[i] {
				t.Fatalf("scope %d: span %d moved within its scope", scope.order, i)
			}
		}
	}
	sortByScope(single)
	if single[0].scope != single[len(single)-1].scope {
		t.Fatal("single: one scope expected")
	}
}

// TestEncodeSpanInvalidUTF8: the old path refused to marshal (and so dropped) a batch holding a
// string attribute that is not valid UTF-8; the byte encoder writes it as it is.  No producer in
// the deployments makes such a value (the raw-bytes attributes are stripped or carried as bytes).
func TestEncodeSpanInvalidUTF8(t *testing.T) {
	stub := tracetest.SpanStub{Name: "n", SpanContext: randomSpanContext(rand.New(rand.NewPCG(9, 9)), true),
		Attributes: []attribute.KeyValue{attribute.String("raw", "\xff\xfe")}}
	s := stub.Snapshot()
	if _, err := proto.Marshal(refSpanProto(s, s.Attributes(), nil, false, policyVanilla)); err == nil {
		t.Fatal("the reference accepted invalid UTF-8")
	}
	// The encoder writes the bytes as they are (a Go protobuf decoder, like proto.Marshal, would
	// refuse them; whether a collector does depends on its decoder).
	got := encodeSpan(s, s.Attributes(), nil, false, policyVanilla)
	if !bytes.Contains(got.body, []byte("\xff\xfe")) {
		t.Fatalf("encoded span lost the raw bytes: %x", got.body)
	}
}

func TestRawCodec(t *testing.T) {
	c := rawCodec{}
	in := []byte{1, 2, 3}
	out, err := c.Marshal(in)
	if err != nil || string(out.Materialize()) != string(in) {
		t.Fatal("marshal", out, err)
	}
	in[0] = 9
	if out.Materialize()[0] != 1 {
		t.Fatal("marshal did not copy: the batch buffer would be shared with gRPC")
	}
	out.Free()
	if _, err := c.Marshal("x"); err == nil {
		t.Fatal("marshal accepted a string")
	}
	var back []byte
	if err := c.Unmarshal(mem.BufferSlice{mem.SliceBuffer(in)}, &back); err != nil || string(back) != string(in) {
		t.Fatal("unmarshal", back, err)
	}
	var str string
	if err := c.Unmarshal(mem.BufferSlice{mem.SliceBuffer(in)}, &str); err == nil || c.Name() != "proto" {
		t.Fatal("unmarshal target type / name")
	}
}

// TestBatchBufferPool: a released batch's buffer comes back for the next batch, grown to the
// largest request it has held.
func TestBatchBufferPool(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	var spans []wireSpan
	for i := 0; i < 300; i++ {
		stub := randomStub(r)
		stub.InstrumentationScope = instrumentation.Scope{Name: "S"}
		spans = append(spans, encodeSpan(stub.Snapshot(), nil, nil, false, policyVanilla))
	}
	res, scope := resourceField(nil), scopeField(instrumentation.Scope{Name: "S"})
	// Start from an empty free list (other tests leave buffers in it).
	for {
		select {
		case <-batchBufs:
			continue
		default:
		}
		break
	}
	big := buildBatch(res, scope, spans)
	want := string(big.req)
	if len(want) <= batchBufInit || len(want) > batchBufMax {
		t.Fatalf("test batch of %d bytes: want one that grows the buffer and is still kept", len(want))
	}
	grown := &big.req[0]
	big.release()
	if big.req != nil || big.buf != nil {
		t.Fatal("release left the batch pointing at the buffer")
	}
	// The next batch, small or big, reuses the grown buffer; rebuilt bytes are the same.
	small := buildBatch(res, scope, spans[:1])
	if &small.req[0] != grown {
		t.Fatal("the released buffer was not reused")
	}
	small.release()
	again := buildBatch(res, scope, spans)
	if &again.req[0] != grown || string(again.req) != want {
		t.Fatal("a rebuilt batch differs or did not reuse the buffer")
	}
	again.release()
	// A buffer that grew past batchBufMax is not kept.
	huge := &[]byte{}
	*huge = make([]byte, 0, batchBufMax+1)
	putBatchBuf(huge)
	if b := getBatchBuf(); b == huge {
		t.Fatal("an oversized buffer was kept")
	}
	// The list is bounded: putting more than batchBufsKept buffers keeps batchBufsKept.
	for i := 0; i < batchBufsKept+5; i++ {
		b := make([]byte, 0, 16)
		putBatchBuf(&b)
	}
	if len(batchBufs) != batchBufsKept {
		t.Fatalf("free list holds %d buffers, want %d", len(batchBufs), batchBufsKept)
	}
}

// TestInternScope: one entry per (name, version), found in the front scan and, past
// scopeFrontMax, in the map; the orders are distinct and the field is the encoded scope.
func TestInternScope(t *testing.T) {
	a := internScope(instrumentation.Scope{Name: "intern-A", Version: "1"})
	if internScope(instrumentation.Scope{Name: "intern-A", Version: "1"}) != a {
		t.Fatal("same scope, different entry")
	}
	b := internScope(instrumentation.Scope{Name: "intern-A", Version: "2"})
	if b == a || b.order == a.order {
		t.Fatal("another version: same entry or order")
	}
	if string(a.field) != string(scopeField(instrumentation.Scope{Name: "intern-A", Version: "1"})) {
		t.Fatal("field differs from scopeField")
	}
	var many []*wireScope
	for i := 0; i < scopeFrontMax+10; i++ {
		many = append(many, internScope(instrumentation.Scope{Name: fmt.Sprintf("intern-many-%d", i)}))
	}
	if f := scopeFront.Load(); f == nil || len(*f) != scopeFrontMax {
		t.Fatalf("front holds %d entries, want %d", len(*f), scopeFrontMax)
	}
	for i, ws := range many {
		if internScope(instrumentation.Scope{Name: fmt.Sprintf("intern-many-%d", i)}) != ws {
			t.Fatalf("scope %d not found again", i)
		}
	}
}

// exportServer records what the processors sent, as the collector would see it.
type exportServer struct {
	coltracepb.UnimplementedTraceServiceServer
	mu       sync.Mutex
	spans    []*tracepb.Span
	priority []string
	fail     bool
}

func (s *exportServer) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return nil, fmt.Errorf("refused")
	}
	prio := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(MetadataPriorityKey); len(v) > 0 {
			prio = v[0]
		}
	}
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				s.spans = append(s.spans, sp)
				s.priority = append(s.priority, prio)
			}
		}
	}
	return &coltracepb.ExportTraceServiceResponse{PartialSuccess: &coltracepb.ExportTracePartialSuccess{RejectedSpans: 0}}, nil
}

func startExportServer(t *testing.T) (*exportServer, string) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	es := &exportServer{}
	coltracepb.RegisterTraceServiceServer(srv, es)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return es, lis.Addr().String()
}

// TestDirectExportRoundTrip runs each live processor's flush against a real gRPC server: the
// spans arrive decodable, with the priority header the collector keys on, and a refused batch is
// counted and recorded as before.
func TestDirectExportRoundTrip(t *testing.T) {
	t.Setenv("REVERSE_TRUSS", "off")
	for _, kind := range []string{"v", "pb", "cgpb", "sb"} {
		t.Run(kind, func(t *testing.T) {
			es, addr := startExportServer(t)
			conn, err := dialOTLP(addr)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.close() })
			var processor sdktrace.SpanProcessor
			var flush func()
			var dropped func() int64
			switch kind {
			case "v":
				p := &VanillaProcessor{conn: conn}
				processor, flush, dropped = p, func() { p.flushBuffer(); p.exportWG.Wait() }, func() int64 { return p.spansDropped }
			case "pb":
				p := &PathBridgeProcessor{conn: conn, checkpointRange: checkpointRange{2, 4}}
				processor, flush, dropped = p, func() { p.flushBuffer(); p.exportWG.Wait() }, func() int64 { return p.spansDropped }
			case "cgpb":
				p := &CallGraphBridgeProcessor{conn: conn, checkpointRange: checkpointRange{2, 4}}
				processor, flush, dropped = p, func() { p.flushBuffer(); p.exportWG.Wait() }, func() int64 { return p.spansDropped }
			case "sb":
				p := &StructuralBridgeProcessor{conn: conn, checkpointRange: checkpointRange{2, 4}}
				processor, flush, dropped = p, func() { p.flushBuffer(); p.exportWG.Wait() }, func() int64 { return p.spansDropped }
			}
			tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(processor))
			ctx := context.Background()
			ctx, root := tp.Tracer("Frontend").Start(ctx, "FrontendServer_Call", trace.WithSpanKind(trace.SpanKindServer))
			root.SetAttributes(attribute.String("note", "x"), attribute.Int("childCount", 2))
			for i := 0; i < 2; i++ {
				_, child := tp.Tracer("Leaf").Start(backend.SetBaggageInContext(context.WithValue(ctx, "seqNum", i+1), map[string]string{}), "LeafClient_Do", trace.WithSpanKind(trace.SpanKindClient))
				child.End()
			}
			root.End()
			flush()
			es.mu.Lock()
			got := len(es.spans)
			var hp, lp int
			for _, p := range es.priority {
				switch p {
				case "hp":
					hp++
				case "lp":
					lp++
				}
			}
			names := map[string]int{}
			for _, sp := range es.spans {
				names[sp.Name]++
			}
			es.mu.Unlock()
			if got != 3 || names["FrontendServer_Call"] != 1 || names["LeafClient_Do"] != 2 {
				t.Fatalf("server got %d spans: %v", got, names)
			}
			if kind != "v" && hp+lp != 3 {
				t.Fatalf("priority header missing: hp=%d lp=%d", hp, lp)
			}
			if kind == "v" && hp+lp != 0 {
				t.Fatalf("vanilla sent a priority header: hp=%d lp=%d", hp, lp)
			}
			// A refused batch is counted and goes to the census.
			es.mu.Lock()
			es.fail = true
			es.mu.Unlock()
			_, _, before := refusedIDs.snapshot(0)
			_, extra := tp.Tracer("Frontend").Start(ctx, "FrontendServer_Call", trace.WithSpanKind(trace.SpanKindServer))
			extra.End()
			flush()
			_, _, after := refusedIDs.snapshot(0)
			if dropped() != 1 || after["spans_all"]-before["spans_all"] != 1 {
				t.Fatalf("refused batch: dropped=%d census delta=%d", dropped(), after["spans_all"]-before["spans_all"])
			}
		})
	}
}
