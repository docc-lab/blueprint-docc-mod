package interceptors

import (
	"encoding/binary"
	"errors"
	"unsafe"

	"go.opentelemetry.io/otel/trace"
)

// Compact trace carrier, sent from the client interceptor to the server interceptor.
//
// The wrappers send the forward context as JSON (backend.EncodeTraceCarrier in
// runtime/core/backend/carrier_json.go: about 150 bytes plus baggage) inside the request message,
// where it costs little more than a copy.  As a gRPC header the same JSON is expensive: HTTP/2
// compresses every header on the way out and decompresses it on the way in (HPACK), and because the
// value differs on every call it also keeps replacing entries in the connection's header table.
// Profiling showed that as the largest part of the interceptors' extra CPU
// (utils/grpc_interceptors/README.md, "Profiling").
//
// So between interceptors the context travels in this compact form instead: a small binary record,
// sent as text in the header TraceCtxKey, written with headerEncoding (header.go: base64 with an
// alphabet HPACK does not Huffman-code).  It carries exactly what the JSON carries, and
// decodeCarrier returns exactly what backend.GetSpanContext returns for the JSON (carrier_test.go
// checks this).
//
// Why text under a normal key and not raw bytes under a "-bin" key: for "-bin" keys gRPC does
// standard base64 itself, on both ends, with extra copies (four allocations per call in total,
// measured with bench_test.go), and HPACK then Huffman-codes it; here the client encodes straight
// into the final string and the server decodes into one buffer.
//
// Binary layout before base64 (n = unsigned varint):
//
//	sampled trace:    0x01 | trace ID (16 bytes) | span ID (8) | trace flags (1)
//	                  | n len, trace state | n entries, then per baggage entry: n len, key, n len, value
//	unsampled trace:  0x00 | trace ID (16 bytes) | span ID (8)
//
// The unsampled form replaces the wrappers' "U:<trace id hex><span id hex>" text carrier.

const (
	carrierUnsampled = 0x00
	carrierSampled   = 0x01
)

var (
	carrierEncoding = headerEncoding // header.go
	errBadCarrier   = errors.New("malformed trace carrier")
)

// encodeCarrier builds the carrier for a sampled span.  Called by the client interceptor (client.go).
func encodeCarrier(sc trace.SpanContext, baggage map[string]string) string {
	var stack [128]byte // the binary record is built here when it fits (it always does without baggage)
	buf := stack[:0]
	tid, sid := sc.TraceID(), sc.SpanID()
	ts := sc.TraceState().String()
	buf = append(buf, carrierSampled)
	buf = append(buf, tid[:]...)
	buf = append(buf, sid[:]...)
	buf = append(buf, byte(sc.TraceFlags()))
	buf = binary.AppendUvarint(buf, uint64(len(ts)))
	buf = append(buf, ts...)
	buf = binary.AppendUvarint(buf, uint64(len(baggage)))
	for k, v := range baggage {
		buf = binary.AppendUvarint(buf, uint64(len(k)))
		buf = append(buf, k...)
		buf = binary.AppendUvarint(buf, uint64(len(v)))
		buf = append(buf, v...)
	}
	return toBase64(buf) // below
}

// encodeUnsampledCarrier builds the carrier for a span that head sampling dropped: only the IDs, so
// downstream hops stay in the same (unsampled) trace.  Called by the client interceptor (client.go).
func encodeUnsampledCarrier(sc trace.SpanContext) string {
	var buf [1 + 16 + 8]byte
	tid, sid := sc.TraceID(), sc.SpanID()
	buf[0] = carrierUnsampled
	copy(buf[1:17], tid[:])
	copy(buf[17:25], sid[:])
	return toBase64(buf[:]) // below
}

// toBase64 encodes raw into a new string with a single allocation: the encoded bytes become the
// string's memory directly (they are never written again), instead of being copied into a string.
func toBase64(raw []byte) string {
	out := make([]byte, carrierEncoding.EncodedLen(len(raw)))
	carrierEncoding.Encode(out, raw)
	return unsafe.String(unsafe.SliceData(out), len(out))
}

// decodeCarrier reads a carrier; full is false for the unsampled form.  For a sampled carrier it
// returns the same span context config and baggage as backend.GetSpanContext does for the wrappers'
// JSON (Remote is false there, as here; the server marks the context remote when it installs it).
// The baggage map is never nil for a sampled carrier, matching the JSON, which always has a
// "baggage" object.  Called by the server interceptor (server.go).
func decodeCarrier(s string) (cfg trace.SpanContextConfig, baggage map[string]string, full bool, err error) {
	// Decode into one new buffer.  The baggage strings below point into it (unsafe.String) instead
	// of being copied one by one; the buffer is never written again, so they stay valid.
	raw := make([]byte, 0, carrierEncoding.DecodedLen(len(s)))
	raw, err = carrierEncoding.AppendDecode(raw, unsafe.Slice(unsafe.StringData(s), len(s)))
	if err != nil || len(raw) < 1+16+8 {
		return trace.SpanContextConfig{}, nil, false, errBadCarrier
	}
	copy(cfg.TraceID[:], raw[1:17])
	copy(cfg.SpanID[:], raw[17:25])
	switch raw[0] {
	case carrierUnsampled:
		return cfg, nil, false, nil
	case carrierSampled:
	default:
		return trace.SpanContextConfig{}, nil, false, errBadCarrier
	}
	if len(raw) < 26 {
		return trace.SpanContextConfig{}, nil, false, errBadCarrier
	}
	cfg.TraceFlags = trace.TraceFlags(raw[25])
	rest := raw[26:]

	// next reads one length-prefixed string from rest.
	next := func() (string, bool) {
		n, w := binary.Uvarint(rest)
		if w <= 0 || uint64(len(rest)-w) < n {
			return "", false
		}
		v := rest[w : w+int(n)]
		rest = rest[w+int(n):]
		return unsafe.String(unsafe.SliceData(v), len(v)), true
	}

	ts, ok := next()
	if !ok {
		return trace.SpanContextConfig{}, nil, false, errBadCarrier
	}
	if cfg.TraceState, err = trace.ParseTraceState(ts); err != nil {
		return trace.SpanContextConfig{}, nil, false, err
	}
	count, w := binary.Uvarint(rest)
	if w <= 0 || count > uint64(len(rest)) {
		return trace.SpanContextConfig{}, nil, false, errBadCarrier
	}
	rest = rest[w:]
	baggage = make(map[string]string, count)
	for i := uint64(0); i < count; i++ {
		k, ok1 := next()
		v, ok2 := next()
		if !ok1 || !ok2 {
			return trace.SpanContextConfig{}, nil, false, errBadCarrier
		}
		baggage[k] = v
	}
	if len(rest) != 0 { // bytes after a complete record: not something encodeCarrier wrote
		return trace.SpanContextConfig{}, nil, false, errBadCarrier
	}
	return cfg, baggage, true, nil
}
