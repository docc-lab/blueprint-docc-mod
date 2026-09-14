package backend

import (
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
)

// Inspect returned checkpoint locations and verify legacy ancestry AMQs.
// To run: RTCTX="<retctx-base64>" go test ./core/backend/ -run TestRTVerify -v
func TestRTVerify(t *testing.T) {
	s := os.Getenv("RTCTX")
	if s == "" {
		t.Skip("set RTCTX=<retctx-base64> to verify")
	}
	fp, parent, m, k, amqs := DecodeRetCtx(s)
	checkpoints, err := DecodeReturnedCheckpoints(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(amqs) == 0 && len(checkpoints) == 0 {
		t.Fatal("retCtx contains no ancestry AMQs or returned checkpoints")
	}
	returned := make(map[string]bool)
	for _, checkpoint := range checkpoints {
		returned[checkpoint.SpanID.String()] = true
		ttl := "absent (probability or legacy)"
		if checkpoint.ReverseTTL != nil {
			ttl = strconv.Itoa(int(*checkpoint.ReverseTTL))
		}
		t.Logf("returned checkpoint: kind=%s spanID=%s depth=%d reverseTTL=%s truss=%x",
			checkpoint.Kind, checkpoint.SpanID, checkpoint.Depth, ttl, checkpoint.Truss)
	}
	t.Logf("fingerprints: %s", fp)
	t.Logf("parentID: %q", parent)
	t.Logf("geometry: m=%d k=%d   AMQ segments: %d", m, k, len(amqs))
	if m == 0 || k == 0 { // pre-geometry trusses / fallback
		m, k = bloom.EstimateParameters(8, 0.0001)
	}
	for _, f := range strings.Split(fp, ",") {
		if returned[f] {
			continue // This origin has a typed SDK truss, not a legacy AMQ.
		}
		b, err := hex.DecodeString(f)
		if err != nil {
			continue
		}
		hit := false
		for i, seg := range amqs {
			if bloom.NewFromBytes(seg, m, k).TestPrehashed(b) {
				t.Logf("  %s -> present in AMQ segment %d  OK", f, i)
				hit = true
			}
		}
		if !hit {
			t.Errorf("  %s -> NOT in any segment (would mean dummy/empty)", f)
		}
	}
}
