package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const maxPayloadBytes = 1 << 20
const maxPayloadBuckets = 128

type weightedSize struct {
	Bytes  *int     `json:"bytes"`
	Weight *float64 `json:"weight"`
}

type distributionSpec struct {
	Source  string         `json:"source,omitempty"`
	Type    string         `json:"type"`
	Min     *int           `json:"min,omitempty"`
	Max     *int           `json:"max,omitempty"`
	Values  []weightedSize `json:"values,omitempty"`
	Samples []*int         `json:"samples,omitempty"`
}

type probabilitySize struct {
	Bytes       int     `json:"bytes"`
	Probability float64 `json:"probability"`
}

type payloadBucket struct {
	Min   int    `json:"min_bytes"`
	Max   int    `json:"max_bytes"`
	Count uint64 `json:"count"`
}

type resolvedDistribution struct {
	Type          string            `json:"type"`
	Sampler       string            `json:"sampler"`
	Source        string            `json:"source"`
	Min           int               `json:"min_bytes"`
	Max           int               `json:"max_bytes"`
	Mean          float64           `json:"expected_mean_bytes"`
	Probabilities []probabilitySize `json:"probabilities,omitempty"`
}

type sizeDistribution struct {
	resolved     resolvedDistribution
	cdf          []float64
	buckets      []payloadBucket
	bucketWidth  int
	exactBuckets bool
}

func validPayloadSize(n int) bool { return n >= 0 && n <= maxPayloadBytes }

// Tomislav-RetCtx: normalize and sort point masses once, outside generation.
// Empirical samples become a histogram; input order has no sampling meaning.
func compileDistribution(spec distributionSpec) (*sizeDistribution, error) {
	if strings.TrimSpace(spec.Source) == "" {
		return nil, errors.New("distribution requires a source description")
	}
	d := &sizeDistribution{resolved: resolvedDistribution{Type: spec.Type, Source: spec.Source, Sampler: "splitmix64-cdf-v1"}}
	var masses map[int]float64
	switch spec.Type {
	case "uniform":
		if spec.Min == nil || spec.Max == nil || spec.Values != nil || spec.Samples != nil {
			return nil, errors.New("uniform distribution requires min/max only")
		}
		if !validPayloadSize(*spec.Min) || !validPayloadSize(*spec.Max) || *spec.Min > *spec.Max {
			return nil, errors.New("uniform bounds must satisfy 0 <= min <= max <= 1048576")
		}
		d.resolved.Min, d.resolved.Max = *spec.Min, *spec.Max
		d.resolved.Mean = float64(*spec.Min+*spec.Max) / 2
	case "discrete":
		if spec.Min != nil || spec.Max != nil || spec.Samples != nil || len(spec.Values) == 0 {
			return nil, errors.New("discrete distribution requires a nonempty values array only")
		}
		maxWeight := 0.0
		for _, v := range spec.Values {
			if v.Bytes == nil || !validPayloadSize(*v.Bytes) || v.Weight == nil || math.IsNaN(*v.Weight) || math.IsInf(*v.Weight, 0) || *v.Weight < 0 {
				return nil, errors.New("each discrete entry needs bytes in 0..1048576 and a finite nonnegative weight")
			}
			maxWeight = max(maxWeight, *v.Weight)
		}
		if maxWeight == 0 {
			return nil, errors.New("distribution needs at least one positive weight")
		}
		masses = make(map[int]float64)
		for _, v := range spec.Values {
			if *v.Weight == 0 {
				continue
			}
			weight := *v.Weight / maxWeight
			if weight == 0 {
				return nil, errors.New("weights exceed the supported floating-point dynamic range")
			}
			masses[*v.Bytes] += weight
		}
	case "empirical":
		if spec.Min != nil || spec.Max != nil || spec.Values != nil || len(spec.Samples) == 0 {
			return nil, errors.New("empirical distribution requires a nonempty samples array only")
		}
		masses = make(map[int]float64)
		for _, n := range spec.Samples {
			if n == nil || !validPayloadSize(*n) {
				return nil, errors.New("empirical samples must be integers in 0..1048576")
			}
			masses[*n]++
		}
	default:
		return nil, fmt.Errorf("unknown distribution type %q; use discrete, empirical, or uniform", spec.Type)
	}
	if masses != nil {
		sizes := make([]int, 0, len(masses))
		for n := range masses {
			sizes = append(sizes, n)
		}
		sort.Ints(sizes)
		sum := 0.0
		for _, n := range sizes {
			sum += masses[n]
		}
		cumulative, previous := 0.0, 0.0
		for _, n := range sizes {
			p := masses[n] / sum
			cumulative += masses[n]
			next := cumulative / sum
			if next <= previous {
				return nil, errors.New("weights exceed the supported floating-point CDF precision")
			}
			previous = next
			d.cdf = append(d.cdf, next)
			d.resolved.Probabilities = append(d.resolved.Probabilities, probabilitySize{n, p})
			d.resolved.Mean += float64(n) * p
		}
		d.resolved.Min, d.resolved.Max = sizes[0], sizes[len(sizes)-1]
		if len(sizes) <= maxPayloadBuckets {
			d.exactBuckets = true
			for _, n := range sizes {
				d.buckets = append(d.buckets, payloadBucket{Min: n, Max: n})
			}
		}
	}
	if !d.exactBuckets {
		width := d.resolved.Max - d.resolved.Min + 1
		d.bucketWidth = (width + maxPayloadBuckets - 1) / maxPayloadBuckets
		for lo := d.resolved.Min; lo <= d.resolved.Max; lo += d.bucketWidth {
			d.buckets = append(d.buckets, payloadBucket{Min: lo, Max: min(lo+d.bucketWidth-1, d.resolved.Max)})
		}
	}
	return d, nil
}

// Tomislav-RetCtx: a counter-based draw ties size to checkpoint ordinal and
// seed, independent of worker scheduling, batch boundaries, or RPC completion.
// These random values are for simulation, not cryptographic use.
func mixSizeBits(x uint64) uint64 {
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func (d *sizeDistribution) sample(seed, ordinal uint64) (int, int) {
	x := mixSizeBits(seed + (ordinal+1)*0x9e3779b97f4a7c15)
	if d.resolved.Type == "uniform" {
		n := uint64(d.resolved.Max - d.resolved.Min + 1)
		// Reject the tiny incomplete interval so modulo does not bias integers.
		threshold := -n % n
		for x < threshold {
			x = mixSizeBits(x + 0x9e3779b97f4a7c15)
		}
		size := d.resolved.Min + int(x%n)
		return size, (size - d.resolved.Min) / d.bucketWidth
	}
	u := float64(x>>11) * (1.0 / (1 << 53))
	i := sort.Search(len(d.cdf), func(i int) bool { return d.cdf[i] > u })
	size := d.resolved.Probabilities[i].Bytes
	if d.exactBuckets {
		return size, i
	}
	return size, (size - d.resolved.Min) / d.bucketWidth
}
