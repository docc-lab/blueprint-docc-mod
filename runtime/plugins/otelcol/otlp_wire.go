package otelcol

// Tomislav-RetCtx: the OTLP export bytes, written directly.
//
// Every live processor (vanilla, PB, CGPB, SB) used to build a *tracepb.Span object tree for each
// ended span (buildSpanProto: about 64 allocations per request), hold it in its buffer, and hand
// the batch to OpenTelemetry's otlptracegrpc exporter client, which marshalled the tree with
// protobuf's reflective encoder and called the collector's Export method.  In the 2026-10-16
// profiles that was 1.7% of all CPU for vanilla (utils/grpc_interceptors/README.md, "Where tracing
// costs CPU").  This file replaces both steps inside the processor: encodeSpan writes a span's OTLP
// bytes straight from the SDK's snapshot (one allocation per span), buildBatch concatenates them
// into an ExportTraceServiceRequest, and otlpConn sends the bytes over a plain gRPC connection with a
// codec that passes them through.  Nothing outside this package changes: the collector receives
// standard OTLP/gRPC with the same "bridges-priority" metadata, and the OpenTelemetry SDK is still
// the SDK.  The exporter client is only used by the earlier processors (eb, rc, priority,
// realtime).
//
// The bytes are the ones proto.Marshal produced for the old object trees, field for field (the
// tests in otlp_wire_test.go hold them to that, byte for byte), including two things the old code
// did that a reader may not expect: an empty Status message is always present on a span (the old
// code always set one), and the status code is copied from the SDK's codes.Code without mapping
// (the SDK and OTLP number Ok and Error the other way round; see docs/tracing/BUG_REPORT.md).
//
// Protobuf wire format, as used here: a field is a tag byte (field number << 3 | wire type) and a
// value; wire type 0 is a varint, 1 a little-endian 64-bit value, 2 a length-prefixed byte string
// (which is also how nested messages and strings are written).  Fields of a proto3 message are
// omitted when they have their default value, except members of a oneof (AnyValue) and message
// fields that are set, which are written even when empty.

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/proto"
)

// ---------------------------------------------------------------------------------------------
// The connection.

// otlpExportMethod is the OTLP/gRPC trace export method (opentelemetry.proto.collector.trace.v1).
const otlpExportMethod = "/opentelemetry.proto.collector.trace.v1.TraceService/Export"

// otlpConn is a plain gRPC connection to the collector's OTLP endpoint, used with pre-encoded
// request bytes.  It replaces the otlptracegrpc exporter client in the live processors.
type otlpConn struct {
	endpoint string
	cc       *grpc.ClientConn
}

// dialOTLP opens the connection as the exporter client did (grpc.NewClient: the connection is made
// lazily, on the first call; insecure, as every deployment here).
func dialOTLP(endpoint string) (*otlpConn, error) {
	cc, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUserAgent("blueprint-otelcol"))
	if err != nil {
		return nil, err
	}
	return &otlpConn{endpoint: endpoint, cc: cc}, nil
}

// export sends one encoded ExportTraceServiceRequest.  ctx carries the deadline and the outgoing
// metadata (the "bridges-priority" header); the error is the gRPC status, as before, so
// categorizeSendError and the retry gate (sdk_retry.go) see what they saw.
func (c *otlpConn) export(ctx context.Context, req []byte) error {
	var resp []byte
	if err := c.cc.Invoke(ctx, otlpExportMethod, req, &resp, grpc.ForceCodecV2(rawCodec{})); err != nil {
		return err
	}
	// A partial success is a success with a note (the exporter client logged it through
	// otel.Handle); the collector's priority processor refuses whole batches instead.
	if len(resp) > 0 {
		var r coltracepb.ExportTraceServiceResponse
		if proto.Unmarshal(resp, &r) == nil && r.PartialSuccess != nil {
			if n, msg := r.PartialSuccess.GetRejectedSpans(), r.PartialSuccess.GetErrorMessage(); n != 0 || msg != "" {
				slog.Warn("OTLP partial success", "rejected_spans", n, "error_message", msg)
			}
		}
	}
	return nil
}

func (c *otlpConn) close() error { return c.cc.Close() }

