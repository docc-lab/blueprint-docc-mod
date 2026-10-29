package otelcol

// Tomislav-RetCtx (user 2026-10-29): one bridge attribute per span.  The layout of `_br`, `_d`
// and `_o` as appendBridgeWireBytes writes them, read back by hand and by splitBR.

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

// decodeKV parses the repeated KeyValue messages appendBridgeWireBytes appended to buf.
func decodeKV(t *testing.T, buf []byte) []*commonpb.KeyValue {
	t.Helper()
	// Wrap as a message with field 9 repeated KeyValue (a Span's attributes field).
	var s struct{}
	_ = s
	type attrs struct {
		*commonpb.InstrumentationScope
	}
	_ = attrs{}
	scope := &commonpb.InstrumentationScope{} // field 3 of InstrumentationScope is repeated KeyValue attributes
	// Re-tag field 9 as field 3 so the bytes parse as InstrumentationScope.attributes.
	retagged := append([]byte{}, buf...)
	for i := 0; i < len(retagged); {
		if retagged[i] != byte(9<<3|2) {
			t.Fatalf("unexpected tag %#x at %d", retagged[i], i)
		}
		retagged[i] = byte(3<<3 | 2)
		n, k := uvarintAt(retagged, i+1)
		i += 1 + k + int(n)
	}
	if err := proto.Unmarshal(retagged, scope); err != nil {
		t.Fatal(err)
	}
	return scope.Attributes
}

func uvarintAt(b []byte, i int) (uint64, int) {
	var v uint64
	var shift uint
	for k := 0; ; k++ {
		c := b[i+k]
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, k + 1
		}
		shift += 7
	}
}

func TestBridgeAttributeIsOnePerSpan(t *testing.T) {
	rc := string([]byte{0x01, 0x09, 0x03, 7, 8, 9}) // a packed reverse envelope, raw
	emit := []byte{4, 0xaa, 0xbb, 0xcc}
	w := &bridgeWire{emit: emit, depth: 300, structural: true, ordinal: 2}
	w.rev.rc = rc
	var scratch [2 * 10]byte

	// checkpoint with a reverse context: one `_br` = uvarint(len(rc)) || rc || payload
	kv := decodeKV(t, appendBridgeWireBytes(nil, w.bridgeBytes(&scratch), true))
	if len(kv) != 1 || kv[0].Key != AttrBREmit {
		t.Fatalf("checkpoint attributes: %v", kv)
	}
	want := append(append([]byte{byte(len(rc))}, rc...), emit...)
	if got := kv[0].Value.GetBytesValue(); !bytes.Equal(got, want) {
		t.Fatalf("_br = %x, want %x", got, want)
	}
	gotRC, gotEmit, ok := splitBR(kv[0].Value.GetBytesValue())
	if !ok || string(gotRC) != rc || !bytes.Equal(gotEmit, emit) {
		t.Fatalf("splitBR: %x %x %v", gotRC, gotEmit, ok)
	}

	// checkpoint without one: a single 0x00 then the payload
	w.rev.rc = ""
	kv = decodeKV(t, appendBridgeWireBytes(nil, w.bridgeBytes(&scratch), true))
	if got := kv[0].Value.GetBytesValue(); !bytes.Equal(got, append([]byte{0}, emit...)) {
		t.Fatalf("_br without rc = %x", got)
	}
	if gotRC, gotEmit, ok := splitBR(kv[0].Value.GetBytesValue()); !ok || gotRC != nil || !bytes.Equal(gotEmit, emit) {
		t.Fatalf("splitBR without rc: %x %x %v", gotRC, gotEmit, ok)
	}

	// ordinary SB span: one `_o` = uvarint(ordinal) || uvarint(depth)
	kv = decodeKV(t, appendBridgeWireBytes(nil, w.bridgeBytes(&scratch), false))
	if len(kv) != 1 || kv[0].Key != AttrO || !bytes.Equal(kv[0].Value.GetBytesValue(), []byte{2, 0xac, 0x02}) {
		t.Fatalf("ordinary SB attributes: %v", kv)
	}
	// ordinary PB / CGPB span: one `_d` = uvarint(depth)
	w.structural = false
	kv = decodeKV(t, appendBridgeWireBytes(nil, w.bridgeBytes(&scratch), false))
	if len(kv) != 1 || kv[0].Key != AttrD || !bytes.Equal(kv[0].Value.GetBytesValue(), []byte{0xac, 0x02}) {
		t.Fatalf("ordinary PB attributes: %v", kv)
	}
}

func TestSplitBRRejectsOverrun(t *testing.T) {
	if _, _, ok := splitBR(nil); ok {
		t.Fatal("empty value accepted")
	}
	if _, _, ok := splitBR([]byte{5, 1, 2}); ok {
		t.Fatal("overrunning length accepted")
	}
	if rc, emit, ok := splitBR([]byte{0}); !ok || rc != nil || len(emit) != 0 {
		t.Fatalf("bare prefix: %x %x %v", rc, emit, ok)
	}
}

func TestLegacyBridgeBytesFoldsTheAttributes(t *testing.T) {
	rc := string([]byte{0x01, 0x09, 0x03, 7, 8, 9})
	attrs := []attribute.KeyValue{
		attribute.String(AttrBREmit, base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})),
		attribute.String(AttrD, base64.RawURLEncoding.EncodeToString([]byte{4})),
		attribute.String(AttrO, "not base64!"),
		attribute.String(backend.ReverseTrussCheckpointKey, backend.RetCtxText(rc)),
	}
	b := legacyBridgeBytes(attrs, policyStructural)
	if b.rc != rc || !bytes.Equal(b.emit, []byte{1, 2, 3}) || !bytes.Equal(b.depth, []byte{4}) || b.ordinal == nil || len(b.ordinal) != 0 || !b.structural {
		t.Fatalf("legacy view: %+v", b)
	}
	kv := decodeKV(t, appendBridgeWireBytes(nil, b, true))
	if len(kv) != 1 || !bytes.Equal(kv[0].Value.GetBytesValue(), append(append([]byte{byte(len(rc))}, rc...), 1, 2, 3)) {
		t.Fatalf("legacy checkpoint: %v", kv)
	}
	kv = decodeKV(t, appendBridgeWireBytes(nil, b, false))
	if len(kv) != 1 || kv[0].Key != AttrO || !bytes.Equal(kv[0].Value.GetBytesValue(), []byte{4}) {
		t.Fatalf("legacy ordinary SB (malformed ordinal, depth 4): %v", kv)
	}
	if kv := decodeKV(t, appendBridgeWireBytes(nil, legacyBridgeBytes(nil, policyPath), false)); len(kv) != 0 {
		t.Fatalf("ordinary PB span without breadcrumbs wrote %v", kv)
	}
}
