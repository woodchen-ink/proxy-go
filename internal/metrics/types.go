package metrics

import (
	"sync/atomic"
	"time"
)

// RequestLog 记录单个请求的信息
type RequestLog struct {
	Time      time.Time
	Path      string
	Status    int
	Latency   time.Duration
	BytesSent int64
	ClientIP  string
}

// PathStats 记录路径统计信息
type PathStats struct {
	requests   atomic.Int64
	errors     atomic.Int64
	bytes      atomic.Int64
	latencySum atomic.Int64
}

// PathMetrics 用于API返回的路径统计信息
type PathMetrics struct {
	Path             string `json:"path"`
	RequestCount     int64  `json:"request_count"`
	ErrorCount       int64  `json:"error_count"`
	AvgLatency       string `json:"avg_latency"`
	BytesTransferred int64  `json:"bytes_transferred"`
}
