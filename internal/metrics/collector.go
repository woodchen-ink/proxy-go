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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Collector 指标收集器
type Collector struct {
	startTime       time.Time
	activeRequests  int64
	totalBytes      int64
	latencySum      int64
	maxLatency      int64 // 最大响应时间
	minLatency      int64 // 最小响应时间
	statusCodeStats sync.Map
	latencyBuckets  sync.Map // 响应时间分布
	refererStats    sync.Map // 引用来源统计
	bandwidthStats  struct {
		sync.RWMutex
		window     time.Duration
		lastUpdate time.Time
		current    int64
		history    map[string]int64
	}
	recentRequests *models.RequestQueue
	config         *config.Config
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
			recentRequests: models.NewRequestQueue(100),
			config:         cfg,
			minLatency:     math.MaxInt64,
		}

		// 初始化带宽统计
		instance.bandwidthStats.window = time.Minute
		instance.bandwidthStats.lastUpdate = time.Now()
		instance.bandwidthStats.history = make(map[string]int64)

		// 初始化延迟分布桶
		buckets := []string{"lt10ms", "10-50ms", "50-200ms", "200-1000ms", "gt1s"}
		for _, bucket := range buckets {
			counter := new(int64)
			*counter = 0
			instance.latencyBuckets.Store(bucket, counter)
		}

		// 启动数据一致性检查器
		instance.startConsistencyChecker()

		// 启动定期清理任务
		instance.startCleanupTask()
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
	// 更新状态码统计
	statusKey := fmt.Sprintf("%d", status)
	if counter, ok := c.statusCodeStats.Load(statusKey); ok {
		atomic.AddInt64(counter.(*int64), 1)
	} else {
		counter := new(int64)
		*counter = 1
		c.statusCodeStats.Store(statusKey, counter)
	}

	// 更新总字节数和带宽统计
	atomic.AddInt64(&c.totalBytes, bytes)
	c.updateBandwidthStats(bytes)

	// 更新延迟统计
	atomic.AddInt64(&c.latencySum, int64(latency))
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
		bucketKey = "lt10ms"
	case latencyMs < 50:
		bucketKey = "10-50ms"
	case latencyMs < 200:
		bucketKey = "50-200ms"
	case latencyMs < 1000:
		bucketKey = "200-1000ms"
	default:
		bucketKey = "gt1s"
	}

	if counter, ok := c.latencyBuckets.Load(bucketKey); ok {
		atomic.AddInt64(counter.(*int64), 1)
	} else {
		counter := new(int64)
		*counter = 1
		c.latencyBuckets.Store(bucketKey, counter)
	}

	// 更新引用来源统计
	if r != nil {
		referer := r.Header.Get("Referer")
		if referer != "" {
			// 简化引用来源，只保留域名部分
			referer = simplifyReferer(referer)

			if value, ok := c.refererStats.Load(referer); ok {
				stat := value.(*models.PathMetrics)
				stat.AddRequest()
				if status >= 400 {
					stat.AddError()
				}
				stat.AddLatency(int64(latency))
				stat.AddBytes(bytes)
			} else {
				newStat := &models.PathMetrics{
					Path: referer,
				}
				newStat.RequestCount.Store(1)
				if status >= 400 {
					newStat.ErrorCount.Store(1)
				}
				newStat.TotalLatency.Store(int64(latency))
				newStat.BytesTransferred.Store(bytes)
				c.refererStats.Store(referer, newStat)
			}
		}
	}

	// 更新最近请求记录
	c.recentRequests.Push(models.RequestLog{
		Time:      time.Now(),
		Path:      path,
		Status:    status,
		Latency:   int64(latency),
		BytesSent: bytes,
		ClientIP:  clientIP,
	})
}

// FormatUptime 格式化运行时间
func FormatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天%d时%d分%d秒", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%d时%d分%d秒", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d分%d秒", minutes, seconds)
	}
	return fmt.Sprintf("%d秒", seconds)
}

