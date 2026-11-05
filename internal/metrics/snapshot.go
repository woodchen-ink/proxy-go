package metrics

import (
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"proxy-go/internal/models"
)

// RawPathStat 逐路径原始累计, 含 total_latency_ns (派生值由中控按合并后的原值重算)
type RawPathStat struct {
	Path             string `json:"path"`
	RequestCount     int64  `json:"request_count"`
	ErrorCount       int64  `json:"error_count"`
	BytesTransferred int64  `json:"bytes_transferred"`
	TotalLatencyNs   int64  `json:"total_latency_ns"`
	LastAccessTime   int64  `json:"last_access_time"`
	Status2xx        int64  `json:"status_2xx"`
	Status3xx        int64  `json:"status_3xx"`
	Status4xx        int64  `json:"status_4xx"`
	Status5xx        int64  `json:"status_5xx"`
	CacheHits        int64  `json:"cache_hits"`
	CacheMisses      int64  `json:"cache_misses"`
	BytesSaved       int64  `json:"bytes_saved"`
}

// LiveStats 推给中控的本节点实时数据, 数值一律是原始量 (不格式化), 由中控跨节点求和后再格式化
type LiveStats struct {
	UptimeSec           int64                    `json:"uptime_sec"`
	ActiveRequests      int64                    `json:"active_requests"`
	RequestsPerSecond   float64                  `json:"requests_per_second"`
	CurrentBandwidthBps float64                  `json:"current_bandwidth_bps"`
	BandwidthHistory    map[string]int64         `json:"bandwidth_history"`
	NumGoroutine        int                      `json:"num_goroutine"`
	MemoryBytes         uint64                   `json:"memory_bytes"`
	LatencySumNs        int64                    `json:"latency_sum_ns"`
	MinLatencyNs        int64                    `json:"min_latency_ns"`
	MaxLatencyNs        int64                    `json:"max_latency_ns"`
	TotalBytes          int64                    `json:"total_bytes"`
	SessionRequests     int64                    `json:"session_requests"`
	StatusCodes         map[string]int64         `json:"status_codes"`
	LatencyDistribution map[string]int64         `json:"latency_distribution"`
	PathStats           []RawPathStat            `json:"path_stats"`
	TopReferers         []models.PathMetricsJSON `json:"top_referers"`
	RecentRequests      []models.RequestLog      `json:"recent_requests"`
}

// LiveSnapshot 汇总本节点实时数据 (每 15s 由 control 包推一次)
func (c *Collector) LiveSnapshot() LiveStats {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	minLatency := atomic.LoadInt64(&c.minLatency)
	if minLatency == math.MaxInt64 {
		minLatency = 0
	}

	c.bandwidthStats.RLock()
	history := make(map[string]int64, len(c.bandwidthStats.history))
	for k, v := range c.bandwidthStats.history {
		history[k] = v
	}
	c.bandwidthStats.RUnlock()

	pathStats := make([]RawPathStat, 0, 32)
	c.pathStats.Range(func(key string, s *models.PathMetrics) bool {
		if s.GetRequestCount() == 0 {
			return true
		}
		pathStats = append(pathStats, RawPathStat{
			Path:             s.Path,
			RequestCount:     s.RequestCount.Load(),
			ErrorCount:       s.ErrorCount.Load(),
			BytesTransferred: s.BytesTransferred.Load(),
			TotalLatencyNs:   s.TotalLatency.Load(),
			LastAccessTime:   s.LastAccessTime.Load(),
			Status2xx:        s.Status2xx.Load(),
			Status3xx:        s.Status3xx.Load(),
			Status4xx:        s.Status4xx.Load(),
			Status5xx:        s.Status5xx.Load(),
			CacheHits:        s.CacheHits.Load(),
			CacheMisses:      s.CacheMisses.Load(),
			BytesSaved:       s.BytesSaved.Load(),
		})
		return true
	})

	stats := c.GetStats()
	topReferers, _ := stats["top_referers"].([]models.PathMetricsJSON)
	recent := c.recentRequests.GetAll()
	if recent == nil {
		recent = []models.RequestLog{}
	}

	return LiveStats{
		UptimeSec:           int64(time.Since(c.startTime).Seconds()),
		ActiveRequests:      atomic.LoadInt64(&c.activeRequests),
		RequestsPerSecond:   c.getRecentRequestsPerSecond(),
		CurrentBandwidthBps: c.getCurrentBandwidth(),
		BandwidthHistory:    history,
		NumGoroutine:        runtime.NumGoroutine(),
		MemoryBytes:         mem.Alloc,
		LatencySumNs:        atomic.LoadInt64(&c.latencySum),
		MinLatencyNs:        minLatency,
		MaxLatencyNs:        atomic.LoadInt64(&c.maxLatency),
		TotalBytes:          atomic.LoadInt64(&c.totalBytes),
		SessionRequests:     atomic.LoadInt64(&c.sessionRequests),
		StatusCodes:         c.statusCodeStats.GetStats(),
		LatencyDistribution: map[string]int64{
			"lt10ms":     atomic.LoadInt64(&c.latencyBuckets.lt10ms),
			"10-50ms":    atomic.LoadInt64(&c.latencyBuckets.ms10_50),
			"50-200ms":   atomic.LoadInt64(&c.latencyBuckets.ms50_200),
			"200-1000ms": atomic.LoadInt64(&c.latencyBuckets.ms200_1000),
			"gt1s":       atomic.LoadInt64(&c.latencyBuckets.gt1s),
		},
		PathStats:      pathStats,
		TopReferers:    topReferers,
		RecentRequests: recent,
	}
}
