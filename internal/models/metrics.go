package models

import (
	"sync/atomic"
)

type PathStats struct {
	Requests   atomic.Int64
	Errors     atomic.Int64
	Bytes      atomic.Int64
	LatencySum atomic.Int64
}

type HistoricalMetrics struct {
	Timestamp     string  `json:"timestamp"`
	TotalRequests int64   `json:"total_requests"`
	TotalErrors   int64   `json:"total_errors"`
	TotalBytes    int64   `json:"total_bytes"`
	ErrorRate     float64 `json:"error_rate"`
	AvgLatency    float64 `json:"avg_latency"`
}