// rawCodec hands gRPC request bytes that are already encoded and gives back the response bytes as
// they are.  Its name is "proto" so the content type stays application/grpc+proto.
//
// Marshal copies the request into a buffer from gRPC's pool (what gRPC's own protobuf codec
// marshals into), which gRPC frees once the bytes are written.  The copy (one memmove of the
// batch) is what lets the batch keep its own buffer: it goes back to batchBufs as soon as the
// export is over, retry included, with nothing in gRPC referencing it.  (gRPC's pool is a
// sync.Pool, so under load it misses as often as it did for the old exporter: the same
// allocation, see batchBufs.)
type rawCodec struct{}

func (rawCodec) Marshal(v any) (mem.BufferSlice, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("rawCodec: want []byte, got %T", v)
	}
	return mem.BufferSlice{mem.Copy(b, mem.DefaultBufferPool())}, nil
}

func (rawCodec) Unmarshal(data mem.BufferSlice, v any) error {
	out, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("rawCodec: want *[]byte, got %T", v)
	}
	*out = data.Materialize()
	return nil
}

func (rawCodec) Name() string { return "proto" }

// ---------------------------------------------------------------------------------------------
// The buffer entry and the batch.

// wireSpan is one ended span as the processors now buffer it: its OTLP Span message body, already
// encoded, with what the flush needs to group and account for it.  48 bytes: the scope is the
// process-wide interned entry (wireScopes), not the SDK's 64-byte Scope value, so the buffers
// and their flush-time sort move half as much.
type wireSpan struct {
	body    []byte        // the Span message (without the ScopeSpans.spans tag and length)
	scope   *wireScope    // groups spans into ScopeSpans at flush time (sortByScope)
	traceID trace.TraceID // for the refused-trace census of a dropped batch
}

// wireBatch is one export: the request bytes and the spans in it (for the count and, if the batch
// is refused, the census).  spans is a sub-slice of the flush's snapshot and the request buffer
// comes from batchBufs, so a batch allocates nothing once the free list has filled.
type wireBatch struct {
	req   []byte
	n     int64
	spans []wireSpan
	buf   *[]byte // the buffer from batchBufs that req was written into
}

// batchBufs holds the request buffers of batches, reused from flush to flush.  A batch keeps its
// buffer until the processor calls release, after the batch was sent or dropped (after its one
// retry, if it got one).  Writing the request into a fresh make([]byte) per batch cost 6-10 KB
// of allocation per request in the 2026-10-20 profiles (utils/grpc_interceptors/README.md).
//
// A free list, not a sync.Pool: the garbage collector empties a sync.Pool at every cycle, and a
// loaded service collects many times between two flushes (100 ms apart), so a sync.Pool here
// missed at every flush (the second profile of 2026-10-20).  The list keeps at most batchBufsKept
// buffers of at most batchBufMax bytes each, so what it holds is bounded; a batch that needs more
// grows its buffer, and release drops a buffer that grew past the cap.
var batchBufs = make(chan *[]byte, batchBufsKept)

const (
	batchBufsKept = 32        // buffers kept: more than the batches one flush spawns
	batchBufInit  = 32 << 10  // a new buffer's capacity (a 100 ms batch of a loaded service)
	batchBufMax   = 256 << 10 // bigger buffers are not kept
)

func getBatchBuf() *[]byte {
	select {
	case b := <-batchBufs:
		return b
	default:
		b := make([]byte, 0, batchBufInit)
		return &b
	}
}

func putBatchBuf(b *[]byte) {
	if cap(*b) > batchBufMax {
		return
	}
	*b = (*b)[:0]
	select {
	case batchBufs <- b:
	default: // the list is full; the garbage collector takes this one
	}
}

// release returns the batch's buffer to the free list (its grown size with it).  Called exactly
// once per batch; the request bytes must not be used afterwards.
func (b *wireBatch) release() {
	*b.buf = b.req[:0]
	b.req = nil
	putBatchBuf(b.buf)
	b.buf = nil
}

// traceIDs lists the batch's trace IDs for the census (recordRefusedIDs); only built when a batch
// is refused.
func (b *wireBatch) traceIDs() []trace.TraceID {
	ids := make([]trace.TraceID, len(b.spans))
	for i := range b.spans {
		ids[i] = b.spans[i].traceID
	}
	return ids
}

