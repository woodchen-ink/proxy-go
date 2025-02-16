package metrics

import (
	"fmt"
	"log"
	"math"
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
	startTime           time.Time
	activeRequests      int64
	totalRequests       int64
	totalErrors         int64
	totalBytes          int64
	latencySum          int64
	maxLatency          int64 // 最大响应时间
	minLatency          int64 // 最小响应时间
	clientErrors        int64 // 4xx错误
	serverErrors        int64 // 5xx错误
	pathStats           sync.Map
	statusCodeStats     sync.Map
	latencyBuckets      sync.Map // 响应时间分布
	bandwidthStats      sync.Map // 带宽统计
	errorTypes          sync.Map // 错误类型统计
	recentRequests      []models.RequestLog
	recentRequestsMutex sync.RWMutex
	pathStatsMutex      sync.RWMutex
	config              *config.Config
	lastMinute          time.Time // 用于计算每分钟带宽
	minuteBytes         int64     // 当前分钟的字节数
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
			lastMinute:     time.Now(),
			recentRequests: make([]models.RequestLog, 0, 1000),
			config:         cfg,
			minLatency:     math.MaxInt64, // 初始化为最大值
		}

		// 初始化延迟分布桶
		instance.latencyBuckets.Store("<10ms", new(int64))
		instance.latencyBuckets.Store("10-50ms", new(int64))
		instance.latencyBuckets.Store("50-200ms", new(int64))
		instance.latencyBuckets.Store("200-1000ms", new(int64))
		instance.latencyBuckets.Store(">1s", new(int64))
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
	// 批量更新基础指标
	atomic.AddInt64(&c.totalRequests, 1)
	atomic.AddInt64(&c.totalBytes, bytes)
	atomic.AddInt64(&c.latencySum, int64(latency))

	// 更新最小和最大响应时间
	latencyNanos := int64(latency)
	for {
		oldMin := atomic.LoadInt64(&c.minLatency)
		if oldMin <= latencyNanos {
			break
		}
		if atomic.CompareAndSwapInt64(&c.minLatency, oldMin, latencyNanos) {
			break
		}
	}
	for {
		oldMax := atomic.LoadInt64(&c.maxLatency)
		if oldMax >= latencyNanos {
			break
		}
		if atomic.CompareAndSwapInt64(&c.maxLatency, oldMax, latencyNanos) {
			break
		}
	}

	// 更新延迟分布
	latencyMs := latency.Milliseconds()
	var bucketKey string
	switch {
	case latencyMs < 10:
		bucketKey = "<10ms"
	case latencyMs < 50:
		bucketKey = "10-50ms"
	case latencyMs < 200:
		bucketKey = "50-200ms"
	case latencyMs < 1000:
		bucketKey = "200-1000ms"
	default:
		bucketKey = ">1s"
	}
	if counter, ok := c.latencyBuckets.Load(bucketKey); ok {
		atomic.AddInt64(counter.(*int64), 1)
	} else {
		var count int64 = 1
		c.latencyBuckets.Store(bucketKey, &count)
	}

	// 更新错误统计
	if status >= 400 {
		atomic.AddInt64(&c.totalErrors, 1)
		if status >= 500 {
			atomic.AddInt64(&c.serverErrors, 1)
		} else {
			atomic.AddInt64(&c.clientErrors, 1)
		}
		errKey := fmt.Sprintf("%d %s", status, http.StatusText(status))
		if counter, ok := c.errorTypes.Load(errKey); ok {
			atomic.AddInt64(counter.(*int64), 1)
		} else {
			var count int64 = 1
			c.errorTypes.Store(errKey, &count)
		}
	}

	// 更新状态码统计
	statusKey := fmt.Sprintf("%d", status)
	if counter, ok := c.statusCodeStats.Load(statusKey); ok {
		atomic.AddInt64(counter.(*int64), 1)
	} else {
		var count int64 = 1
		c.statusCodeStats.Store(statusKey, &count)
	}

	// 更新路径统计
	c.pathStatsMutex.Lock()
	if value, ok := c.pathStats.Load(path); ok {
		stat := value.(*models.PathMetrics)
		atomic.AddInt64(&stat.RequestCount, 1)
		if status >= 400 {
			atomic.AddInt64(&stat.ErrorCount, 1)
		}
		atomic.AddInt64(&stat.TotalLatency, int64(latency))
		atomic.AddInt64(&stat.BytesTransferred, bytes)
	} else {
		c.pathStats.Store(path, &models.PathMetrics{
			Path:             path,
			RequestCount:     1,
			ErrorCount:       map[bool]int64{true: 1, false: 0}[status >= 400],
			TotalLatency:     int64(latency),
			BytesTransferred: bytes,
		})
	}
	c.pathStatsMutex.Unlock()

	// 更新最近请求记录
	c.recentRequestsMutex.Lock()
	c.recentRequests = append([]models.RequestLog{{
		Time:      time.Now(),
		Path:      path,
		Status:    status,
		Latency:   int64(latency),
		BytesSent: bytes,
		ClientIP:  clientIP,
	}}, c.recentRequests...)
	if len(c.recentRequests) > 100 { // 只保留最近100条记录
		c.recentRequests = c.recentRequests[:100]
	}
	c.recentRequestsMutex.Unlock()
}

// FormatUptime 格式化运行时间
func FormatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天%d小时%d分钟%d秒", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟%d秒", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d分钟%d秒", minutes, seconds)
	}
	return fmt.Sprintf("%d秒", seconds)
}

