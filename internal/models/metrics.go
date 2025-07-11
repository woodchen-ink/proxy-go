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
	LastAccessTime   atomic.Int64 `json:"last_access_time"` // 最后访问时间戳
}

// PathMetricsJSON 用于 JSON 序列化的路径统计信息
type PathMetricsJSON struct {
	Path             string `json:"path"`
	RequestCount     int64  `json:"request_count"`
	ErrorCount       int64  `json:"error_count"`
	BytesTransferred int64  `json:"bytes_transferred"`
	AvgLatency       string `json:"avg_latency"`
	LastAccessTime   int64  `json:"last_access_time"` // 最后访问时间戳
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

// ToJSON 转换为 JSON 友好的结构
func (p *PathMetrics) ToJSON() PathMetricsJSON {
	return PathMetricsJSON{
		Path:             p.Path,
		RequestCount:     p.RequestCount.Load(),
		ErrorCount:       p.ErrorCount.Load(),
		BytesTransferred: p.BytesTransferred.Load(),
		AvgLatency:       p.AvgLatency,
	}
}

type HistoricalMetrics struct {
	Timestamp     string  `json:"timestamp"`
	TotalRequests int64   `json:"total_requests"`
	TotalErrors   int64   `json:"total_errors"`
	TotalBytes    int64   `json:"total_bytes"`
	ErrorRate     float64 `json:"error_rate"`
	AvgLatency    float64 `json:"avg_latency"`
}
