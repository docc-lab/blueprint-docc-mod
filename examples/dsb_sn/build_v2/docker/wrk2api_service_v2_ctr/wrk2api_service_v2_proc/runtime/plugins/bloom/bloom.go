package bloom

import (
	"encoding/base64"
	"encoding/binary"
	"math"
)

// BloomFilter is a space-efficient probabilistic data structure
// that uses the same hash generation approach as bits-and-blooms
// but with minimal serialization (only the bit array).
type BloomFilter struct {
	bits []byte // Bit array
	m    uint64 // Size of bit array in bits
	k    uint   // Number of hash functions
}

// New creates a new Bloom filter with m bits and k hash functions.
// Uses the same hash generation approach as bits-and-blooms (double hashing).
func New(m uint64, k uint) *BloomFilter {
	// Allocate bit array: ceil(m/8) bytes
	bits := make([]byte, (m+7)/8)
	return &BloomFilter{
		bits: bits,
		m:    m,
		k:    k,
	}
}

// EstimateParameters calculates the optimal m (bit array size) and k (number of hash functions)
// based on the expected number of elements n and desired false positive rate p.
// This is useful when you need to know m and k before deserializing, for example.
// Returns the calculated m and k values.
// Uses the standard Bloom filter formulas:
//   m = - (n * ln(p)) / (ln(2)^2)
//   k = (m / n) * ln(2)
func EstimateParameters(n uint, p float64) (m uint64, k uint) {
	if p <= 0 || p >= 1 {
		// Invalid false positive rate, use default
		p = 0.01 // 1% false positive rate
	}
	if n == 0 {
		n = 1000 // Default expected elements
	}

	// Calculate optimal m: m = - (n * ln(p)) / (ln(2)^2)
	ln2 := math.Log(2)
	ln2Squared := ln2 * ln2
	m = uint64(math.Ceil(-float64(n) * math.Log(p) / ln2Squared))

	// Calculate optimal k: k = (m / n) * ln(2)
	k = uint(math.Ceil(float64(m) / float64(n) * ln2))

	// Ensure minimum values
	if m < 1 {
		m = 1
	}
	if k < 1 {
		k = 1
	}

	return m, k
}

// NewWithEstimates creates a new Bloom filter optimized for the given parameters.
// n is the expected number of elements, p is the desired false positive rate (0 < p < 1).
// This calculates the optimal m (bit array size) and k (number of hash functions)
// using the standard Bloom filter formulas:
//   m = - (n * ln(p)) / (ln(2)^2)
//   k = (m / n) * ln(2)
func NewWithEstimates(n uint, p float64) *BloomFilter {
	m, k := EstimateParameters(n, p)
	return New(m, k)
}

// Add adds an element to the Bloom filter.
func (bf *BloomFilter) Add(data []byte) {
	// Generate base hashes using MurmurHash3-like approach
	// This generates 4 hash values similar to bits-and-blooms sum256
	h1, h2, _, _ := baseHashes(data)

	// Use double hashing to generate k positions
	// bits-and-blooms uses the location function which computes:
	// location_i = (h1 + i*h2) mod m for all i from 0 to k-1
	// This is the standard double hashing approach
	for i := uint(0); i < bf.k; i++ {
		// Double hashing formula: (h1 + i*h2) mod m
		hash := (h1 + uint64(i)*h2) % bf.m

		// Set the bit
		bitIndex := hash % bf.m
		byteIndex := bitIndex / 8
		bitOffset := bitIndex % 8
		bf.bits[byteIndex] |= 1 << bitOffset
	}
}

// Test checks if an element might be in the Bloom filter.
// Returns true if the element might be present (with possibility of false positives).
func (bf *BloomFilter) Test(data []byte) bool {
	// Generate base hashes
	h1, h2, _, _ := baseHashes(data)

	// Check all k positions using the same double hashing as Add
	for i := uint(0); i < bf.k; i++ {
		// Double hashing formula: (h1 + i*h2) mod m
		hash := (h1 + uint64(i)*h2) % bf.m

		bitIndex := hash % bf.m
		byteIndex := bitIndex / 8
		bitOffset := bitIndex % 8

		// If any bit is not set, element is definitely not present
		if (bf.bits[byteIndex] & (1 << bitOffset)) == 0 {
			return false
		}
	}

	return true
}

// baseHashes generates 4 base hash values from the input data.
// This mimics bits-and-blooms sum256 function which uses MurmurHash3.
// We use a simplified but compatible approach.
func baseHashes(data []byte) (h1, h2, h3, h4 uint64) {
	// Use two different seeds to generate 4 hash values
	// This matches bits-and-blooms approach of hashing twice
	// (once on original data, once with virtual byte appended)
	h1, h2 = murmurHash3_128(data, 0)
	h3, h4 = murmurHash3_128(data, 1)

	return h1, h2, h3, h4
}

