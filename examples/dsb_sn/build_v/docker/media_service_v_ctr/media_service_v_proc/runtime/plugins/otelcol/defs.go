package otelcol

import (
	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
)

// Constants for baggage keys
const (
	BAG_BLOOM_FILTER       = "__bag.bf"
	BAG_HASH_ARRAY         = "__bag.ha"
	BAG_ORDINAL_ARRAY      = "__bag.oa"
	BAG_END_EVENTS         = "__bag.ee"
	BAG_DELAYED_END_EVENTS = "__bag.dee"
)

// Ancestry tagging keys
const (
	AncestryModeKey  = "ancestry_mode"
	AncestryKey      = "ancestry"
	AncestryExtraKey = "ancestry2"
)

// AncestryMode selects the ancestry encoding strategy
type AncestryMode string

var (
	// AncestryModeBloom  AncestryMode = "bloom"
	// AncestryModeHash   AncestryMode = "hash"
	// AncestryModeHybrid AncestryMode = "hybrid"
	AncestryModePB     AncestryMode = AncestryMode(string([]byte{0x00}))
	AncestryModeHash   AncestryMode = AncestryMode(string([]byte{0x01}))
	AncestryModeHybrid AncestryMode = AncestryMode(string([]byte{0x02}))
)

// Manual toggle: when true, both high and low priority spans are exported
// via the high-priority OTLP client (single channel). When false, low
// priority spans use a separate OTLP client/endpoint.
const singleOTLPClient = true

// Bloom filter parameters (m = bit array size, k = number of hash functions)
var (
	BloomFilterM uint64 = 10 // Size of bit array in bits
	BloomFilterK uint   = 7  // Number of hash functions
)

// ConfigResponse represents the response from the config discovery endpoint
type ConfigResponse struct {
	Config map[string]interface{} `json:"config"`
}

// serializeBloomFilter converts a bloom filter to a base64-encoded string.
// Only serializes the bit array, not metadata (much more efficient than GobEncode).
func serializeBloomFilter(bf *bloom.BloomFilter) (string, error) {
	return bf.Serialize(), nil
}

// deserializeBloomFilter converts a base64-encoded string back to a bloom filter.
// Uses minimal deserialization (only the bit array).
func deserializeBloomFilter(serialized string) (*bloom.BloomFilter, error) {
	return bloom.Deserialize(serialized, BloomFilterM, BloomFilterK)
}

// createEmptyBloomFilter creates a new empty bloom filter
func createEmptyBloomFilter() *bloom.BloomFilter {
	return bloom.New(BloomFilterM, BloomFilterK)
}

// getBaggageKeys returns the keys from a baggage map for logging
func getBaggageKeys(baggage map[string]string) []string {
	keys := make([]string, 0, len(baggage))
	for k := range baggage {
		keys = append(keys, k)
	}
	return keys
}
