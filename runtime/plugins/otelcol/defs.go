package otelcol

import (
	"encoding/base64"

	"github.com/bits-and-blooms/bloom"
)

// Constants for baggage keys
const (
	BAG_BLOOM_FILTER = "__bag.bf"
	BAG_HASH_ARRAY   = "__bag.ha"
)

// Ancestry tagging keys
const (
	AncestryModeKey = "ancestry_mode"
	AncestryKey     = "ancestry"
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

// ConfigResponse represents the response from the config discovery endpoint
type ConfigResponse struct {
	Config map[string]interface{} `json:"config"`
}

// serializeBloomFilter converts a bloom filter to a base64-encoded string
func serializeBloomFilter(bf *bloom.BloomFilter) (string, error) {
	data, err := bf.GobEncode()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// deserializeBloomFilter converts a base64-encoded string back to a bloom filter
func deserializeBloomFilter(serialized string) (*bloom.BloomFilter, error) {
	if serialized == "" {
		return createEmptyBloomFilter(), nil
	}

	data, err := base64.StdEncoding.DecodeString(serialized)
	if err != nil {
		return createEmptyBloomFilter(), err
	}

	bf := &bloom.BloomFilter{}
	err = bf.GobDecode(data)
	if err != nil {
		return createEmptyBloomFilter(), err
	}

	return bf, nil
}

// createEmptyBloomFilter creates a new empty bloom filter
func createEmptyBloomFilter() *bloom.BloomFilter {
	return bloom.New(10, 7) // Same parameters as existing
}

// getBaggageKeys returns the keys from a baggage map for logging
func getBaggageKeys(baggage map[string]string) []string {
	keys := make([]string, 0, len(baggage))
	for k := range baggage {
		keys = append(keys, k)
	}
	return keys
}
