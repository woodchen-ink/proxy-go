package models

import (
	"fmt"
	"sync/atomic"
	"time"
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
	// TotalLatency 纳秒累计, 不出现在接口里, 仅用于跨节点合并时按请求数重算加权平均延迟
	// (平均值本身既不能相加也不能简单取平均)
	TotalLatency     int64   `json:"-"`
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

	// Nodes 该路径在各节点上的分布, 上面的字段是这些节点的合计
	// 多节点部署时用于看清流量偏斜; 单节点部署为空数组 (契约: 不返回 null)
	Nodes []PathNodeStat `json:"nodes"`
}

// PathNodeStat 单个路径在单个节点上的计数
type PathNodeStat struct {
	NodeID       string  `json:"node_id"`
	IsCurrent    bool    `json:"is_current"` // 是否为响应本次请求的节点 (其数据为实时, 其余最多滞后一个上报周期)
	RequestCount int64   `json:"request_count"`
	ErrorCount   int64   `json:"error_count"`
	ErrorRate    float64 `json:"error_rate"`

	BytesTransferred int64 `json:"bytes_transferred"`
	CacheHits        int64 `json:"cache_hits"`
	CacheMisses      int64 `json:"cache_misses"`

	CacheHitRate float64 `json:"cache_hit_rate"`
	AvgLatency   string  `json:"avg_latency"`

	// totalLatency 纳秒累计, 仅用于重算平均延迟, 不出现在接口里
	totalLatency int64
}

// AddLatency 累加纳秒延迟, 供跨节点聚合时使用
func (p *PathNodeStat) AddLatency(ns int64) {
	p.totalLatency += ns
}

// Finalize 由累加后的原始计数重算派生值 (错误率 / 命中率 / 平均延迟)
//
// 派生值不能相加, 必须在累加完成后统一重算
func (p *PathNodeStat) Finalize() {
	if p.RequestCount > 0 {
		p.ErrorRate = float64(p.ErrorCount) / float64(p.RequestCount)
		avgMs := float64(p.totalLatency) / float64(p.RequestCount) / float64(time.Millisecond)
		p.AvgLatency = fmt.Sprintf("%.2fms", avgMs)
	} else {
		p.AvgLatency = "0.00ms"
	}
	if total := p.CacheHits + p.CacheMisses; total > 0 {
		p.CacheHitRate = float64(p.CacheHits) / float64(total)
	}
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
		TotalLatency:     p.TotalLatency.Load(),
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
