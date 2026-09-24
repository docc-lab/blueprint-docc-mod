package backend

// Tomislav-RetCtx: reflection-free encoding of the RPC trace carrier.
//
// Every generated OT client wrapper (plugins/opentelemetry/ir_ot_client.go) sends
//
//	{"baggage":{...},"trace_ctx":{"TraceID":..,"SpanID":..,"TraceFlags":..,"TraceState":..,"Remote":..}}
//
// built by SpanContext.MarshalJSON and AddBaggageToTraceContext through encoding/json's
// reflection path. In the deployed hotel frontend (CPU profile 2026-09-23, 12k rps) that was
// 8.7 s of CPU per 30 s under vanilla and 12.2 s under the path bridge, whose baggage is
// larger. The encoder below writes the same bytes directly: keys in encoding/json's sorted
// order, strings escaped exactly as encoding/json escapes them (HTML-safe, invalid UTF-8 as
// U+FFFD, U+2028/U+2029 escaped). carrier_json_test.go checks byte equality against the
// reflection encoder.

import (
	"encoding/json"
	"slices"
	"unicode/utf8"

	"go.opentelemetry.io/otel/trace"
)

const hexDigits = "0123456789abcdef"

// appendJSONString appends s as a JSON string literal, escaped as encoding/json escapes it.
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' && b != '<' && b != '>' && b != '&' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '"', '\\':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, `�`...)
			i += size
			start = i
			continue
		}
		if r == ' ' || r == ' ' {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// appendJSONStringMap appends m as encoding/json encodes a map[string]string (sorted keys;
// nil as null).
func appendJSONStringMap(dst []byte, m map[string]string) []byte {
	if m == nil {
		return append(dst, "null"...)
	}
	var stack [16]string
	keys := stack[:0]
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	dst = append(dst, '{')
	for i, k := range keys {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendJSONString(dst, k)
		dst = append(dst, ':')
		dst = appendJSONString(dst, m[k])
	}
	return append(dst, '}')
}

// appendSpanContextJSON appends sc exactly as trace.SpanContext.MarshalJSON encodes it.
func appendSpanContextJSON(dst []byte, sc trace.SpanContext) []byte {
	tid, sid, flags := sc.TraceID(), sc.SpanID(), sc.TraceFlags()
	dst = append(dst, `{"TraceID":"`...)
	for _, b := range tid {
		dst = append(dst, hexDigits[b>>4], hexDigits[b&0xF])
	}
	dst = append(dst, `","SpanID":"`...)
	for _, b := range sid {
		dst = append(dst, hexDigits[b>>4], hexDigits[b&0xF])
	}
	dst = append(dst, `","TraceFlags":"`...)
	dst = append(dst, hexDigits[byte(flags)>>4], hexDigits[byte(flags)&0xF])
	dst = append(dst, `","TraceState":`...)
	dst = appendJSONString(dst, sc.TraceState().String())
	if sc.IsRemote() {
		return append(dst, `,"Remote":true}`...)
	}
	return append(dst, `,"Remote":false}`...)
}

// EncodeTraceCarrier returns the carrier the RPC wrappers send, byte-identical to
// AddBaggageToTraceContext(string(sc.MarshalJSON()), baggage).
func EncodeTraceCarrier(sc trace.SpanContext, baggage map[string]string) string {
	buf := make([]byte, 0, 160+64*len(baggage))
	buf = append(buf, `{"baggage":`...)
	buf = appendJSONStringMap(buf, baggage)
	buf = append(buf, `,"trace_ctx":`...)
	buf = appendSpanContextJSON(buf, sc)
	return string(append(buf, '}'))
}

// addBaggageFast is AddBaggageToTraceContext without reflection, for a trace context that
// is already a compact JSON object (what SpanContext.MarshalJSON produces); anything else
// takes the reflection path, which also validates and compacts it.
func addBaggageFast(traceContextJSON string, baggage map[string]string) (string, bool) {
	n := len(traceContextJSON)
	if n < 2 || traceContextJSON[0] != '{' || traceContextJSON[n-1] != '}' || !json.Valid([]byte(traceContextJSON)) {
		return "", false
	}
	for i := 0; i < n; i++ {
		if c := traceContextJSON[i]; c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			return "", false // not compact; let encoding/json compact it
		}
	}
	buf := make([]byte, 0, 32+n+64*len(baggage))
	buf = append(buf, `{"baggage":`...)
	buf = appendJSONStringMap(buf, baggage)
	buf = append(buf, `,"trace_ctx":`...)
	buf = append(buf, traceContextJSON...)
	return string(append(buf, '}')), true
}

// parseCarrierFast decodes a carrier in exactly the shape EncodeTraceCarrier (and the
// reflection encoder) produce. It reports false for anything else -- escapes, other key
// orders, whitespace, unexpected tokens -- and the caller then uses encoding/json, so the
// accepted language and every error stay exactly those of json.Unmarshal.
func parseCarrierFast(s string) (traceCtxWithBaggage, bool) {
	var out traceCtxWithBaggage
	i := 0
	lit := func(want string) bool {
		if len(s)-i < len(want) || s[i:i+len(want)] != want {
			return false
		}
		i += len(want)
		return true
	}
	// str reads a JSON string with no escapes or control characters.
	str := func() (string, bool) {
		if i >= len(s) || s[i] != '"' {
			return "", false
		}
		j := i + 1
		for ; j < len(s); j++ {
			c := s[j]
			if c == '"' {
				break
			}
			if c == '\\' || c < 0x20 {
				return "", false
			}
		}
		if j >= len(s) {
			return "", false
		}
		v := s[i+1 : j]
		if !utf8.ValidString(v) {
			return "", false
		}
		i = j + 1
		return v, true
	}
	if !lit(`{"baggage":`) {
		return out, false
	}
	if lit("null") {
		out.Baggage = nil
	} else {
		if !lit("{") {
			return out, false
		}
		out.Baggage = map[string]string{}
		if !lit("}") {
			for {
				k, ok := str()
				if !ok || !lit(":") {
					return out, false
				}
				v, ok := str()
				if !ok {
					return out, false
				}
				out.Baggage[k] = v
				if lit("}") {
					break
				}
				if !lit(",") {
					return out, false
				}
			}
		}
	}
	var ok bool
	if !lit(`,"trace_ctx":{"TraceID":`) {
		return out, false
	}
	if out.TraceCtx.TraceID, ok = str(); !ok || !lit(`,"SpanID":`) {
		return out, false
	}
	if out.TraceCtx.SpanID, ok = str(); !ok || !lit(`,"TraceFlags":`) {
		return out, false
	}
	if out.TraceCtx.TraceFlags, ok = str(); !ok || !lit(`,"TraceState":`) {
		return out, false
	}
	if out.TraceCtx.TraceState, ok = str(); !ok || !lit(`,"Remote":`) {
		return out, false
	}
	switch {
	case lit("true"):
		out.TraceCtx.Remote = true
	case lit("false"):
	default:
		return out, false
	}
	return out, lit("}}") && i == len(s)
}
