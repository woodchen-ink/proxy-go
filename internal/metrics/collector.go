package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Collector struct {
	startTime      time.Time
	activeRequests int64
	totalRequests  int64
	totalErrors    int64
	totalBytes     atomic.Int64
	latencySum     atomic.Int64
	pathStats      sync.Map
	refererStats   sync.Map
	statusStats    [6]atomic.Int64
	latencyBuckets [10]atomic.Int64
	recentRequests struct {
		sync.RWMutex
		items  [1000]*RequestLog
		cursor atomic.Int64
	}
}

var globalCollector = &Collector{
	startTime:      time.Now(),
	pathStats:      sync.Map{},
	statusStats:    [6]atomic.Int64{},
	latencyBuckets: [10]atomic.Int64{},
}

func GetCollector() *Collector {
	return globalCollector
}

func (c *Collector) BeginRequest() {
	atomic.AddInt64(&c.activeRequests, 1)
}

func (c *Collector) EndRequest() {
	atomic.AddInt64(&c.activeRequests, -1)
}

func (c *Collector) RecordRequest(path string, status int, latency time.Duration, bytes int64, clientIP string, r *http.Request) {
	// 更新总请求数
	atomic.AddInt64(&c.totalRequests, 1)

	// 更新总字节数
	c.totalBytes.Add(bytes)

	// 更新状态码统计
	if status >= 100 && status < 600 {
		c.statusStats[status/100-1].Add(1)
	}

	// 更新错误数
	if status >= 400 {
		atomic.AddInt64(&c.totalErrors, 1)
	}

	// 更新延迟分布
	bucket := int(latency.Milliseconds() / 100)
	if bucket < 10 {
		c.latencyBuckets[bucket].Add(1)
	}

	// 更新路径统计
	if stats, ok := c.pathStats.Load(path); ok {
		pathStats := stats.(*PathStats)
		pathStats.requests.Add(1)
		if status >= 400 {
			pathStats.errors.Add(1)
		}
		pathStats.bytes.Add(bytes)
		pathStats.latencySum.Add(int64(latency))
	} else {
		newStats := &PathStats{}
		newStats.requests.Add(1)
		if status >= 400 {
			newStats.errors.Add(1)
		}
		newStats.bytes.Add(bytes)
		newStats.latencySum.Add(int64(latency))
		c.pathStats.Store(path, newStats)
	}

	// 更新引用来源统计
	if referer := r.Header.Get("Referer"); referer != "" {
		if stats, ok := c.refererStats.Load(referer); ok {
			stats.(*PathStats).requests.Add(1)
		} else {
			newStats := &PathStats{}
			newStats.requests.Add(1)
			c.refererStats.Store(referer, newStats)
		}
	}

	// 记录最近的请求
	log := &RequestLog{
		Time:      time.Now(),
		Path:      path,
		Status:    status,
		Latency:   latency,
		BytesSent: bytes,
		ClientIP:  clientIP,
	}

	c.recentRequests.Lock()
	cursor := c.recentRequests.cursor.Add(1) % 1000
	c.recentRequests.items[cursor] = log
	c.recentRequests.Unlock()

	c.latencySum.Add(int64(latency))
}

func (c *Collector) GetStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(c.startTime)
	totalRequests := atomic.LoadInt64(&c.totalRequests)
	totalErrors := atomic.LoadInt64(&c.totalErrors)

	// 获取状态码统计
	statusStats := make(map[string]int64)
	for i, v := range c.statusStats {
		statusStats[fmt.Sprintf("%dxx", i+1)] = v.Load()
	}

	// 获取Top 10路径统计
	var pathMetrics []PathMetrics
	var allPaths []PathMetrics

	c.pathStats.Range(func(key, value interface{}) bool {
		stats := value.(*PathStats)
		if stats.requests.Load() == 0 {
			return true
		}
		allPaths = append(allPaths, PathMetrics{
			Path:             key.(string),
			RequestCount:     stats.requests.Load(),
			ErrorCount:       stats.errors.Load(),
			AvgLatency:       FormatDuration(time.Duration(stats.latencySum.Load() / stats.requests.Load())),
			BytesTransferred: stats.bytes.Load(),
		})
		return true
	})

	// 按请求数排序
	sort.Slice(allPaths, func(i, j int) bool {
		return allPaths[i].RequestCount > allPaths[j].RequestCount
	})

	// 取前10个
	if len(allPaths) > 10 {
		pathMetrics = allPaths[:10]
	} else {
		pathMetrics = allPaths
	}

	// 获取Top 10引用来源
	var refererMetrics []PathMetrics
	var allReferers []PathMetrics
	c.refererStats.Range(func(key, value interface{}) bool {
		stats := value.(*PathStats)
		if stats.requests.Load() == 0 {
			return true
		}
		allReferers = append(allReferers, PathMetrics{
			Path:         key.(string),
			RequestCount: stats.requests.Load(),
		})
		return true
	})

	// 按请求数排序
	sort.Slice(allReferers, func(i, j int) bool {
		return allReferers[i].RequestCount > allReferers[j].RequestCount
	})

	// 取前10个
	if len(allReferers) > 10 {
		refererMetrics = allReferers[:10]
	} else {
		refererMetrics = allReferers
	}

	return map[string]interface{}{
		"uptime":           uptime.String(),
		"active_requests":  atomic.LoadInt64(&c.activeRequests),
		"total_requests":   totalRequests,
		"total_errors":     totalErrors,
		"error_rate":       float64(totalErrors) / float64(totalRequests),
		"num_goroutine":    runtime.NumGoroutine(),
		"memory_usage":     FormatBytes(m.Alloc),
		"total_bytes":      c.totalBytes.Load(),
		"bytes_per_second": float64(c.totalBytes.Load()) / Max(uptime.Seconds(), 1),
		"avg_latency": func() int64 {
			if totalRequests > 0 {
				return int64(c.latencySum.Load() / totalRequests)
			}
			return 0
		}(),
		"status_code_stats": statusStats,
		"top_paths":         pathMetrics,
		"recent_requests":   c.getRecentRequests(),
		"top_referers":      refererMetrics,
	}
}

func (c *Collector) getRecentRequests() []RequestLog {
	var recentReqs []RequestLog
	c.recentRequests.RLock()
	defer c.recentRequests.RUnlock()

	cursor := c.recentRequests.cursor.Load()
	for i := 0; i < 10; i++ {
		idx := (cursor - int64(i) + 1000) % 1000
		if c.recentRequests.items[idx] != nil {
			recentReqs = append(recentReqs, *c.recentRequests.items[idx])
		}
	}
	return recentReqs
}

// 辅助函数
func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2f μs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.2f ms", float64(d.Milliseconds()))
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

func FormatBytes(bytes uint64) string {
	const (
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}

func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