// wireScope is one InstrumentationScope as the flush needs it: the encoded ScopeSpans.scope field
// and the scope's interning order, the key sortByScope groups by.  A process has a handful: the
// server-side tracer of its service and one client-side tracer per service it calls (the
// interceptors name the tracers as the wrappers did).
type wireScope struct {
	name, version string // the identity, for the scopeFront scan
	field         []byte // the ScopeSpans.scope field, tag and length included
	order         int    // interning order (from 1); the sort key
}

// scopeKey is the InstrumentationScope identity at the proto level.
type scopeKey struct{ name, version string }

// wireScopes interns the scopes, process-wide; the encoding is a pure function of the scope.
// Lookups scan scopeFront, an immutable slice of the first scopes interned (a new one replaces the
// slice under wireScopesMu): comparing two strings that are the same string (the tracer's) is a
// pointer check, where hashing the key for the sync.Map cost a quarter of encodeSpan in the PB
// profile of 2026-10-20 (0.33 s of 1.43 s).  A process with more scopes than scopeFrontMax finds
// the rest in the map.
var (
	wireScopes   sync.Map   // scopeKey -> *wireScope
	wireScopesMu sync.Mutex // serializes interning a new scope, so the orders are unique
	wireScopeN   int        // scopes interned so far (under wireScopesMu)
	scopeFront   atomic.Pointer[[]*wireScope]
)

const scopeFrontMax = 32

// internScope returns the process-wide entry of a scope.  encodeSpan calls it for every span, so
// the found case is a scan of scopeFront, a few pointer comparisons.
func internScope(scope instrumentation.Scope) *wireScope {
	if front := scopeFront.Load(); front != nil {
		for _, ws := range *front {
			if ws.name == scope.Name && ws.version == scope.Version {
				return ws
			}
		}
	}
	key := scopeKey{scope.Name, scope.Version}
	if ws, ok := wireScopes.Load(key); ok {
		return ws.(*wireScope)
	}
	wireScopesMu.Lock()
	defer wireScopesMu.Unlock()
	if ws, ok := wireScopes.Load(key); ok {
		return ws.(*wireScope)
	}
	body, _ := proto.Marshal(&commonpb.InstrumentationScope{Name: scope.Name, Version: scope.Version})
	wireScopeN++
	ws := &wireScope{name: scope.Name, version: scope.Version, field: appendBytesFieldAlways(nil, 1, body), order: wireScopeN} // a set message is written even when empty
	wireScopes.Store(key, ws)
	var front []*wireScope
	if f := scopeFront.Load(); f != nil {
		front = *f
	}
	if len(front) < scopeFrontMax {
		next := append(append([]*wireScope(nil), front...), ws) // a new slice: readers hold the old one
		scopeFront.Store(&next)
	}
	return ws
}

// scopeField is the encoded ScopeSpans.scope field of a scope (tests and benchmarks).
func scopeField(scope instrumentation.Scope) []byte { return internScope(scope).field }

// resourceField encodes the ResourceSpans.resource field for a TracerProvider's Resource, once per
// processor (every span of a provider shares it): the attributes as the old convertResourceToProto
// built them, marshalled once.
func resourceField(res *resource.Resource) []byte {
	var attrs []*commonpb.KeyValue
	if res != nil {
		iter := res.Iter()
		if iter.Len() > 0 {
			attrs = make([]*commonpb.KeyValue, 0, iter.Len())
			for iter.Next() {
				kv := iter.Attribute()
				attrs = append(attrs, &commonpb.KeyValue{Key: string(kv.Key), Value: anyValueProto(kv.Value)})
			}
		}
	}
	body, _ := proto.Marshal(&resourcepb.Resource{Attributes: attrs})
	return appendBytesFieldAlways(nil, 1, body) // a set message is written even when empty
}