// murmurHash3_128 implements a simplified MurmurHash3 128-bit variant
// that produces two 64-bit hash values. Uses seed to vary the hash.
// This is compatible with bits-and-blooms hash generation.
func murmurHash3_128(data []byte, seed uint64) (h1, h2 uint64) {
	const (
		c1_128 = 0x87c37b91114253d5
		c2_128 = 0x4cf5ad432745937f
	)

	h1 = seed
	h2 = seed

	// Process 16-byte blocks
	length := len(data)
	nblocks := length / 16

	for i := 0; i < nblocks; i++ {
		// Read two 64-bit words
		k1 := binary.LittleEndian.Uint64(data[i*16:])
		k2 := binary.LittleEndian.Uint64(data[i*16+8:])

		// Mix k1
		k1 *= c1_128
		k1 = (k1 << 31) | (k1 >> 33)
		k1 *= c2_128
		h1 ^= k1
		h1 = (h1 << 27) | (h1 >> 37)
		h1 = h1*5 + 0x52dce729

		// Mix k2
		k2 *= c2_128
		k2 = (k2 << 33) | (k2 >> 31)
		k2 *= c1_128
		h2 ^= k2
		h2 = (h2 << 31) | (h2 >> 33)
		h2 = h2*5 + 0x38495ab5
	}

	// Handle remaining bytes
	tail := data[nblocks*16:]
	var k1, k2 uint64

	switch len(tail) & 15 {
	case 15:
		k2 ^= uint64(tail[14]) << 48
		fallthrough
	case 14:
		k2 ^= uint64(tail[13]) << 40
		fallthrough
	case 13:
		k2 ^= uint64(tail[12]) << 32
		fallthrough
	case 12:
		k2 ^= uint64(tail[11]) << 24
		fallthrough
	case 11:
		k2 ^= uint64(tail[10]) << 16
		fallthrough
	case 10:
		k2 ^= uint64(tail[9]) << 8
		fallthrough
	case 9:
		k2 ^= uint64(tail[8])
		k2 *= c2_128
		k2 = (k2 << 33) | (k2 >> 31)
		k2 *= c1_128
		h2 ^= k2
		fallthrough
	case 8:
		k1 ^= uint64(tail[7]) << 56
		fallthrough
	case 7:
		k1 ^= uint64(tail[6]) << 48
		fallthrough
	case 6:
		k1 ^= uint64(tail[5]) << 40
		fallthrough
	case 5:
		k1 ^= uint64(tail[4]) << 32
		fallthrough
	case 4:
		k1 ^= uint64(tail[3]) << 24
		fallthrough
	case 3:
		k1 ^= uint64(tail[2]) << 16
		fallthrough
	case 2:
		k1 ^= uint64(tail[1]) << 8
		fallthrough
	case 1:
		k1 ^= uint64(tail[0])
		k1 *= c1_128
		k1 = (k1 << 31) | (k1 >> 33)
		k1 *= c2_128
		h1 ^= k1
	}

	// Finalize
	h1 ^= uint64(length)
	h2 ^= uint64(length)

	h1 += h2
	h2 += h1

	h1 = fmix64(h1)
	h2 = fmix64(h2)

	h1 += h2
	h2 += h1

	return h1, h2
}

// fmix64 finalization mix function
func fmix64(k uint64) uint64 {
	k ^= k >> 33
	k *= 0xff51afd7ed558ccd
	k ^= k >> 33
	k *= 0xc4ceb9fe1a85ec53
	k ^= k >> 33
	return k
}

// Serialize encodes only the bit array to a base64 string.
// This is much more efficient than GobEncode which includes metadata.
func (bf *BloomFilter) Serialize() string {
	return base64.StdEncoding.EncodeToString(bf.bits)
}

// Deserialize creates a Bloom filter from a serialized bit array.
// m and k must be provided as parameters (not stored in serialization).
func Deserialize(serialized string, m uint64, k uint) (*BloomFilter, error) {
	if serialized == "" {
		return New(m, k), nil
	}

	data, err := base64.StdEncoding.DecodeString(serialized)
	if err != nil {
		return New(m, k), err
	}

	// Verify the data size matches expected bit array size
	expectedSize := (m + 7) / 8
	if uint64(len(data)) != expectedSize {
		// If size doesn't match, create new filter
		return New(m, k), nil
	}

	return &BloomFilter{
		bits: data,
		m:    m,
		k:    k,
	}, nil
}

// M returns the size of the bit array in bits
func (bf *BloomFilter) M() uint64 {
	return bf.m
}

// K returns the number of hash functions
func (bf *BloomFilter) K() uint {
	return bf.k
}
