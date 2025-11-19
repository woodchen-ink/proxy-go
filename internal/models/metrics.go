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

	// 状态码统计
	Status2xx        atomic.Int64 `json:"status_2xx"` // 2xx 成功
	Status3xx        atomic.Int64 `json:"status_3xx"` // 3xx 重定向
	Status4xx        atomic.Int64 `json:"status_4xx"` // 4xx 客户端错误
	Status5xx        atomic.Int64 `json:"status_5xx"` // 5xx 服务器错误

	// 缓存统计
	CacheHits        atomic.Int64 `json:"cache_hits"`   // 缓存命中
	CacheMisses      atomic.Int64 `json:"cache_misses"` // 缓存未命中
	BytesSaved       atomic.Int64 `json:"bytes_saved"`  // 通过缓存节省的字节数
}

// PathMetricsJSON 用于 JSON 序列化的路径统计信息
type PathMetricsJSON struct {
	Path             string  `json:"path"`
	RequestCount     int64   `json:"request_count"`
	ErrorCount       int64   `json:"error_count"`
	BytesTransferred int64   `json:"bytes_transferred"`
	AvgLatency       string  `json:"avg_latency"`
	LastAccessTime   int64   `json:"last_access_time"` // 最后访问时间戳

	// 状态码统计
	Status2xx        int64   `json:"status_2xx"`
	Status3xx        int64   `json:"status_3xx"`
	Status4xx        int64   `json:"status_4xx"`
	Status5xx        int64   `json:"status_5xx"`

	// 缓存统计
	CacheHits        int64   `json:"cache_hits"`
	CacheMisses      int64   `json:"cache_misses"`
	CacheHitRate     float64 `json:"cache_hit_rate"` // 缓存命中率
	BytesSaved       int64   `json:"bytes_saved"`
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
	cacheHits := p.CacheHits.Load()
	cacheMisses := p.CacheMisses.Load()
	totalCache := cacheHits + cacheMisses
	var cacheHitRate float64
	if totalCache > 0 {
		cacheHitRate = float64(cacheHits) / float64(totalCache)
	}

	return PathMetricsJSON{
		Path:             p.Path,
		RequestCount:     p.RequestCount.Load(),
		ErrorCount:       p.ErrorCount.Load(),
		BytesTransferred: p.BytesTransferred.Load(),
		AvgLatency:       p.AvgLatency,
		LastAccessTime:   p.LastAccessTime.Load(),
		Status2xx:        p.Status2xx.Load(),
		Status3xx:        p.Status3xx.Load(),
		Status4xx:        p.Status4xx.Load(),
		Status5xx:        p.Status5xx.Load(),
		CacheHits:        cacheHits,
		CacheMisses:      cacheMisses,
		CacheHitRate:     cacheHitRate,
		BytesSaved:       p.BytesSaved.Load(),
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
