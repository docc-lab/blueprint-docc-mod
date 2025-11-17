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
	TotalRequests      int                     `json:"total_requests"`
	TotalWorkloads     int                     `json:"total_workloads"`
	SuccessfulRequests int                     `json:"successful_requests"`
	FailedRequests     int                     `json:"failed_requests"`
	AverageLatency     time.Duration           `json:"average_latency_ms"`
	MaxLatency         time.Duration           `json:"max_latency_ms"`
	MinLatency         time.Duration           `json:"min_latency_ms"`
	Requests           []RequestStat           `json:"requests"`
	OperationStats     map[string]int          `json:"operation_stats"`
	ErrorStats         map[string]int          `json:"error_stats"`
	StatusCodeStats    map[int]int             `json:"status_code_stats"`
	EndpointStats      map[string]EndpointStat `json:"endpoint_stats"`
	StartTime          time.Time               `json:"start_time"`
	EndTime            time.Time               `json:"end_time"`
	TotalDuration      time.Duration           `json:"total_duration_ms"`
	WorkloadsPerSecond float64                 `json:"workloads_per_second"`
	RequestsPerSecond  float64                 `json:"requests_per_second"`
}

// EndpointStat tracks statistics for a specific endpoint
type EndpointStat struct {
	Count         int           `json:"count"`
	TotalDuration time.Duration `json:"total_duration_ms"`
	AvgDuration   time.Duration `json:"avg_duration_ms"`
	MinDuration   time.Duration `json:"min_duration_ms"`
	MaxDuration   time.Duration `json:"max_duration_ms"`
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
			EndpointStats:   make(map[string]EndpointStat),
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

	// Update endpoint stats - extract root endpoint from operation
	endpoint := extractRootEndpoint(stat.Operation)
	if endpoint != "" {
		endpointStat := sc.stats.EndpointStats[endpoint]
		endpointStat.Count++
		endpointStat.TotalDuration += stat.Duration
		if endpointStat.MinDuration == 0 || stat.Duration < endpointStat.MinDuration {
			endpointStat.MinDuration = stat.Duration
		}
		if stat.Duration > endpointStat.MaxDuration {
			endpointStat.MaxDuration = stat.Duration
		}
		sc.stats.EndpointStats[endpoint] = endpointStat
	}
}

// extractRootEndpoint extracts the root endpoint from an operation name
func extractRootEndpoint(operation string) string {
	// Map operation names to root endpoints
	endpointMap := map[string]string{
		"browse_catalogue": "/Catalogue",
		"add_to_cart":      "/AddItem",
		"add_address":      "/PostAddress",
		"add_payment":      "/PostCard",
		"place_order":      "/NewOrder",
		"get_orders":       "/Orders",
		"check_orders":     "/Orders",
		"browse_by_tags":   "/Tags",
		"search_items":     "/Search",
		"get_item_details": "/Item",
		"login":            "/Login",
		"generate_session": "/GenerateSession",
	}

	if endpoint, ok := endpointMap[operation]; ok {
		return endpoint
	}
	// If not found, return empty string
	return ""
}

// RecordWorkloadStart records the start of a workload iteration
func (sc *StatsCollector) RecordWorkloadStart() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.stats.TotalWorkloads++
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

	// Calculate rates per second
	if sc.stats.TotalDuration > 0 {
		durationSeconds := sc.stats.TotalDuration.Seconds()
		sc.stats.WorkloadsPerSecond = float64(sc.stats.TotalWorkloads) / durationSeconds
		sc.stats.RequestsPerSecond = float64(sc.stats.TotalRequests) / durationSeconds
	}

	// Calculate average duration per endpoint
	for endpoint, stat := range sc.stats.EndpointStats {
		if stat.Count > 0 {
			stat.AvgDuration = stat.TotalDuration / time.Duration(stat.Count)
			sc.stats.EndpointStats[endpoint] = stat
		}
	}
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
	fmt.Printf("Total workloads: %d\n", stats.TotalWorkloads)
	fmt.Printf("Total requests: %d\n", stats.TotalRequests)
	fmt.Printf("Workloads per second: %.2f\n", stats.WorkloadsPerSecond)
	fmt.Printf("Requests per second: %.2f\n", stats.RequestsPerSecond)
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

	if len(stats.EndpointStats) > 0 {
		fmt.Printf("\nEndpoint Response Time Statistics:\n")
		// Sort endpoints for consistent output
		endpoints := make([]string, 0, len(stats.EndpointStats))
		for endpoint := range stats.EndpointStats {
			endpoints = append(endpoints, endpoint)
		}
		// Simple alphabetical sort
		for i := 0; i < len(endpoints)-1; i++ {
			for j := i + 1; j < len(endpoints); j++ {
				if endpoints[i] > endpoints[j] {
					endpoints[i], endpoints[j] = endpoints[j], endpoints[i]
				}
			}
		}
		for _, endpoint := range endpoints {
			stat := stats.EndpointStats[endpoint]
			fmt.Printf("  %s:\n", endpoint)
			fmt.Printf("    Count: %d\n", stat.Count)
			fmt.Printf("    Average: %.4fus\n", float64(stat.AvgDuration.Nanoseconds())/1e3)
			fmt.Printf("    Min: %.4fus\n", float64(stat.MinDuration.Nanoseconds())/1e3)
			fmt.Printf("    Max: %.4fus\n", float64(stat.MaxDuration.Nanoseconds())/1e3)
		}
	}
}