// GetStats 获取统计数据
func (c *Collector) GetStats() map[string]interface{} {
	// 获取统计数据
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	now := time.Now()
	totalRuntime := now.Sub(c.startTime)

	// 计算总请求数和平均延迟
	var totalRequests int64
	c.statusCodeStats.Range(func(key, value interface{}) bool {
		if counter, ok := value.(*int64); ok {
			totalRequests += atomic.LoadInt64(counter)
		} else {
			totalRequests += value.(int64)
		}
		return true
	})

	avgLatency := float64(0)
	if totalRequests > 0 {
		avgLatency = float64(atomic.LoadInt64(&c.latencySum)) / float64(totalRequests)
	}

	// 计算总体平均每秒请求数
	requestsPerSecond := float64(totalRequests) / totalRuntime.Seconds()

	// 收集状态码统计
	statusCodeStats := make(map[string]int64)
	c.statusCodeStats.Range(func(key, value interface{}) bool {
		if counter, ok := value.(*int64); ok {
			statusCodeStats[key.(string)] = atomic.LoadInt64(counter)
		} else {
			statusCodeStats[key.(string)] = value.(int64)
		}
		return true
	})

	// 收集引用来源统计
	var refererMetrics []*models.PathMetrics
	refererCount := 0
	c.refererStats.Range(func(key, value interface{}) bool {
		stats := value.(*models.PathMetrics)
		requestCount := stats.GetRequestCount()
		if requestCount > 0 {
			totalLatency := stats.GetTotalLatency()
			avgLatencyMs := float64(totalLatency) / float64(requestCount) / float64(time.Millisecond)
			stats.AvgLatency = fmt.Sprintf("%.2fms", avgLatencyMs)
			refererMetrics = append(refererMetrics, stats)
		}

		// 限制遍历的数量，避免过多数据导致内存占用过高
		refererCount++
		return refererCount < 50 // 最多遍历50个引用来源
	})

	// 按请求数降序排序，请求数相同时按引用来源字典序排序
	sort.Slice(refererMetrics, func(i, j int) bool {
		countI := refererMetrics[i].GetRequestCount()
		countJ := refererMetrics[j].GetRequestCount()
		if countI != countJ {
			return countI > countJ
		}
		return refererMetrics[i].Path < refererMetrics[j].Path
	})

	// 只保留前20个
	if len(refererMetrics) > 20 {
		refererMetrics = refererMetrics[:20]
	}

	// 转换为值切片
	refererMetricsValues := make([]models.PathMetricsJSON, len(refererMetrics))
	for i, metric := range refererMetrics {
		refererMetricsValues[i] = metric.ToJSON()
	}

	// 收集延迟分布
	latencyDistribution := make(map[string]int64)

	// 确保所有桶都存在，即使计数为0
	buckets := []string{"lt10ms", "10-50ms", "50-200ms", "200-1000ms", "gt1s"}
	for _, bucket := range buckets {
		if counter, ok := c.latencyBuckets.Load(bucket); ok {
			if counter != nil {
				value := atomic.LoadInt64(counter.(*int64))
				latencyDistribution[bucket] = value
			} else {
				latencyDistribution[bucket] = 0
			}
		} else {
			latencyDistribution[bucket] = 0
		}
	}

	// 获取最近请求记录（使用读锁）
	recentRequests := c.recentRequests.GetAll()

	// 获取最小和最大响应时间
	minLatency := atomic.LoadInt64(&c.minLatency)
	maxLatency := atomic.LoadInt64(&c.maxLatency)
	if minLatency == math.MaxInt64 {
		minLatency = 0
	}

	// 收集带宽历史记录
	bandwidthHistory := c.getBandwidthHistory()

	return map[string]interface{}{
		"uptime":              FormatUptime(totalRuntime),
		"active_requests":     atomic.LoadInt64(&c.activeRequests),
		"total_bytes":         atomic.LoadInt64(&c.totalBytes),
		"num_goroutine":       runtime.NumGoroutine(),
		"memory_usage":        utils.FormatBytes(int64(mem.Alloc)),
		"avg_response_time":   fmt.Sprintf("%.2fms", avgLatency/float64(time.Millisecond)),
		"requests_per_second": requestsPerSecond,
		"bytes_per_second":    float64(atomic.LoadInt64(&c.totalBytes)) / totalRuntime.Seconds(),
		"status_code_stats":   statusCodeStats,
		"top_referers":        refererMetricsValues,
		"recent_requests":     recentRequests,
		"latency_stats": map[string]interface{}{
			"min":          fmt.Sprintf("%.2fms", float64(minLatency)/float64(time.Millisecond)),
			"max":          fmt.Sprintf("%.2fms", float64(maxLatency)/float64(time.Millisecond)),
			"distribution": latencyDistribution,
		},
		"bandwidth_history": bandwidthHistory,
		"current_bandwidth": utils.FormatBytes(int64(c.getCurrentBandwidth())) + "/s",
	}
}

func (c *Collector) SaveMetrics(stats map[string]interface{}) error {
	lastSaveTime = time.Now()

	// 如果指标存储服务已初始化，则调用它来保存指标
	if metricsStorage != nil {
		return metricsStorage.SaveMetrics()
	}

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
	if c.totalBytes < 0 ||
		c.activeRequests < 0 {
		return fmt.Errorf("invalid negative stats values")
	}

	// 验证状态码统计
	var statusCodeTotal int64
	c.statusCodeStats.Range(func(key, value interface{}) bool {
		count := atomic.LoadInt64(value.(*int64))
		if count < 0 {
			return false
		}
		statusCodeTotal += count
		return true
	})

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
	if err := c.validateLoadedData(); err != nil {
		return err
	}
	return nil
}

// 添加定期检查数据一致性的功能
func (c *Collector) startConsistencyChecker() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			if err := c.validateLoadedData(); err != nil {
				log.Printf("[Metrics] Data consistency check failed: %v", err)
				// 可以在这里添加修复逻辑或报警通知
			}
		}
	}()
}