// anyValueProto is the object form of an attribute value (resource attributes only; spans use
// appendAnyValue below).  Same mapping, incl. the fmt.Sprintf fallback.
func anyValueProto(v attribute.Value) *commonpb.AnyValue {
	av := new(commonpb.AnyValue)
	switch v.Type() {
	case attribute.STRING:
		av.Value = &commonpb.AnyValue_StringValue{StringValue: v.AsString()}
	case attribute.INT64:
		av.Value = &commonpb.AnyValue_IntValue{IntValue: v.AsInt64()}
	case attribute.FLOAT64:
		av.Value = &commonpb.AnyValue_DoubleValue{DoubleValue: v.AsFloat64()}
	case attribute.BOOL:
		av.Value = &commonpb.AnyValue_BoolValue{BoolValue: v.AsBool()}
	case attribute.BOOLSLICE:
		vals := make([]*commonpb.AnyValue, 0)
		for _, x := range v.AsBoolSlice() {
			vals = append(vals, &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: x}})
		}
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: vals}}
	case attribute.INT64SLICE:
		vals := make([]*commonpb.AnyValue, 0)
		for _, x := range v.AsInt64Slice() {
			vals = append(vals, &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: x}})
		}
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: vals}}
	case attribute.FLOAT64SLICE:
		vals := make([]*commonpb.AnyValue, 0)
		for _, x := range v.AsFloat64Slice() {
			vals = append(vals, &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: x}})
		}
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: vals}}
	case attribute.STRINGSLICE:
		vals := make([]*commonpb.AnyValue, 0)
		for _, x := range v.AsStringSlice() {
			vals = append(vals, &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: x}})
		}
		av.Value = &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: vals}}
	default:
		av.Value = &commonpb.AnyValue_StringValue{StringValue: fmt.Sprintf("%v", v.AsInterface())}
	}
	return av
}

// buildBatch encodes one ExportTraceServiceRequest: one ResourceSpans (the processor's resource)
// holding one ScopeSpans (scope) holding the given spans, which must all be of that scope.
//
//	request    := 0x0a len( resourceSpans )
//	resourceSpans := resourceField || 0x12 len( scopeSpans )
//	scopeSpans    := scopeField || ( 0x12 len( span ) )*
func buildBatch(resource, scope []byte, spans []wireSpan) *wireBatch {
	ss := len(scope)
	for i := range spans {
		ss += 1 + uvarintLen(uint64(len(spans[i].body))) + len(spans[i].body)
	}
	rs := len(resource) + 1 + uvarintLen(uint64(ss)) + ss
	total := 1 + uvarintLen(uint64(rs)) + rs
	buf := getBatchBuf()
	req := (*buf)[:0]
	if cap(req) < total {
		req = make([]byte, 0, total) // the free-list entry grows to it at release
	}
	req = append(req, 0x0a)
	req = binary.AppendUvarint(req, uint64(rs))
	req = append(req, resource...)
	req = append(req, 0x12)
	req = binary.AppendUvarint(req, uint64(ss))
	req = append(req, scope...)
	for i := range spans {
		req = appendBytesField(req, 2, spans[i].body)
	}
	return &wireBatch{req: req, n: int64(len(spans)), spans: spans, buf: buf}
}

// sortByScope groups a buffer snapshot in place: a stable sort by scope (interning order), after
// which each scope's spans are one run, in their buffered order.  Nothing is copied or allocated.
// The first version of this code copied the entries into per-scope slices (about 26 us and 4.5 KB
// of allocation per request in the PB profiles of 2026-10-20), and a one-scope fast path did not
// help: every process that calls other services has one client-side scope per callee.  A snapshot
// that is already in scope order (a leaf service: one scope) is left as it is.
func sortByScope(snap []wireSpan) {
	for i := 1; i < len(snap); i++ {
		if snap[i].scope.order < snap[i-1].scope.order {
			slices.SortStableFunc(snap, func(a, b wireSpan) int { return a.scope.order - b.scope.order })
			return
		}
	}
}

// batches sorts a snapshot by scope and hands emit one batch per run of at most max spans of one
// scope, each encoded by buildBatch under the processor's resource: one ResourceSpans wrapping one
// ScopeSpans wrapping the run.  The batches keep sub-slices of the snapshot, which the caller must
// not reuse.  Used by the flush of every processor.
func batches(resource []byte, snap []wireSpan, max int, emit func(*wireBatch)) {
	sortByScope(snap)
	for start := 0; start < len(snap); {
		end := start + 1
		for end < len(snap) && snap[end].scope == snap[start].scope {
			end++
		}
		for s := start; s < end; s += max {
			emit(buildBatch(resource, snap[start].scope.field, snap[s:min(s+max, end)]))
		}
		start = end
	}
}

// ---------------------------------------------------------------------------------------------
// The span encoder.

// attrPolicy is which attributes a processor exports and how: the strip lists and the breadcrumb
// handling of the old per-processor convertAttributes, unchanged.
type attrPolicy uint8

