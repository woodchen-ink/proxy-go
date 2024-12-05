package metrics

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/constants"
	"proxy-go/internal/models"
	"proxy-go/internal/monitor"
	"proxy-go/internal/utils"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Collector struct {
	startTime       time.Time
	activeRequests  int64
	totalRequests   int64
	totalErrors     int64
	totalBytes      atomic.Int64
	latencySum      atomic.Int64
	persistentStats struct {
		totalRequests atomic.Int64
		totalErrors   atomic.Int64
		totalBytes    atomic.Int64
	}
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

	// 加载历史数据
	if lastMetrics, err := db.GetLastMetrics(); err == nil && lastMetrics != nil {
		globalCollector.persistentStats.totalRequests.Store(lastMetrics.TotalRequests)
		globalCollector.persistentStats.totalErrors.Store(lastMetrics.TotalErrors)
		globalCollector.persistentStats.totalBytes.Store(lastMetrics.TotalBytes)
		if err := loadRecentStatusStats(db); err != nil {
			log.Printf("Warning: Failed to load recent status stats: %v", err)
		}
		log.Printf("Loaded historical metrics: requests=%d, errors=%d, bytes=%d",
			lastMetrics.TotalRequests, lastMetrics.TotalErrors, lastMetrics.TotalBytes)
	}

	// 加载最近5分钟的统计数据
	if err := globalCollector.LoadRecentStats(); err != nil {
		log.Printf("Warning: Failed to load recent stats: %v", err)
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
		// 避免所有实例同时保存
		time.Sleep(time.Duration(rand.Int63n(60)) * time.Second)
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			stats := globalCollector.GetStats()
			start := time.Now()

			// 保存基础指标和完整统计数据
			if err := db.SaveMetrics(stats); err != nil {
				log.Printf("Error saving metrics: %v", err)
			} else {
				log.Printf("Basic metrics saved successfully")
			}

			// 同时保存完整统计数据
			if err := db.SaveFullMetrics(stats); err != nil {
				log.Printf("Error saving full metrics: %v", err)
			} else {
				log.Printf("Metrics saved in %v", time.Since(start))
			}
		}
	}()

	// 设置程序退出时的处理
	utils.SetupCloseHandler(func() {
		log.Println("Saving final metrics before shutdown...")
		// 确保所有正在进行的操作完成
		time.Sleep(time.Second)

		stats := globalCollector.GetStats()
		if err := db.SaveMetrics(stats); err != nil {
			log.Printf("Error saving final metrics: %v", err)
		} else {
			log.Printf("Basic metrics saved successfully")
		}

		// 保存完整统计数据
		if err := db.SaveFullMetrics(stats); err != nil {
			log.Printf("Error saving full metrics: %v", err)
		} else {
			log.Printf("Full metrics saved successfully")
		}

		// 等待数据写入完成
		time.Sleep(time.Second)

		db.Close()
		log.Println("Database closed successfully")
	})

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

	// 基础指标 - 合并当前会话和持久化的数据
	currentRequests := atomic.LoadInt64(&c.totalRequests)
	currentErrors := atomic.LoadInt64(&c.totalErrors)
	currentBytes := c.totalBytes.Load()

	totalRequests := currentRequests + c.persistentStats.totalRequests.Load()
	totalErrors := currentErrors + c.persistentStats.totalErrors.Load()
	totalBytes := currentBytes + c.persistentStats.totalBytes.Load()

	// 计算每秒指标
	uptime := time.Since(c.startTime).Seconds()
	stats["requests_per_second"] = float64(currentRequests) / Max(uptime, 1)
	stats["bytes_per_second"] = float64(currentBytes) / Max(uptime, 1)

	stats["active_requests"] = atomic.LoadInt64(&c.activeRequests)
	stats["total_requests"] = totalRequests
	stats["total_errors"] = totalErrors
	stats["total_bytes"] = totalBytes

	// 系统指标
	stats["num_goroutine"] = runtime.NumGoroutine()
	stats["memory_usage"] = FormatBytes(m.Alloc)

	// 延迟指标
	currentTotalRequests := atomic.LoadInt64(&c.totalRequests)
	latencySum := c.latencySum.Load()
	if currentTotalRequests <= 0 || latencySum <= 0 {
		stats["avg_latency"] = int64(0)
	} else {
		stats["avg_latency"] = latencySum / currentTotalRequests
	}

	// 状态码统计
	statusStats := make(map[string]int64)
	for i := range c.statusStats {
		if i < len(c.statusStats) {
			statusStats[fmt.Sprintf("%dxx", i+1)] = c.statusStats[i].Load()
		}
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
			AvgLatency:       formatAvgLatency(stats.LatencySum.Load(), stats.Requests.Load()),
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

func (c *Collector) SaveMetrics(stats map[string]interface{}) error {
	// 更新持久化数据
	c.persistentStats.totalRequests.Store(stats["total_requests"].(int64))
	c.persistentStats.totalErrors.Store(stats["total_errors"].(int64))
	c.persistentStats.totalBytes.Store(stats["total_bytes"].(int64))

	// 重置当前会话计数器
	atomic.StoreInt64(&c.totalRequests, 0)
	atomic.StoreInt64(&c.totalErrors, 0)
	c.totalBytes.Store(0)
	c.latencySum.Store(0)

	// 重置状态码统计
	for i := range c.statusStats {
		c.statusStats[i].Store(0)
	}

	// 重置路径统计
	c.pathStats.Range(func(key, _ interface{}) bool {
		c.pathStats.Delete(key)
		return true
	})

	// 重置引用来源统计
	c.refererStats.Range(func(key, _ interface{}) bool {
		c.refererStats.Delete(key)
		return true
	})

	return c.db.SaveMetrics(stats)
}

// LoadRecentStats 加载最近的统计数据
func (c *Collector) LoadRecentStats() error {
	// 加载最近5分钟的数据
	row := c.db.DB.QueryRow(`
		SELECT 
			total_requests, total_errors, total_bytes, avg_latency
		FROM metrics_history 
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		ORDER BY timestamp DESC 
		LIMIT 1
	`)

	var metrics models.HistoricalMetrics
	if err := row.Scan(
		&metrics.TotalRequests,
		&metrics.TotalErrors,
		&metrics.TotalBytes,
		&metrics.AvgLatency,
	); err != nil && err != sql.ErrNoRows {
		return err
	}

	// 更新持久化数据
	if metrics.TotalRequests > 0 {
		c.persistentStats.totalRequests.Store(metrics.TotalRequests)
		c.persistentStats.totalErrors.Store(metrics.TotalErrors)
		c.persistentStats.totalBytes.Store(metrics.TotalBytes)
	}

	// 加载状态码统计
	rows, err := c.db.DB.Query(`
		SELECT status_group, SUM(count) as count 
		FROM status_code_history 
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		GROUP BY status_group
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var group string
		var count int64
		if err := rows.Scan(&group, &count); err != nil {
			return err
		}
		if len(group) > 0 {
			idx := (int(group[0]) - '0') - 1
			if idx >= 0 && idx < len(c.statusStats) {
				c.statusStats[idx].Store(count)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error scanning status code rows: %v", err)
	}

	// 加载路径统计
	rows, err = c.db.DB.Query(`
		SELECT 
			path, 
			SUM(request_count) as requests,
			SUM(error_count) as errors,
			AVG(bytes_transferred) as bytes,
			AVG(avg_latency) as latency
		FROM popular_paths_history
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		GROUP BY path
		ORDER BY requests DESC
		LIMIT 10
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var path string
		var requests, errors, bytes int64
		var latency float64
		if err := rows.Scan(&path, &requests, &errors, &bytes, &latency); err != nil {
			return err
		}
		stats := &models.PathStats{}
		stats.Requests.Store(requests)
		stats.Errors.Store(errors)
		stats.Bytes.Store(bytes)
		stats.LatencySum.Store(int64(latency))
		c.pathStats.Store(path, stats)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error scanning path stats rows: %v", err)
	}

	// 加载引用来源统计
	rows, err = c.db.DB.Query(`
		SELECT 
			referer,
			SUM(request_count) as requests
		FROM referer_history
		WHERE timestamp >= datetime('now', '-5', 'minutes')
		GROUP BY referer
		ORDER BY requests DESC
		LIMIT 10
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var referer string
		var requests int64
		if err := rows.Scan(&referer, &requests); err != nil {
			return err
		}
		stats := &models.PathStats{}
		stats.Requests.Store(requests)
		c.refererStats.Store(referer, stats)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error scanning referer stats rows: %v", err)
	}

	return nil
}

func formatAvgLatency(latencySum, requests int64) string {
	if requests <= 0 || latencySum <= 0 {
		return "0 ms"
	}
	return FormatDuration(time.Duration(latencySum / requests))
}

func loadRecentStatusStats(db *models.MetricsDB) error {
	rows, err := db.DB.Query(`
		SELECT status_group, count 
		FROM status_stats 
		WHERE timestamp >= datetime('now', '-5', 'minutes')
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var group string
		var count int64
		if err := rows.Scan(&group, &count); err != nil {
			return err
		}
		if len(group) > 0 {
			idx := (int(group[0]) - '0') - 1
			if idx >= 0 && idx < len(globalCollector.statusStats) {
				globalCollector.statusStats[idx].Store(count)
			}
		}
	}
	return rows.Err()
}
