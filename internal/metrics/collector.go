package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/constants"
	"proxy-go/internal/models"
	"proxy-go/internal/monitor"
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
		items  [1000]*models.RequestLog
		cursor atomic.Int64
	}
	cache     *cache.Cache
	monitor   *monitor.Monitor
	statsPool sync.Pool
}

var globalCollector *Collector

func InitCollector(config *config.Config) error {
	globalCollector = &Collector{
		startTime:      time.Now(),
		pathStats:      sync.Map{},
		statusStats:    [6]atomic.Int64{},
		latencyBuckets: [10]atomic.Int64{},
	}

	// 初始化 cache
	globalCollector.cache = cache.NewCache(constants.CacheTTL)

	// 初始化监控器
	globalCollector.monitor = monitor.NewMonitor(globalCollector)

	// 初始化对象池
	globalCollector.statsPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{}, 20)
		},
	}

	// 设置最后保存时间
	lastSaveTime = time.Now()

	return nil
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
		pathStats := stats.(*models.PathStats)
		pathStats.Requests.Add(1)
		if status >= 400 {
			pathStats.Errors.Add(1)
		}
		pathStats.Bytes.Add(bytes)
		pathStats.LatencySum.Add(int64(latency))
	} else {
		newStats := &models.PathStats{}
		newStats.Requests.Add(1)
		if status >= 400 {
			newStats.Errors.Add(1)
		}
		newStats.Bytes.Add(bytes)
		newStats.LatencySum.Add(int64(latency))
		c.pathStats.Store(path, newStats)
	}

	// 更新引用来源统计
	if referer := r.Header.Get("Referer"); referer != "" {
		if stats, ok := c.refererStats.Load(referer); ok {
			stats.(*models.PathStats).Requests.Add(1)
		} else {
			newStats := &models.PathStats{}
			newStats.Requests.Add(1)
			c.refererStats.Store(referer, newStats)
		}
	}

	// 记录最近的请求
	log := &models.RequestLog{
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

	// 更新错误统计
	if status >= 400 {
		c.monitor.RecordError()
	}
	c.monitor.RecordRequest()

	// 检查延迟
	c.monitor.CheckLatency(latency, bytes)
}

func (c *Collector) GetStats() map[string]interface{} {
	// 先查缓存
	if stats, ok := c.cache.Get("stats"); ok {
		if statsMap, ok := stats.(map[string]interface{}); ok {
			return statsMap
		}
	}

	stats := c.statsPool.Get().(map[string]interface{})
	defer c.statsPool.Put(stats)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(c.startTime)
	currentRequests := atomic.LoadInt64(&c.totalRequests)
	currentErrors := atomic.LoadInt64(&c.totalErrors)
	currentBytes := c.totalBytes.Load()

	// 计算错误率
	var errorRate float64
	if currentRequests > 0 {
		errorRate = float64(currentErrors) / float64(currentRequests)
	}

	// 基础指标
	stats["uptime"] = uptime.String()
	stats["active_requests"] = atomic.LoadInt64(&c.activeRequests)
	stats["total_requests"] = currentRequests
	stats["total_errors"] = currentErrors
	stats["error_rate"] = errorRate
	stats["total_bytes"] = currentBytes
	stats["bytes_per_second"] = float64(currentBytes) / Max(uptime.Seconds(), 1)
	stats["requests_per_second"] = float64(currentRequests) / Max(uptime.Seconds(), 1)

	// 系统指标
	stats["num_goroutine"] = runtime.NumGoroutine()
	stats["memory_usage"] = FormatBytes(m.Alloc)

	// 延迟指标
	latencySum := c.latencySum.Load()
	if currentRequests > 0 {
		stats["avg_response_time"] = FormatDuration(time.Duration(latencySum / currentRequests))
	} else {
		stats["avg_response_time"] = FormatDuration(0)
	}

	// 状态码统计
	statusStats := make(map[string]int64)
	for i := range c.statusStats {
		statusStats[fmt.Sprintf("%dxx", i+1)] = c.statusStats[i].Load()
	}
	stats["status_code_stats"] = statusStats

	// 延迟百分位数
	stats["latency_percentiles"] = make([]float64, 0)

	// 路径统计
	var pathMetrics []models.PathMetrics
	c.pathStats.Range(func(key, value interface{}) bool {
		stats := value.(*models.PathStats)
		if stats.Requests.Load() > 0 {
			pathMetrics = append(pathMetrics, models.PathMetrics{
				Path:             key.(string),
				RequestCount:     stats.Requests.Load(),
				ErrorCount:       stats.Errors.Load(),
				AvgLatency:       formatAvgLatency(stats.LatencySum.Load(), stats.Requests.Load()),
				BytesTransferred: stats.Bytes.Load(),
			})
		}
		return true
	})

	// 按请求数排序
	sort.Slice(pathMetrics, func(i, j int) bool {
		return pathMetrics[i].RequestCount > pathMetrics[j].RequestCount
	})

	if len(pathMetrics) > 10 {
		stats["top_paths"] = pathMetrics[:10]
	} else {
		stats["top_paths"] = pathMetrics
	}

	// 最近请求
	stats["recent_requests"] = c.getRecentRequests()

	// 引用来源统计
	var refererMetrics []models.PathMetrics
	c.refererStats.Range(func(key, value interface{}) bool {
		stats := value.(*models.PathStats)
		if stats.Requests.Load() > 0 {
			refererMetrics = append(refererMetrics, models.PathMetrics{
				Path:         key.(string),
				RequestCount: stats.Requests.Load(),
			})
		}
		return true
	})

	// 按请求数排序
	sort.Slice(refererMetrics, func(i, j int) bool {
		return refererMetrics[i].RequestCount > refererMetrics[j].RequestCount
	})

	if len(refererMetrics) > 10 {
		stats["top_referers"] = refererMetrics[:10]
	} else {
		stats["top_referers"] = refererMetrics
	}

	// 检查告警
	c.monitor.CheckMetrics(stats)

	// 写入缓存
	c.cache.Set("stats", stats)

	return stats
}