const (
	policyVanilla    attrPolicy = iota // vanilla: everything but the reverse-path keys
	policyPath                         // PB
	policyCallGraph                    // CGPB
	policyStructural                   // SB
)

// encodeSpan writes the OTLP Span message for s.  attrs is s.Attributes() (the caller already
// holds it); w is the span's bridge wire entry (nil for vanilla); highPriority says whether the
// span ended up a checkpoint, which decides which breadcrumbs it carries.
func encodeSpan(s sdktrace.ReadOnlySpan, attrs []attribute.KeyValue, w *bridgeWire, highPriority bool, policy attrPolicy) wireSpan {
	sc := s.SpanContext()
	tid, sid := sc.TraceID(), sc.SpanID()
	size := 64 + len(s.Name()) + 32*len(attrs) + 48*len(s.Events()) + 48*len(s.Links())
	if w != nil {
		size += len(w.emit) + len(w.rev.rc) + 16
	}
	buf := make([]byte, 0, size)
	buf = appendBytesField(buf, 1, tid[:])
	buf = appendBytesField(buf, 2, sid[:])
	if parent := s.Parent(); parent.IsValid() {
		pid := parent.SpanID()
		buf = appendBytesField(buf, 4, pid[:])
	}
	buf = appendStringField(buf, 5, s.Name())
	buf = appendVarintField(buf, 6, uint64(spanKindCode(s.SpanKind())))
	buf = appendFixed64Field(buf, 7, uint64(s.StartTime().UnixNano()))
	buf = appendFixed64Field(buf, 8, uint64(s.EndTime().UnixNano()))
	buf = appendAttributes(buf, 9, attrs, highPriority, policy)
	buf = appendBridgeWireBytes(buf, w, highPriority)
	for _, ev := range s.Events() {
		mark := len(buf)
		buf = appendFixed64Field(buf, 1, uint64(ev.Time.UnixNano()))
		buf = appendStringField(buf, 2, ev.Name)
		buf = appendAttributes(buf, 3, ev.Attributes, true, policy)
		buf = closeMessage(buf, 11, mark)
	}
	for _, link := range s.Links() {
		mark := len(buf)
		ltid, lsid := link.SpanContext.TraceID(), link.SpanContext.SpanID()
		buf = appendBytesField(buf, 1, ltid[:])
		buf = appendBytesField(buf, 2, lsid[:])
		buf = appendAttributes(buf, 4, link.Attributes, true, policy)
		buf = closeMessage(buf, 13, mark)
	}
	// Status is always present (the old code always built one), even when empty.
	mark := len(buf)
	st := s.Status()
	buf = appendStringField(buf, 2, st.Description)
	buf = appendVarintField(buf, 3, uint64(st.Code)) // unmapped, as before (see the file comment)
	buf = closeMessage(buf, 15, mark)
	return wireSpan{body: buf, scope: internScope(s.InstrumentationScope()), traceID: tid}
}

func spanKindCode(kind trace.SpanKind) int {
	switch kind {
	case trace.SpanKindInternal:
		return 1
	case trace.SpanKindServer:
		return 2
	case trace.SpanKindClient:
		return 3
	case trace.SpanKindProducer:
		return 4
	case trace.SpanKindConsumer:
		return 5
	default:
		return 0
	}
}

