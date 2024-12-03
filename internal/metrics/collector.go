package metrics

import (
	"fmt"
	"log"
	"net/http"
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
	db        *models.MetricsDB
	cache     *cache.Cache
	monitor   *monitor.Monitor
	statsPool sync.Pool
}

var globalCollector *Collector

func InitCollector(dbPath string, config *config.Config) error {
	db, err := models.NewMetricsDB(dbPath)
	if err != nil {
		return err
	}

	globalCollector = &Collector{
		startTime:      time.Now(),
		pathStats:      sync.Map{},
		statusStats:    [6]atomic.Int64{},
		latencyBuckets: [10]atomic.Int64{},
		db:             db,
	}

	globalCollector.cache = cache.NewCache(constants.CacheTTL)
	globalCollector.monitor = monitor.NewMonitor()

	// 如果配置了飞书webhook，则启用飞书告警
	if config.Metrics.FeishuWebhook != "" {
		globalCollector.monitor.AddHandler(
			monitor.NewFeishuHandler(config.Metrics.FeishuWebhook),
		)
		log.Printf("Feishu alert enabled")
	}

	// 启动定时保存
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			stats := globalCollector.GetStats()
			if err := db.SaveMetrics(stats); err != nil {
				log.Printf("Error saving metrics: %v", err)
			} else {
				log.Printf("Metrics saved successfully")
			}
		}
	}()

	globalCollector.statsPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{}, 20)
		},
	}

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

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 确保所有字段都被初始化
	stats := make(map[string]interface{})

	// 基础指标
	stats["active_requests"] = atomic.LoadInt64(&c.activeRequests)
	stats["total_requests"] = atomic.LoadInt64(&c.totalRequests)
	stats["total_errors"] = atomic.LoadInt64(&c.totalErrors)
	stats["total_bytes"] = c.totalBytes.Load()

	// 系统指标
	stats["num_goroutine"] = runtime.NumGoroutine()
	stats["memory_usage"] = FormatBytes(m.Alloc)

	// 延迟指标
	totalRequests := atomic.LoadInt64(&c.totalRequests)
	if totalRequests > 0 {
		stats["avg_latency"] = c.latencySum.Load() / totalRequests
	} else {
		stats["avg_latency"] = int64(0)
	}

	// 状态码统计
	statusStats := make(map[string]int64)
	for i := range c.statusStats {
		statusStats[fmt.Sprintf("%dxx", i+1)] = c.statusStats[i].Load()
	}
	stats["status_code_stats"] = statusStats

	// 获取Top 10路径统计
	var pathMetrics []models.PathMetrics
	var allPaths []models.PathMetrics

	c.pathStats.Range(func(key, value interface{}) bool {
		stats := value.(*models.PathStats)
		if stats.Requests.Load() == 0 {
			return true
		}
		allPaths = append(allPaths, models.PathMetrics{
			Path:             key.(string),
			RequestCount:     stats.Requests.Load(),
			ErrorCount:       stats.Errors.Load(),
			AvgLatency:       FormatDuration(time.Duration(stats.LatencySum.Load() / stats.Requests.Load())),
			BytesTransferred: stats.Bytes.Load(),
		})
		return true
	})

	// 按请求数排序并获取前10个
	sort.Slice(allPaths, func(i, j int) bool {
		return allPaths[i].RequestCount > allPaths[j].RequestCount
	})

	if len(allPaths) > 10 {
		pathMetrics = allPaths[:10]
	} else {
		pathMetrics = allPaths
	}
	stats["top_paths"] = pathMetrics

	// 获取最近请求
	stats["recent_requests"] = c.getRecentRequests()

	// 获取Top 10引用来源
	var refererMetrics []models.PathMetrics
	var allReferers []models.PathMetrics
	c.refererStats.Range(func(key, value interface{}) bool {
		stats := value.(*models.PathStats)
		if stats.Requests.Load() == 0 {
			return true
		}
		allReferers = append(allReferers, models.PathMetrics{
			Path:         key.(string),
			RequestCount: stats.Requests.Load(),
		})
		return true
	})

	// 按请求数排序并获取前10个
	sort.Slice(allReferers, func(i, j int) bool {
		return allReferers[i].RequestCount > allReferers[j].RequestCount
	})

	if len(allReferers) > 10 {
		refererMetrics = allReferers[:10]
	} else {
		refererMetrics = allReferers
	}
	stats["top_referers"] = refererMetrics

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

func (c *Collector) GetDB() *models.MetricsDB {
	return c.db
}