func (c *Collector) getRecentRequests() []models.RequestLog {
	var recentReqs []models.RequestLog
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
		c.totalBytes.Load() < 0 {
		return fmt.Errorf("invalid stats values")
	}

	// 验证错误数不能大于总请求数
	if atomic.LoadInt64(&c.totalErrors) > atomic.LoadInt64(&c.totalRequests) {
		return fmt.Errorf("total errors exceeds total requests")
	}

	// 验证状态码统计
	for i := range c.statusStats {
		if c.statusStats[i].Load() < 0 {
			return fmt.Errorf("invalid status code count at index %d", i)
		}
	}

	// 验证路径统计
	var totalPathRequests int64
	c.pathStats.Range(func(_, value interface{}) bool {
		stats := value.(*models.PathStats)
		if stats.Requests.Load() < 0 || stats.Errors.Load() < 0 {
			return false
		}
		totalPathRequests += stats.Requests.Load()
		return true
	})

	// 验证总数一致性
	if totalPathRequests > atomic.LoadInt64(&c.totalRequests) {
		return fmt.Errorf("path stats total exceeds total requests")
	}

	return nil
}

func formatAvgLatency(latencySum, requests int64) string {
	if requests <= 0 || latencySum <= 0 {
		return "0 ms"
	}
	return FormatDuration(time.Duration(latencySum / requests))
}

// SaveBackup 保存数据备份
func (c *Collector) SaveBackup() error {
	stats := c.GetStats()
	backupFile := fmt.Sprintf("backup_%s.json", time.Now().Format("20060102_150405"))

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path.Join("data/backup", backupFile), data, 0644)
}

// LoadBackup 加载备份数据
func (c *Collector) LoadBackup(backupFile string) error {
	data, err := os.ReadFile(path.Join("data/backup", backupFile))
	if err != nil {
		return err
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(data, &stats); err != nil {
		return err
	}

	return c.RestoreFromBackup(stats)
}

// RestoreFromBackup 从备份恢复数据
func (c *Collector) RestoreFromBackup(stats map[string]interface{}) error {
	// 恢复基础指标
	if totalReqs, ok := stats["total_requests"].(int64); ok {
		atomic.StoreInt64(&c.totalRequests, totalReqs)
	}
	if totalErrs, ok := stats["total_errors"].(int64); ok {
		atomic.StoreInt64(&c.totalErrors, totalErrs)
	}
	if totalBytes, ok := stats["total_bytes"].(int64); ok {
		c.totalBytes.Store(totalBytes)
	}

	// 恢复状态码统计
	if statusStats, ok := stats["status_code_stats"].(map[string]int64); ok {
		for group, count := range statusStats {
			if len(group) > 0 {
				idx := (int(group[0]) - '0') - 1
				if idx >= 0 && idx < len(c.statusStats) {
					c.statusStats[idx].Store(count)
				}
			}
		}
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
