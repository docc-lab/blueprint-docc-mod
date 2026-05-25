package critpath

import (
	"sort"

	"github.com/blueprint-uservices/blueprint/runtime/plugins/bloom"
)

type StartEnd struct {
	Start uint64
	End   uint64
}

func ComputeCriticalPath(spans map[string]StartEnd) []string {
	if len(spans) == 0 {
		return []string{}
	}

	criticalPath := []string{}

	// Produce a list of spans sorted in reverse order of end time
	sortedSpans := make([]string, 0, len(spans))
	for spanID := range spans {
		sortedSpans = append(sortedSpans, spanID)
	}
	sort.Slice(sortedSpans, func(i, j int) bool {
		return spans[sortedSpans[i]].End > spans[sortedSpans[j]].End
	})

	lastEndingChild := sortedSpans[0]
	criticalPath = append(criticalPath, lastEndingChild)
	for i := 1; i < len(sortedSpans); i++ {
		// Check whether the current span ends before the last
		// ending child starts - if so, add to critical path
		if spans[sortedSpans[i]].End < spans[lastEndingChild].Start {
			criticalPath = append(criticalPath, sortedSpans[i])
			lastEndingChild = sortedSpans[i]
		}
	}

	return criticalPath
}

// ComputeCriticalPathWithBloom computes the critical path and returns it along with
// a bloom filter containing all non-critical-path span IDs.
// Returns (criticalPath, bloomFilter) where criticalPath is []string and bloomFilter is *bloom.BloomFilter.
func ComputeCriticalPathWithBloom(spans map[string]StartEnd) ([]string, *bloom.BloomFilter) {
	if len(spans) == 0 {
		m, k := bloom.EstimateParameters(0, 0.01)
		return []string{}, bloom.New(m, k)
	}

	criticalPath := []string{}
	nonCriticalPath := []string{}

	// Produce a list of spans sorted in reverse order of end time
	sortedSpans := make([]string, 0, len(spans))
	for spanID := range spans {
		sortedSpans = append(sortedSpans, spanID)
	}
	sort.Slice(sortedSpans, func(i, j int) bool {
		return spans[sortedSpans[i]].End > spans[sortedSpans[j]].End
	})

	lastEndingChild := sortedSpans[0]
	criticalPath = append(criticalPath, lastEndingChild)
	for i := 1; i < len(sortedSpans); i++ {
		// Check whether the current span ends before the last
		// ending child starts - if so, add to critical path
		if spans[sortedSpans[i]].End < spans[lastEndingChild].Start {
			criticalPath = append(criticalPath, sortedSpans[i])
			lastEndingChild = sortedSpans[i]
		} else {
			// Span doesn't satisfy inequality - add to non-critical path
			nonCriticalPath = append(nonCriticalPath, sortedSpans[i])
		}
	}

	// Initialize bloom filter with the right number of elements for non-critical section
	var bf *bloom.BloomFilter
	if len(nonCriticalPath) > 0 {
		m, k := bloom.EstimateParameters(uint(len(nonCriticalPath)), 0.01) // 1% false positive rate
		bf = bloom.New(m, k)
		// Fill bloom filter with non-critical elements
		for _, spanID := range nonCriticalPath {
			bf.Add([]byte(spanID))
		}
	} else {
		// Empty bloom filter if no non-critical-path elements - sized for 0 elements
		m, k := bloom.EstimateParameters(0, 0.01)
		bf = bloom.New(m, k)
	}

	return criticalPath, bf
}
