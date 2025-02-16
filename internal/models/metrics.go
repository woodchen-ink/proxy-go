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

// PathMetrics 路径统计信息
type PathMetrics struct {
	Path             string       `json:"path"`
	RequestCount     atomic.Int64 `json:"request_count"`
	ErrorCount       atomic.Int64 `json:"error_count"`
	TotalLatency     atomic.Int64 `json:"-"`
	BytesTransferred atomic.Int64 `json:"bytes_transferred"`
	AvgLatency       string       `json:"avg_latency"`
}

// GetRequestCount 获取请求数
func (p *PathMetrics) GetRequestCount() int64 {
	return p.RequestCount.Load()
}

// GetErrorCount 获取错误数
func (p *PathMetrics) GetErrorCount() int64 {
	return p.ErrorCount.Load()
}

// GetTotalLatency 获取总延迟
func (p *PathMetrics) GetTotalLatency() int64 {
	return p.TotalLatency.Load()
}

// GetBytesTransferred 获取传输字节数
func (p *PathMetrics) GetBytesTransferred() int64 {
	return p.BytesTransferred.Load()
}

// AddRequest 增加请求数
func (p *PathMetrics) AddRequest() {
	p.RequestCount.Add(1)
}

// AddError 增加错误数
func (p *PathMetrics) AddError() {
	p.ErrorCount.Add(1)
}

// AddLatency 增加延迟
func (p *PathMetrics) AddLatency(latency int64) {
	p.TotalLatency.Add(latency)
}

// AddBytes 增加传输字节数
func (p *PathMetrics) AddBytes(bytes int64) {
	p.BytesTransferred.Add(bytes)
}

type HistoricalMetrics struct {
	Timestamp     string  `json:"timestamp"`
	TotalRequests int64   `json:"total_requests"`
	TotalErrors   int64   `json:"total_errors"`
	TotalBytes    int64   `json:"total_bytes"`
	ErrorRate     float64 `json:"error_rate"`
	AvgLatency    float64 `json:"avg_latency"`
}