// appendAttributes writes the attributes the policy lets through as repeated KeyValue messages of
// the given field.  The per-policy rules are those of the old convertAttributes functions:
//
//	vanilla:  skip the reverse-path keys (backend.IsReverseBaggageKey, __bag.__rt_depth)
//	PB:       skip __bag.*, depth, hasChildren; `_br` iff HP and `_d` iff LP as raw bytes
//	          (legacy attribute-carried breadcrumbs, bridgeBytesKV); skip the ancestry keys
//	CGPB:     as PB, also skipping childCount
//	SB:       skip __bag.*, depth, hasChildren, childCount, eventCount, remEndEvents; `_br` iff HP,
//	          `_o` and `_d` iff LP, as raw bytes; skip `_oc` and `d`
func appendAttributes(buf []byte, field int, attrs []attribute.KeyValue, highPriority bool, policy attrPolicy) []byte {
	for _, attr := range attrs {
		key := string(attr.Key)
		switch policy {
		case policyVanilla:
			if backend.IsReverseBaggageKey(attr.Key) || key == reverseDepthAttribute {
				continue
			}
		case policyPath, policyCallGraph:
			if strings.HasPrefix(key, "__bag.") {
				continue
			}
			switch key {
			case "depth", "hasChildren", AncestryKey, AncestryModeKey, AncestryExtraKey:
				continue
			case "childCount":
				if policy == policyCallGraph {
					continue
				}
			case AttrBREmit:
				if !highPriority {
					continue
				}
				buf = appendBridgeBytesAttr(buf, field, key, attr.Value.AsString())
				continue
			case AttrD:
				if highPriority {
					continue
				}
				buf = appendBridgeBytesAttr(buf, field, key, attr.Value.AsString())
				continue
			}
		case policyStructural:
			if strings.HasPrefix(key, "__bag.") {
				continue
			}
			switch key {
			case "depth", "hasChildren", "childCount", "eventCount", "remEndEvents", AttrOC, AttrDepth:
				continue
			case AttrBREmit:
				if !highPriority {
					continue
				}
				buf = appendBridgeBytesAttr(buf, field, key, attr.Value.AsString())
				continue
			case AttrO, AttrD:
				if highPriority {
					continue
				}
				buf = appendBridgeBytesAttr(buf, field, key, attr.Value.AsString())
				continue
			}
		}
		mark := len(buf)
		buf = appendStringField(buf, 1, key)
		buf = appendAnyValue(buf, 2, attr.Value)
		buf = closeMessage(buf, field, mark)
	}
	return buf
}

// appendBridgeBytesAttr writes key = raw bytes decoded from a base64 attribute (the legacy
// attribute-carried breadcrumbs; bridgeBytesKV did this into an object).  Malformed base64 gives
// empty bytes, as before.
func appendBridgeBytesAttr(buf []byte, field int, key, b64 string) []byte {
	raw, _ := decodeBR(b64)
	return appendKeyBytes(buf, field, key, raw)
}

// appendKeyBytes writes one KeyValue whose value is the bytes_value raw (written even when empty:
// the oneof member is set).
func appendKeyBytes(buf []byte, field int, key string, raw []byte) []byte {
	mark := len(buf)
	buf = appendStringField(buf, 1, key)
	inner := len(buf)
	buf = appendBytesFieldAlways(buf, 7, raw)
	buf = closeMessage(buf, 2, inner)
	return closeMessage(buf, field, mark)
}

// appendKeyString writes one KeyValue whose value is the string_value s.
func appendKeyString(buf []byte, field int, key, s string) []byte {
	mark := len(buf)
	buf = appendStringField(buf, 1, key)
	inner := len(buf)
	buf = appendStringFieldAlways(buf, 1, s)
	buf = closeMessage(buf, 2, inner)
	return closeMessage(buf, field, mark)
}

// appendBridgeWireBytes writes the span's wire attributes from its bridge wire entry, as
// appendBridgeWire did into the object tree: `_rc` when PrepareCheckpoint decided one, then `_br`
// (the raw emit payload) on checkpoints, or `_o` (SB, the start ordinal) and `_d` (the depth) on
// ordinary spans, both as uvarint bytes.
func appendBridgeWireBytes(buf []byte, w *bridgeWire, highPriority bool) []byte {
	if w == nil {
		return buf
	}
	if w.rev.rc != "" { // Tomislav-RetCtx: `_rc`, decided by PrepareCheckpoint (reverse_checkpoint.go)
		buf = appendKeyString(buf, 9, backend.ReverseTrussCheckpointKey, w.rev.rc)
	}
	if highPriority {
		return appendKeyBytes(buf, 9, AttrBREmit, w.emit)
	}
	var tmp [binary.MaxVarintLen64]byte
	if w.structural {
		buf = appendKeyBytes(buf, 9, AttrO, tmp[:binary.PutUvarint(tmp[:], uint64(w.ordinal))])
	}
	return appendKeyBytes(buf, 9, AttrD, tmp[:binary.PutUvarint(tmp[:], uint64(w.depth))])
}

