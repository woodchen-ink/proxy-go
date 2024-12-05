package metrics

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
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

const (
	// 数据变化率阈值
	highThreshold = 0.8 // 高变化率阈值
	lowThreshold  = 0.2 // 低变化率阈值
)

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

	// 1. 先初始化 cache
	globalCollector.cache = cache.NewCache(constants.CacheTTL)

	// 2. 初始化监控器
	globalCollector.monitor = monitor.NewMonitor(globalCollector)

	// 3. 如果配置了飞书webhook，则启用飞书告警
	if config.Metrics.FeishuWebhook != "" {
		globalCollector.monitor.AddHandler(
			monitor.NewFeishuHandler(config.Metrics.FeishuWebhook),
		)
		log.Printf("Feishu alert enabled")
	}

	// 4. 初始化对象池
	globalCollector.statsPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{}, 20)
		},
	}

	// 5. 设置最后保存时间
	lastSaveTime = time.Now()

	// 6. 加载历史数据
	if lastMetrics, err := db.GetLastMetrics(); err == nil && lastMetrics != nil {
		globalCollector.persistentStats.totalRequests.Store(lastMetrics.TotalRequests)
		globalCollector.persistentStats.totalErrors.Store(lastMetrics.TotalErrors)
		globalCollector.persistentStats.totalBytes.Store(lastMetrics.TotalBytes)

		// 确保在加载历史数据后立即保存一次，以更新所有统计信息
		stats := globalCollector.GetStats()
		if err := db.SaveAllMetrics(stats); err != nil {
			log.Printf("Warning: Failed to save initial metrics: %v", err)
		}

		if err := globalCollector.LoadRecentStats(); err != nil {
			log.Printf("Warning: Failed to load recent stats: %v", err)
		}
		log.Printf("Loaded historical metrics: requests=%d, errors=%d, bytes=%d",
			lastMetrics.TotalRequests, lastMetrics.TotalErrors, lastMetrics.TotalBytes)
	}

	// 7. 启动定时保存
	go func() {
		time.Sleep(time.Duration(rand.Int63n(60)) * time.Second)
		var (
			saveInterval = 10 * time.Minute
			minInterval  = 5 * time.Minute
			maxInterval  = 15 * time.Minute
		)
		ticker := time.NewTicker(saveInterval)
		lastChangeTime := time.Now()

		for range ticker.C {
			stats := globalCollector.GetStats()
			start := time.Now()

			// 根据数据变化频率动态调整保存间隔
			changeRate := calculateChangeRate(stats)
			// 避免频繁调整
			if time.Since(lastChangeTime) > time.Minute {
				if changeRate > highThreshold && saveInterval > minInterval {
					saveInterval = saveInterval - time.Minute
					lastChangeTime = time.Now()
				} else if changeRate < lowThreshold && saveInterval < maxInterval {
					saveInterval = saveInterval + time.Minute
					lastChangeTime = time.Now()
				}
				ticker.Reset(saveInterval)
				log.Printf("Adjusted save interval to %v (change rate: %.2f)",
					saveInterval, changeRate)
			}

			if err := db.SaveAllMetrics(stats); err != nil {
				log.Printf("Error saving metrics: %v", err)
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
		if err := db.SaveAllMetrics(stats); err != nil {
			log.Printf("Error saving final metrics: %v", err)
		} else {
			log.Printf("Basic metrics saved successfully")
		}

		// 等待数据写入完成
		time.Sleep(time.Second)

		db.Close()
		log.Println("Database closed successfully")
	})

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

	// 确保所有字段都被初始化
	stats := c.statsPool.Get().(map[string]interface{})
	defer c.statsPool.Put(stats)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 基础指标
	uptime := time.Since(c.startTime)
	currentRequests := atomic.LoadInt64(&c.totalRequests)
	currentErrors := atomic.LoadInt64(&c.totalErrors)
	currentBytes := c.totalBytes.Load()

	totalRequests := currentRequests + c.persistentStats.totalRequests.Load()
	totalErrors := currentErrors + c.persistentStats.totalErrors.Load()
	totalBytes := currentBytes + c.persistentStats.totalBytes.Load()

	// 计算错误率
	var errorRate float64
	if totalRequests > 0 {
		errorRate = float64(totalErrors) / float64(totalRequests)
	}

	// 基础指标
	stats["uptime"] = uptime.String()
	stats["active_requests"] = atomic.LoadInt64(&c.activeRequests)
	stats["total_requests"] = totalRequests
	stats["total_errors"] = totalErrors
	stats["error_rate"] = errorRate
	stats["total_bytes"] = totalBytes
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

func (c *Collector) GetDB() *models.MetricsDB {
	return c.db
}

func (c *Collector) SaveMetrics(stats map[string]interface{}) error {
	// 更新持久化数据
	if totalReqs, ok := stats["total_requests"].(int64); ok {
		c.persistentStats.totalRequests.Store(totalReqs)
	}
	if totalErrs, ok := stats["total_errors"].(int64); ok {
		c.persistentStats.totalErrors.Store(totalErrs)
	}
	if totalBytes, ok := stats["total_bytes"].(int64); ok {
		c.persistentStats.totalBytes.Store(totalBytes)
	}

	// 在重置前记录当前值用于日志
	oldRequests := atomic.LoadInt64(&c.totalRequests)
	oldErrors := atomic.LoadInt64(&c.totalErrors)
	oldBytes := c.totalBytes.Load()

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

	// 保存到数据库
	err := c.db.SaveMetrics(stats)
	if err == nil {
		// 记录重置日志
		log.Printf("Reset counters: requests=%d->0, errors=%d->0, bytes=%d->0",
			oldRequests, oldErrors, oldBytes)
		lastSaveTime = time.Now() // 更新最后保存时间
	}

	return err
}

// LoadRecentStats 加载最近的统计数据
func (c *Collector) LoadRecentStats() error {
	start := time.Now()
	log.Printf("Starting to load recent stats...")

	var err error
	// 添加重试机制
	for retryCount := 0; retryCount < 3; retryCount++ {
		if err = c.loadRecentStatsInternal(); err == nil {
			// 添加数据验证
			if err = c.validateLoadedData(); err != nil {
				log.Printf("Data validation failed: %v", err)
				continue
			}
			break
		}
		log.Printf("Retry %d/3: Failed to load stats: %v", retryCount+1, err)
		time.Sleep(time.Second)
	}

	if err != nil {
		return fmt.Errorf("failed to load stats after retries: %v", err)
	}

	log.Printf("Successfully loaded all stats in %v", time.Since(start))
	return nil
}

// loadRecentStatsInternal 内部加载函数
func (c *Collector) loadRecentStatsInternal() error {
	loadStart := time.Now()
	// 先加载基础指标
	if err := c.loadBasicMetrics(); err != nil {
		return fmt.Errorf("failed to load basic metrics: %v", err)
	}
	log.Printf("Loaded basic metrics in %v", time.Since(loadStart))

	// 再加载状态码统计
	statusStart := time.Now()
	if err := c.loadStatusStats(); err != nil {
		return fmt.Errorf("failed to load status stats: %v", err)
	}
	log.Printf("Loaded status codes in %v", time.Since(statusStart))

	// 最后加载路径和引用来源统计
	pathStart := time.Now()
	if err := c.loadPathAndRefererStats(); err != nil {
		return fmt.Errorf("failed to load path and referer stats: %v", err)
	}
	log.Printf("Loaded path and referer stats in %v", time.Since(pathStart))

	return nil
}

func formatAvgLatency(latencySum, requests int64) string {
	if requests <= 0 || latencySum <= 0 {
		return "0 ms"
	}
	return FormatDuration(time.Duration(latencySum / requests))
}

// calculateChangeRate 计算数据变化率
func calculateChangeRate(stats map[string]interface{}) float64 {
	// 获取当前值
	currentReqs, _ := stats["total_requests"].(int64)
	currentErrs, _ := stats["total_errors"].(int64)
	currentBytes, _ := stats["total_bytes"].(int64)

	// 计算变化率 (可以根据实际需求调整计算方法)
	var changeRate float64
	if currentReqs > 0 {
		// 计算请求数的变化率
		reqRate := float64(currentReqs) / float64(constants.MaxRequestsPerMinute)
		// 计算错误率的变化
		errRate := float64(currentErrs) / float64(currentReqs)
		// 计算流量的变化率
		bytesRate := float64(currentBytes) / float64(constants.MaxBytesPerMinute)

		// 综合评分
		changeRate = (reqRate + errRate + bytesRate) / 3
	}

	return changeRate
}

// CheckDataConsistency 检查数据一致性
func (c *Collector) CheckDataConsistency() error {
	totalReqs := c.persistentStats.totalRequests.Load() + atomic.LoadInt64(&c.totalRequests)

	// 检查状态码统计
	var statusTotal int64
	for i := range c.statusStats {
		count := c.statusStats[i].Load()
		if count < 0 {
			return fmt.Errorf("invalid status code count: %d", count)
		}
		statusTotal += count
	}

	// 检查路径统计
	var pathTotal int64
	c.pathStats.Range(func(_, value interface{}) bool {
		stats := value.(*models.PathStats)
		count := stats.Requests.Load()
		if count < 0 {
			return false
		}
		pathTotal += count
		return true
	})

	// 修改一致性检查的逻辑
	// 1. 如果总数为0，不进行告警
	// 2. 增加容差范围到5%
	if totalReqs > 0 {
		tolerance := totalReqs / 20 // 5% 的容差
		if statusTotal > 0 && abs(statusTotal-totalReqs) > tolerance {
			log.Printf("Warning: Status code total (%d) differs from total requests (%d) by more than 5%%",
				statusTotal, totalReqs)
		}
		if pathTotal > 0 && abs(pathTotal-totalReqs) > tolerance {
			log.Printf("Warning: Path total (%d) differs from total requests (%d) by more than 5%%",
				pathTotal, totalReqs)
		}
	}

	return nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func (c *Collector) validateLoadedData() error {
	// 验证基础指标
	if c.persistentStats.totalRequests.Load() < 0 ||
		c.persistentStats.totalErrors.Load() < 0 ||
		c.persistentStats.totalBytes.Load() < 0 {
		return fmt.Errorf("invalid persistent stats values")
	}

	// 验证错误数不能大于总请求数
	if c.persistentStats.totalErrors.Load() > c.persistentStats.totalRequests.Load() {
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
	if totalPathRequests > c.persistentStats.totalRequests.Load() {
		return fmt.Errorf("path stats total exceeds total requests")
	}

	return nil
}

// loadBasicMetrics 加载基础指标
func (c *Collector) loadBasicMetrics() error {
	// 加载最近5分钟的数据
	row := c.db.DB.QueryRow(`
		SELECT 
			total_requests, total_errors, total_bytes, avg_latency
		FROM metrics_history 
		WHERE timestamp >= datetime('now', '-24', 'hours')
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
		log.Printf("Loaded persistent stats: requests=%d, errors=%d, bytes=%d",
			metrics.TotalRequests, metrics.TotalErrors, metrics.TotalBytes)
	}
	return nil
}

// loadStatusStats 加载状态码统计
func (c *Collector) loadStatusStats() error {
	rows, err := c.db.DB.Query(`
		SELECT status_group, SUM(count) as count 
		FROM status_code_history 
		WHERE timestamp >= datetime('now', '-24', 'hours')
		GROUP BY status_group
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var totalStatusCodes int64
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
				totalStatusCodes += count
			}
		}
	}
	return rows.Err()
}

// loadPathAndRefererStats 加载路径和引用来源统计
func (c *Collector) loadPathAndRefererStats() error {
	// 加载路径统计
	rows, err := c.db.DB.Query(`
		SELECT 
			path, 
			SUM(request_count) as requests,
			SUM(error_count) as errors,
			AVG(bytes_transferred) as bytes,
			AVG(avg_latency) as latency
		FROM popular_paths_history
		WHERE timestamp >= datetime('now', '-24', 'hours')
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
		return err
	}

	// 加载引用来源统计
	rows, err = c.db.DB.Query(`
		SELECT 
			referer,
			SUM(request_count) as requests
		FROM referer_history
		WHERE timestamp >= datetime('now', '-24', 'hours')
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
		var count int64
		if err := rows.Scan(&referer, &count); err != nil {
			return err
		}
		stats := &models.PathStats{}
		stats.Requests.Store(count)
		c.refererStats.Store(referer, stats)
	}

	return rows.Err()
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
		c.persistentStats.totalRequests.Store(totalReqs)
	}
	if totalErrs, ok := stats["total_errors"].(int64); ok {
		c.persistentStats.totalErrors.Store(totalErrs)
	}
	if totalBytes, ok := stats["total_bytes"].(int64); ok {
		c.persistentStats.totalBytes.Store(totalBytes)
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
