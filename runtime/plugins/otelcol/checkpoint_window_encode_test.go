package otelcol

// Tomislav-RetCtx: the single-buffer window encoder must produce exactly the bytes of the
// filter-object path it replaced.

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
	"go.opentelemetry.io/otel/trace"
)

func referenceWindowPayloads(r checkpointRange, depth int, anchor [8]byte, distance int, inherited, ha []byte,
	isCheckpoint bool, outgoingTTL byte, sid trace.SpanID) (emit, wrapped []byte) {
	geometry := checkpointBlooms[distance-1]
	filter := bloom.NewFromBytes(inherited, geometry.m, geometry.k)
	emit = packCheckpointWindowBR(depth, anchor, distance, filter.Bytes(), ha)
	if isCheckpoint {
		anchor = [8]byte(sid)
		distance = int(outgoingTTL) + 1
		geometry = checkpointBlooms[distance-1]
		filter = bloom.New(geometry.m, geometry.k)
		ha = nil
	} else {
		filter.AddPrehashed(sid[:])
	}
	return emit, r.wrap(outgoingTTL, packCheckpointWindowBR(depth, anchor, distance, filter.Bytes(), ha))
}

func TestEncodeWindowPayloadsMatchesFilterPath(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 20000; i++ {
		r := checkpointRange{2, 6}
		if i%5 == 0 {
			r = checkpointRange{}
		}
		distance := 1 + rng.Intn(256)
		g := checkpointBlooms[distance-1]
		var inherited []byte
		switch rng.Intn(4) {
		case 0: // none (root / invalid parent)
		case 1: // wrong size: starts empty
			inherited = make([]byte, rng.Intn(g.bytes+3))
			rng.Read(inherited)
		default:
			inherited = make([]byte, g.bytes)
			rng.Read(inherited)
		}
		var ha []byte
		if rng.Intn(3) == 0 {
			ha = make([]byte, 9*rng.Intn(4))
			rng.Read(ha)
		}
		var anchor [8]byte
		var sid trace.SpanID
		rng.Read(anchor[:])
		rng.Read(sid[:])
		depth := rng.Intn(1 << uint(rng.Intn(20)))
		isCheckpoint := rng.Intn(2) == 0
		ttl := byte(rng.Intn(256))
		we, ww := referenceWindowPayloads(r, depth, anchor, distance, inherited, ha, isCheckpoint, ttl, sid)
		ge, gw := encodeWindowPayloads(r, depth, anchor, distance, inherited, ha, isCheckpoint, ttl, sid)
		if !bytes.Equal(we, ge) || !bytes.Equal(ww, gw) {
			t.Fatalf("case %d: emit %v wrapped %v differ", i, bytes.Equal(we, ge), bytes.Equal(ww, gw))
		}
	}
}
