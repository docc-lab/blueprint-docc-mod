package main

import (
	"io"
	"math"
	"testing"
)

func TestCheckpointFractionSampling(t *testing.T) {
	c := testConfig(t, "--bridge", "pb", "--checkpoint-fraction", "0.5250588161787073", "--payload-seed", "42", "--bridge-distribution", tempJSON(t, mixtureJSON))
	p := testProfile(t, c)
	const draws = 500000
	count, sum := 0, 0
	for position := uint64(0); position < draws; position++ {
		checkpoint, ordinal := c.checkpointAt(position)
		if ordinal != position {
			t.Fatal("fractional size draws must use global span position")
		}
		if checkpoint {
			count++
			size, _ := p.distribution.sample(c.Seed, ordinal)
			sum += size
		}
	}
	fraction := *c.CheckpointFraction
	if math.Abs(float64(count)-draws*fraction) > 6*math.Sqrt(draws*fraction*(1-fraction)) {
		t.Fatalf("checkpoint fraction: %d/%d", count, draws)
	}
	// Conditional size draws should retain the requested mixture.
	if math.Abs(float64(sum)/float64(count)-24.8) > 0.35 {
		t.Fatal("membership biased the conditional size mixture")
	}
	b := newBatchBuilder(c, p, [8]byte{1})
	req, checkpoints := b.build(91, 121)
	var actual uint64
	for i, s := range req.ResourceSpans[0].ScopeSpans[0].Spans {
		selected, ordinal := c.checkpointAt(uint64(91 + i))
		attr := s.Attributes[0]
		if selected {
			actual++
			size, _ := p.distribution.sample(c.Seed, ordinal)
			if attr.Key != "_br" || len(attr.Value.GetBytesValue()) != size {
				t.Fatal("checkpoint value mismatch")
			}
		} else if attr.Key != "_d" {
			t.Fatal("ordinary span mismatch")
		}
	}
	if actual != checkpoints || actual != b.payloadCounts.Count {
		t.Fatal("checkpoint accounting mismatch")
	}
}

func TestCheckpointFractionBoundsAndValidation(t *testing.T) {
	for _, value := range []string{"0", "1"} {
		c := testConfig(t, "--bridge", "sb", "--checkpoint-fraction", value, "--bridge-bytes", "10")
		_, count := newBatchBuilder(c, testProfile(t, c), [8]byte{1}).build(73, 100)
		if (value == "0" && count != 0) || (value == "1" && count != 100) {
			t.Fatal("fraction boundary")
		}
	}
	for _, args := range [][]string{
		{"--checkpoint-fraction", "0.5"},
		{"--bridge", "pb", "--checkpoint-fraction", "NaN"},
		{"--bridge", "pb", "--checkpoint-fraction", "Inf"},
		{"--bridge", "pb", "--checkpoint-fraction", "1.1"},
		{"--bridge", "pb", "--checkpoint-fraction", "-0.1"},
		{"--bridge", "pb", "--checkpoint-fraction", "0.5", "--cpd", "2"},
	} {
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