// appendAnyValue writes an AnyValue message as the given field.  A oneof member is written even
// when it is the zero value.
func appendAnyValue(buf []byte, field int, v attribute.Value) []byte {
	mark := len(buf)
	switch v.Type() {
	case attribute.STRING:
		buf = appendStringFieldAlways(buf, 1, v.AsString())
	case attribute.INT64:
		buf = appendVarintFieldAlways(buf, 3, uint64(v.AsInt64()))
	case attribute.FLOAT64:
		buf = appendFixed64FieldAlways(buf, 4, math.Float64bits(v.AsFloat64()))
	case attribute.BOOL:
		var b uint64
		if v.AsBool() {
			b = 1
		}
		buf = appendVarintFieldAlways(buf, 2, b)
	case attribute.BOOLSLICE:
		inner := len(buf)
		for _, x := range v.AsBoolSlice() {
			m := len(buf)
			var b uint64
			if x {
				b = 1
			}
			buf = appendVarintFieldAlways(buf, 2, b)
			buf = closeMessage(buf, 1, m)
		}
		buf = closeMessage(buf, 5, inner)
	case attribute.INT64SLICE:
		inner := len(buf)
		for _, x := range v.AsInt64Slice() {
			m := len(buf)
			buf = appendVarintFieldAlways(buf, 3, uint64(x))
			buf = closeMessage(buf, 1, m)
		}
		buf = closeMessage(buf, 5, inner)
	case attribute.FLOAT64SLICE:
		inner := len(buf)
		for _, x := range v.AsFloat64Slice() {
			m := len(buf)
			buf = appendFixed64FieldAlways(buf, 4, math.Float64bits(x))
			buf = closeMessage(buf, 1, m)
		}
		buf = closeMessage(buf, 5, inner)
	case attribute.STRINGSLICE:
		inner := len(buf)
		for _, x := range v.AsStringSlice() {
			m := len(buf)
			buf = appendStringFieldAlways(buf, 1, x)
			buf = closeMessage(buf, 1, m)
		}
		buf = closeMessage(buf, 5, inner)
	default:
		buf = appendStringFieldAlways(buf, 1, fmt.Sprintf("%v", v.AsInterface()))
	}
	return closeMessage(buf, field, mark)
}

// ---------------------------------------------------------------------------------------------
// Protobuf primitives.  Fields are numbered below 16, so every tag is one byte.

func uvarintLen(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

// closeMessage turns the bytes written since mark into a length-prefixed message field: the tag
// and the length are inserted in front of the body by moving the body right (one copy of a small
// body, instead of encoding into a scratch buffer and copying it over).
func closeMessage(buf []byte, field int, mark int) []byte {
	n := len(buf) - mark
	hdr := 1 + uvarintLen(uint64(n))
	buf = append(buf, make([]byte, hdr)...)
	copy(buf[mark+hdr:], buf[mark:mark+n])
	buf[mark] = byte(field<<3 | 2)
	binary.PutUvarint(buf[mark+1:], uint64(n))
	return buf
}

// Proto3 scalars are omitted at their default value ...
func appendBytesField(buf []byte, field int, b []byte) []byte {
	if len(b) == 0 {
		return buf
	}
	return appendBytesFieldAlways(buf, field, b)
}

func appendStringField(buf []byte, field int, s string) []byte {
	if s == "" {
		return buf
	}
	return appendStringFieldAlways(buf, field, s)
}

func appendVarintField(buf []byte, field int, v uint64) []byte {
	if v == 0 {
		return buf
	}
	return appendVarintFieldAlways(buf, field, v)
}

func appendFixed64Field(buf []byte, field int, v uint64) []byte {
	if v == 0 {
		return buf
	}
	return appendFixed64FieldAlways(buf, field, v)
}

// ... oneof members and set messages are not.
func appendBytesFieldAlways(buf []byte, field int, b []byte) []byte {
	buf = append(buf, byte(field<<3|2))
	buf = binary.AppendUvarint(buf, uint64(len(b)))
	return append(buf, b...)
}

func appendStringFieldAlways(buf []byte, field int, s string) []byte {
	buf = append(buf, byte(field<<3|2))
	buf = binary.AppendUvarint(buf, uint64(len(s)))
	return append(buf, s...)
}

func appendVarintFieldAlways(buf []byte, field int, v uint64) []byte {
	buf = append(buf, byte(field<<3|0))
	return binary.AppendUvarint(buf, v)
}

func appendFixed64FieldAlways(buf []byte, field int, v uint64) []byte {
	buf = append(buf, byte(field<<3|1))
	return binary.LittleEndian.AppendUint64(buf, v)
}
