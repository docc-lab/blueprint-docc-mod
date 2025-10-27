package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// RequestStat represents a single request statistic
type RequestStat struct {
	UserID     string        `json:"user_id"`
	Operation  string        `json:"operation"`
	Duration   time.Duration `json:"duration_ms"`
	Success    bool          `json:"success"`
	Error      string        `json:"error,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// WorkloadStats aggregates all workload statistics
type WorkloadStats struct {
	TotalRequests      int            `json:"total_requests"`
	SuccessfulRequests int            `json:"successful_requests"`
	FailedRequests     int            `json:"failed_requests"`
	AverageLatency     time.Duration  `json:"average_latency_ms"`
	MaxLatency         time.Duration  `json:"max_latency_ms"`
	MinLatency         time.Duration  `json:"min_latency_ms"`
	Requests           []RequestStat  `json:"requests"`
	OperationStats     map[string]int `json:"operation_stats"`
	ErrorStats         map[string]int `json:"error_stats"`
	StatusCodeStats    map[int]int    `json:"status_code_stats"`
	StartTime          time.Time      `json:"start_time"`
	EndTime            time.Time      `json:"end_time"`
	TotalDuration      time.Duration  `json:"total_duration_ms"`
}

// StatsCollector manages statistics collection in a thread-safe manner
type StatsCollector struct {
	stats *WorkloadStats
	mutex sync.Mutex
}

// NewStatsCollector creates a new statistics collector
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		stats: &WorkloadStats{
			Requests:        make([]RequestStat, 0),
			OperationStats:  make(map[string]int),
			ErrorStats:      make(map[string]int),
			StatusCodeStats: make(map[int]int),
			MinLatency:      time.Hour, // Initialize with high value
			StartTime:       time.Now(),
		},
	}
}

// RecordStat records a single request statistic
func (sc *StatsCollector) RecordStat(stat RequestStat) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	sc.stats.Requests = append(sc.stats.Requests, stat)
	sc.stats.TotalRequests++

	if stat.Success {
		sc.stats.SuccessfulRequests++
	} else {
		sc.stats.FailedRequests++
	}

	// Update latency stats
	if stat.Duration < sc.stats.MinLatency {
		sc.stats.MinLatency = stat.Duration
	}
	if stat.Duration > sc.stats.MaxLatency {
		sc.stats.MaxLatency = stat.Duration
	}

	// Update operation stats
	sc.stats.OperationStats[stat.Operation]++

	// Update error stats
	if !stat.Success && stat.Error != "" {
		sc.stats.ErrorStats[stat.Error]++
	}

	// Update status code stats
	if stat.StatusCode > 0 {
		sc.stats.StatusCodeStats[stat.StatusCode]++
	}
}

// GetStats returns a copy of the current statistics
func (sc *StatsCollector) GetStats() WorkloadStats {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	// Calculate final statistics
	sc.calculateFinalStats()

	// Return a copy
	return *sc.stats
}

// Finalize marks the end of statistics collection
func (sc *StatsCollector) Finalize() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	sc.stats.EndTime = time.Now()
	sc.stats.TotalDuration = sc.stats.EndTime.Sub(sc.stats.StartTime)
	sc.calculateFinalStats()
}

// calculateFinalStats calculates derived statistics
func (sc *StatsCollector) calculateFinalStats() {
	if len(sc.stats.Requests) == 0 {
		return
	}

	var totalDuration time.Duration
	for _, req := range sc.stats.Requests {
		totalDuration += req.Duration
	}
	sc.stats.AverageLatency = totalDuration / time.Duration(len(sc.stats.Requests))
}

// SaveToFile saves statistics to a JSON file
func (sc *StatsCollector) SaveToFile(filename string) error {
	stats := sc.GetStats()
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	return os.WriteFile(filename, data, 0644)
}

// PrintSummary prints a summary of the statistics
func (sc *StatsCollector) PrintSummary() {
	stats := sc.GetStats()

	fmt.Printf("\nWorkload Results Summary\n")
	fmt.Printf("========================\n")
	fmt.Printf("Total duration: %v\n", stats.TotalDuration)
	fmt.Printf("Total requests: %d\n", stats.TotalRequests)
	fmt.Printf("Successful requests: %d (%.1f%%)\n", stats.SuccessfulRequests, float64(stats.SuccessfulRequests)/float64(stats.TotalRequests)*100)
	fmt.Printf("Failed requests: %d (%.1f%%)\n", stats.FailedRequests, float64(stats.FailedRequests)/float64(stats.TotalRequests)*100)
	fmt.Printf("Average latency: %.2fms\n", float64(stats.AverageLatency.Nanoseconds())/1e6)
	fmt.Printf("Min latency: %.2fms\n", float64(stats.MinLatency.Nanoseconds())/1e6)
	fmt.Printf("Max latency: %.2fms\n", float64(stats.MaxLatency.Nanoseconds())/1e6)

	fmt.Printf("\nOperation Statistics:\n")
	for op, count := range stats.OperationStats {
		fmt.Printf("  %s: %d requests\n", op, count)
	}

	if len(stats.ErrorStats) > 0 {
		fmt.Printf("\nError Statistics:\n")
		for err, count := range stats.ErrorStats {
			fmt.Printf("  %s: %d failures\n", err, count)
		}
	}

	if len(stats.StatusCodeStats) > 0 {
		fmt.Printf("\nStatus Code Statistics:\n")
		for code, count := range stats.StatusCodeStats {
			fmt.Printf("  %d: %d requests\n", code, count)
		}
	}
}
