package bloom

import (
	"encoding/binary"
	"encoding/hex"
	"math/rand"
	"testing"
)

func randID(r *rand.Rand) []byte {
	b := make([]byte, 8) // 8-byte span-ID-like random key
	binary.BigEndian.PutUint64(b, r.Uint64())
	return b
}

// No false negatives: everything added via AddPrehashed must TestPrehashed true.
func TestPrehashedRoundtrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	bf := NewWithEstimates(5, 0.01) // bridge-sized: capacity 5 (cpd=6), 1% target FPR
	added := make([][]byte, 5)
	for i := range added {
		added[i] = randID(r)
		bf.AddPrehashed(added[i])
	}
	for i, id := range added {
		if !bf.TestPrehashed(id) {
			t.Fatalf("false negative on added id #%d (%x)", i, id)
		}
	}
}

// FPR sanity: with capacity-many elements the measured FPR on unseen random keys
// should be in the same ballpark as the murmur path (not ~100%, not 0%).
func TestPrehashedFPRSane(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	const cap, trials = 5, 200000
	fpr := func(prehashed bool) float64 {
		bf := NewWithEstimates(cap, 0.01)
		for i := 0; i < cap; i++ {
			id := randID(r)
			if prehashed { bf.AddPrehashed(id) } else { bf.Add(id) }
		}
		fp := 0
		for i := 0; i < trials; i++ {
			id := randID(r)
			if prehashed { if bf.TestPrehashed(id) { fp++ } } else { if bf.Test(id) { fp++ } }
		}
		return float64(fp) / trials
	}
	pre, mur := fpr(true), fpr(false)
	t.Logf("FPR: prehashed=%.4f murmur=%.4f", pre, mur)
	if pre > 0.10 {
		t.Fatalf("prehashed FPR too high: %.4f (expected ~murmur=%.4f)", pre, mur)
	}
}

// Full per-span non-checkpoint bloom path (NewFromBytes + Add + Bytes), to split
// the hashing cost from the allocation cost (NewFromBytes/Bytes copies).
func benchPath(b *testing.B, mode string) {
	r := rand.New(rand.NewSource(4))
	m, k := EstimateParameters(5, 0.01)
	parent := New(m, k).Bytes()
	ids := make([][]byte, 1024)
	hexes := make([][]byte, 1024)
	for i := range ids {
		ids[i] = randID(r)
		hexes[i] = []byte(hex.EncodeToString(ids[i])) // what the OLD code passed to Add
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf := NewFromBytes(parent, m, k)
		switch mode {
		case "murmur":
			bf.Add(hexes[i&1023])
		case "prehashed":
			bf.AddPrehashed(ids[i&1023])
		case "alloconly": // no Add — isolates NewFromBytes + Bytes cost
		}
		_ = bf.Bytes()
	}
}
func BenchmarkPathMurmur(b *testing.B)    { benchPath(b, "murmur") }
func BenchmarkPathPrehashed(b *testing.B) { benchPath(b, "prehashed") }
func BenchmarkPathAllocOnly(b *testing.B) { benchPath(b, "alloconly") }

func BenchmarkAddMurmur(b *testing.B) {
	r := rand.New(rand.NewSource(3))
	ids := make([][]byte, 1024)
	for i := range ids { ids[i] = randID(r) }
	bf := NewWithEstimates(5, 0.01)
	b.ResetTimer()
	for i := 0; i < b.N; i++ { bf.Add(ids[i&1023]) }
}

func BenchmarkAddPrehashed(b *testing.B) {
	r := rand.New(rand.NewSource(3))
	ids := make([][]byte, 1024)
	for i := range ids { ids[i] = randID(r) }
	bf := NewWithEstimates(5, 0.01)
	b.ResetTimer()
	for i := 0; i < b.N; i++ { bf.AddPrehashed(ids[i&1023]) }
}