// GetStats 获取统计数据
func (c *Collector) GetStats() map[string]interface{} {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	// 计算平均延迟
	avgLatency := float64(0)
	totalReqs := atomic.LoadInt64(&c.totalRequests)
	if totalReqs > 0 {
		avgLatency = float64(atomic.LoadInt64(&c.latencySum)) / float64(totalReqs)
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
		stats := value.(*models.PathMetrics)
		if stats.RequestCount > 0 {
			avgLatencyMs := float64(stats.TotalLatency) / float64(stats.RequestCount) / float64(time.Millisecond)
			stats.AvgLatency = fmt.Sprintf("%.2fms", avgLatencyMs)
		}
		pathMetrics = append(pathMetrics, *stats)
		return true
	})

	// 按请求数降序排序
	sort.Slice(pathMetrics, func(i, j int) bool {
		return pathMetrics[i].RequestCount > pathMetrics[j].RequestCount
	})

	// 只保留前10个
	if len(pathMetrics) > 10 {
		pathMetrics = pathMetrics[:10]
	}

	// 收集延迟分布
	latencyDistribution := make(map[string]int64)
	c.latencyBuckets.Range(func(key, value interface{}) bool {
		latencyDistribution[key.(string)] = atomic.LoadInt64(value.(*int64))
		return true
	})

	// 收集错误类型统计
	errorTypeStats := make(map[string]int64)
	c.errorTypes.Range(func(key, value interface{}) bool {
		errorTypeStats[key.(string)] = atomic.LoadInt64(value.(*int64))
		return true
	})

	// 收集最近5分钟的带宽统计
	bandwidthHistory := make(map[string]string)
	var times []string
	c.bandwidthStats.Range(func(key, value interface{}) bool {
		times = append(times, key.(string))
		return true
	})
	sort.Strings(times)
	if len(times) > 5 {
		times = times[len(times)-5:]
	}
	for _, t := range times {
		if bytes, ok := c.bandwidthStats.Load(t); ok {
			bandwidthHistory[t] = utils.FormatBytes(atomic.LoadInt64(bytes.(*int64))) + "/min"
		}
	}

	// 获取最小和最大响应时间
	minLatency := atomic.LoadInt64(&c.minLatency)
	maxLatency := atomic.LoadInt64(&c.maxLatency)
	if minLatency == math.MaxInt64 {
		minLatency = 0
	}

	return map[string]interface{}{
		"uptime":            FormatUptime(time.Since(c.startTime)),
		"active_requests":   atomic.LoadInt64(&c.activeRequests),
		"total_requests":    atomic.LoadInt64(&c.totalRequests),
		"total_errors":      atomic.LoadInt64(&c.totalErrors),
		"total_bytes":       atomic.LoadInt64(&c.totalBytes),
		"num_goroutine":     runtime.NumGoroutine(),
		"memory_usage":      utils.FormatBytes(int64(mem.Alloc)),
		"avg_response_time": fmt.Sprintf("%.2fms", avgLatency/float64(time.Millisecond)),
		"status_code_stats": statusCodeStats,
		"top_paths":         pathMetrics,
		"recent_requests":   c.recentRequests,
		"latency_stats": map[string]interface{}{
			"min":          fmt.Sprintf("%.2fms", float64(minLatency)/float64(time.Millisecond)),
			"max":          fmt.Sprintf("%.2fms", float64(maxLatency)/float64(time.Millisecond)),
			"distribution": latencyDistribution,
		},
		"error_stats": map[string]interface{}{
			"client_errors": atomic.LoadInt64(&c.clientErrors),
			"server_errors": atomic.LoadInt64(&c.serverErrors),
			"types":         errorTypeStats,
		},
		"bandwidth_history": bandwidthHistory,
		"current_bandwidth": utils.FormatBytes(atomic.LoadInt64(&c.minuteBytes)) + "/min",
	}
}

func (c *Collector) SaveMetrics(stats map[string]interface{}) error {
	lastSaveTime = time.Now()
	return nil
}

// LoadRecentStats 简化为只进行数据验证
func (c *Collector) LoadRecentStats() error {
	start := time.Now()
	log.Printf("[Metrics] Loading stats...")

	if err := c.validateLoadedData(); err != nil {
		return fmt.Errorf("data validation failed: %v", err)
	}

	log.Printf("[Metrics] Loaded stats in %v", time.Since(start))
	return nil
}

// validateLoadedData 验证当前数据的有效性
func (c *Collector) validateLoadedData() error {
	// 验证基础指标
	if c.totalRequests < 0 ||
		c.totalErrors < 0 ||
		c.totalBytes < 0 {
		return fmt.Errorf("invalid stats values")
	}

	// 验证错误数不能大于总请求数
	if c.totalErrors > c.totalRequests {
		return fmt.Errorf("total errors exceeds total requests")
	}

	// 验证状态码统计
	c.statusCodeStats.Range(func(key, value interface{}) bool {
		return value.(int64) >= 0
	})

	// 验证路径统计
	var totalPathRequests int64
	c.pathStats.Range(func(_, value interface{}) bool {
		stats := value.(*models.PathMetrics)
		if stats.RequestCount < 0 || stats.ErrorCount < 0 {
			return false
		}
		totalPathRequests += stats.RequestCount
		return true
	})

	// 验证总数一致性
	if totalPathRequests > c.totalRequests {
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
	if c.totalErrors > c.totalRequests {
		return fmt.Errorf("total errors exceeds total requests")
	}
	return nil
}
