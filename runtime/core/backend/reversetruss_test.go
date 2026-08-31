package backend

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
)

// Verify a retCtx holds real ancestry
// To run: RTCTX="<retctx-base64>" go test ./core/backend/ -run TestRTVerify -v
func TestRTVerify(t *testing.T) {
	s := os.Getenv("RTCTX")
	if s == "" {
		t.Skip("set RTCTX=<retctx-base64> to verify")
	}
	fp, parent, m, k, amqs := DecodeRetCtx(s)
	t.Logf("fingerprints: %s", fp)
	t.Logf("parentID: %q", parent)
	t.Logf("geometry: m=%d k=%d   AMQ segments: %d", m, k, len(amqs))
	if m == 0 || k == 0 { // pre-geometry trusses / fallback
		m, k = bloom.EstimateParameters(8, 0.0001)
	}
	for _, f := range strings.Split(fp, ",") {
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
