package metrics

import (
	"fmt"
	"log"
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/models"
	"proxy-go/internal/utils"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Collector 指标收集器
type Collector struct {
	startTime       time.Time
	activeRequests  int64
	totalRequests   int64
	totalErrors     int64
	totalBytes      int64
	latencySum      int64
	pathStats       sync.Map
	statusCodeStats sync.Map
	recentRequests  *models.RequestQueue
	config          *config.Config
}

var (
	instance *Collector
	once     sync.Once
)

// InitCollector 初始化收集器
func InitCollector(cfg *config.Config) error {
	once.Do(func() {
		instance = &Collector{
			startTime:      time.Now(),
			recentRequests: models.NewRequestQueue(1000),
			config:         cfg,
		}
	})
	return nil
}

// GetCollector 获取收集器实例
func GetCollector() *Collector {
	return instance
}

// BeginRequest 开始请求
func (c *Collector) BeginRequest() {
	atomic.AddInt64(&c.activeRequests, 1)
}

// EndRequest 结束请求
func (c *Collector) EndRequest() {
	atomic.AddInt64(&c.activeRequests, -1)
}

// RecordRequest 记录请求
func (c *Collector) RecordRequest(path string, status int, latency time.Duration, bytes int64, clientIP string, r *http.Request) {
	atomic.AddInt64(&c.totalRequests, 1)
	atomic.AddInt64(&c.totalBytes, bytes)
	atomic.AddInt64(&c.latencySum, int64(latency))

	if status >= 400 {
		atomic.AddInt64(&c.totalErrors, 1)
	}

	// 更新状态码统计
	statusKey := fmt.Sprintf("%d", status)
	if value, ok := c.statusCodeStats.Load(statusKey); ok {
		atomic.AddInt64(value.(*int64), 1)
	} else {
		var count int64 = 1
		c.statusCodeStats.Store(statusKey, &count)
	}

	// 更新路径统计
	if pathStats, ok := c.pathStats.Load(path); ok {
		stats := pathStats.(models.PathMetrics)
		atomic.AddInt64(&stats.RequestCount, 1)
		if status >= 400 {
			atomic.AddInt64(&stats.ErrorCount, 1)
		}
		atomic.AddInt64(&stats.TotalLatency, int64(latency))
		atomic.AddInt64(&stats.BytesTransferred, bytes)
	} else {
		stats := models.PathMetrics{
			Path:             path,
			RequestCount:     1,
			ErrorCount:       int64(map[bool]int{true: 1, false: 0}[status >= 400]),
			TotalLatency:     int64(latency),
			BytesTransferred: bytes,
		}
		c.pathStats.Store(path, stats)
	}

	// 记录最近请求
	c.recentRequests.Push(models.RequestLog{
		Time:      time.Now(),
		Path:      path,
		Status:    status,
		Latency:   int64(latency),
		BytesSent: bytes,
		ClientIP:  clientIP,
	})
}

// GetStats 获取统计数据
func (c *Collector) GetStats() map[string]interface{} {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// 计算平均延迟
	avgLatency := float64(0)
	if c.totalRequests > 0 {
		avgLatency = float64(c.latencySum) / float64(c.totalRequests)
	}

	// 收集状态码统计
	statusCodeStats := make(map[string]int64)
	c.statusCodeStats.Range(func(key, value interface{}) bool {
		statusCodeStats[key.(string)] = atomic.LoadInt64(value.(*int64))
		return true
	})

	// 收集路径统计
	var pathMetrics []models.PathMetrics
	c.pathStats.Range(func(key, value interface{}) bool {
		stats := value.(models.PathMetrics)
		pathMetrics = append(pathMetrics, stats)
		return true
	})

	// 按请求数排序
	sort.Slice(pathMetrics, func(i, j int) bool {
		return pathMetrics[i].RequestCount > pathMetrics[j].RequestCount
	})

	// 只保留前10个
	if len(pathMetrics) > 10 {
		pathMetrics = pathMetrics[:10]
	}

	// 计算每个路径的平均延迟
	for i := range pathMetrics {
		if pathMetrics[i].RequestCount > 0 {
			avgLatencyMs := float64(pathMetrics[i].TotalLatency) / float64(pathMetrics[i].RequestCount) / float64(time.Millisecond)
			pathMetrics[i].AvgLatency = fmt.Sprintf("%.2fms", avgLatencyMs)
		}
	}

	return map[string]interface{}{
		"uptime":            time.Since(c.startTime).String(),
		"active_requests":   atomic.LoadInt64(&c.activeRequests),
		"total_requests":    atomic.LoadInt64(&c.totalRequests),
		"total_errors":      atomic.LoadInt64(&c.totalErrors),
		"total_bytes":       atomic.LoadInt64(&c.totalBytes),
		"num_goroutine":     runtime.NumGoroutine(),
		"memory_usage":      utils.FormatBytes(int64(mem.Alloc)),
		"avg_response_time": fmt.Sprintf("%.2fms", avgLatency/float64(time.Millisecond)),
		"status_code_stats": statusCodeStats,
		"top_paths":         pathMetrics,
		"recent_requests":   c.recentRequests.GetAll(),
	}
}

func (c *Collector) SaveMetrics(stats map[string]interface{}) error {
	lastSaveTime = time.Now()
	return nil
}

// LoadRecentStats 简化为只进行数据验证
func (c *Collector) LoadRecentStats() error {
	start := time.Now()
	log.Printf("Starting to validate stats...")

	if err := c.validateLoadedData(); err != nil {
		return fmt.Errorf("data validation failed: %v", err)
	}

	log.Printf("Successfully validated stats in %v", time.Since(start))
	return nil
}

// validateLoadedData 验证当前数据的有效性
func (c *Collector) validateLoadedData() error {
	// 验证基础指标
	if atomic.LoadInt64(&c.totalRequests) < 0 ||
		atomic.LoadInt64(&c.totalErrors) < 0 ||
		atomic.LoadInt64(&c.totalBytes) < 0 {
		return fmt.Errorf("invalid stats values")
	}

	// 验证错误数不能大于总请求数
	if atomic.LoadInt64(&c.totalErrors) > atomic.LoadInt64(&c.totalRequests) {
		return fmt.Errorf("total errors exceeds total requests")
	}

	// 验证状态码统计
	c.statusCodeStats.Range(func(key, value interface{}) bool {
		return atomic.LoadInt64(value.(*int64)) >= 0
	})

	// 验证路径统计
	var totalPathRequests int64
	c.pathStats.Range(func(_, value interface{}) bool {
		stats := value.(models.PathMetrics)
		if stats.RequestCount < 0 || stats.ErrorCount < 0 {
			return false
		}
		totalPathRequests += stats.RequestCount
		return true
	})

	// 验证总数一致性
	if totalPathRequests > atomic.LoadInt64(&c.totalRequests) {
		return fmt.Errorf("path stats total exceeds total requests")
	}

	return nil
}

// GetLastSaveTime 实现 interfaces.MetricsCollector 接口
var lastSaveTime time.Time

func (c *Collector) GetLastSaveTime() time.Time {
	return lastSaveTime
}

// CheckDataConsistency 实现 interfaces.MetricsCollector 接口
func (c *Collector) CheckDataConsistency() error {
	// 简单的数据验证
	if atomic.LoadInt64(&c.totalErrors) > atomic.LoadInt64(&c.totalRequests) {
		return fmt.Errorf("total errors exceeds total requests")
	}
	return nil
}