// updateBandwidthStats 更新带宽统计
func (c *Collector) updateBandwidthStats(bytes int64) {
	c.bandwidthStats.Lock()
	defer c.bandwidthStats.Unlock()

	now := time.Now()
	if now.Sub(c.bandwidthStats.lastUpdate) >= c.bandwidthStats.window {
		// 保存当前时间窗口的数据
		key := c.bandwidthStats.lastUpdate.Format("01-02 15:04")
		c.bandwidthStats.history[key] = c.bandwidthStats.current

		// 清理旧数据（保留最近5个时间窗口）
		if len(c.bandwidthStats.history) > 5 {
			var oldestTime time.Time
			var oldestKey string
			for k := range c.bandwidthStats.history {
				t, _ := time.Parse("01-02 15:04", k)
				if oldestTime.IsZero() || t.Before(oldestTime) {
					oldestTime = t
					oldestKey = k
				}
			}
			delete(c.bandwidthStats.history, oldestKey)
		}

		// 重置当前窗口
		c.bandwidthStats.current = bytes
		c.bandwidthStats.lastUpdate = now
	} else {
		c.bandwidthStats.current += bytes
	}
}

// getCurrentBandwidth 获取当前带宽
func (c *Collector) getCurrentBandwidth() float64 {
	c.bandwidthStats.RLock()
	defer c.bandwidthStats.RUnlock()

	now := time.Now()
	duration := now.Sub(c.bandwidthStats.lastUpdate).Seconds()
	if duration == 0 {
		return 0
	}
	return float64(c.bandwidthStats.current) / duration
}

// getBandwidthHistory 获取带宽历史记录
func (c *Collector) getBandwidthHistory() map[string]string {
	c.bandwidthStats.RLock()
	defer c.bandwidthStats.RUnlock()

	history := make(map[string]string)
	for k, v := range c.bandwidthStats.history {
		history[k] = utils.FormatBytes(v) + "/min"
	}
	return history
}

// simplifyReferer 简化引用来源URL，只保留域名部分
func simplifyReferer(referer string) string {
	// 移除协议部分
	if idx := strings.Index(referer, "://"); idx != -1 {
		referer = referer[idx+3:]
	}

	// 只保留域名部分
	if idx := strings.Index(referer, "/"); idx != -1 {
		referer = referer[:idx]
	}

	return referer
}

// startCleanupTask 启动定期清理任务
func (c *Collector) startCleanupTask() {
	go func() {
		// 先立即执行一次清理
		c.cleanupOldData()

		ticker := time.NewTicker(15 * time.Minute) // 每15分钟清理一次
		defer ticker.Stop()

		for range ticker.C {
			c.cleanupOldData()
		}
	}()
}

// cleanupOldData 清理旧数据
func (c *Collector) cleanupOldData() {
	log.Printf("[Metrics] 开始清理旧数据...")

	// 清理引用来源统计 - 类似地处理
	var referersToRemove []string
	var referersCount int
	var totalRefererRequests int64

	// 先收集所有引用来源及其请求数
	type refererInfo struct {
		referer string
		count   int64
	}
	var referers []refererInfo

	c.refererStats.Range(func(key, value interface{}) bool {
		referer := key.(string)
		stats := value.(*models.PathMetrics)
		count := stats.GetRequestCount()
		referersCount++
		totalRefererRequests += count
		referers = append(referers, refererInfo{referer, count})
		return true
	})

	// 按请求数排序
	sort.Slice(referers, func(i, j int) bool {
		return referers[i].count > referers[j].count
	})

	// 只保留前50个请求数最多的引用来源，或者请求数占总请求数2%以上的引用来源
	refThreshold := totalRefererRequests / 50 // 2%的阈值
	if refThreshold < 5 {
		refThreshold = 5 // 至少保留请求数>=5的引用来源
	}

	// 标记要删除的引用来源
	for _, ri := range referers {
		if len(referers)-len(referersToRemove) <= 50 {
			// 已经只剩下50个引用来源了，不再删除
			break
		}

		if ri.count < refThreshold {
			referersToRemove = append(referersToRemove, ri.referer)
		}
	}

	// 删除标记的引用来源
	for _, referer := range referersToRemove {
		c.refererStats.Delete(referer)
	}

	// 清理带宽历史 - 只保留最近的记录
	c.bandwidthStats.Lock()
	if len(c.bandwidthStats.history) > 10 {
		// 找出最旧的记录并删除
		var oldestKeys []string
		var oldestTimes []time.Time

		for k := range c.bandwidthStats.history {
			t, err := time.Parse("01-02 15:04", k)
			if err != nil {
				continue
			}
			oldestTimes = append(oldestTimes, t)
			oldestKeys = append(oldestKeys, k)
		}

		// 按时间排序
		sort.Slice(oldestKeys, func(i, j int) bool {
			return oldestTimes[i].Before(oldestTimes[j])
		})

		// 删除最旧的记录，只保留最近10条
		for i := 0; i < len(oldestKeys)-10; i++ {
			delete(c.bandwidthStats.history, oldestKeys[i])
		}
	}
	c.bandwidthStats.Unlock()

	// 强制进行一次GC
	runtime.GC()

	// 打印内存使用情况
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	log.Printf("[Metrics] 清理完成: 删除了 %d/%d 个引用来源, 当前内存使用: %s",
		len(referersToRemove), referersCount,
		utils.FormatBytes(int64(mem.Alloc)))
}
