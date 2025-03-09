package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"proxy-go/internal/config"
	"proxy-go/internal/models"
	"proxy-go/internal/utils"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	pathStats       sync.Map
	statusCodeStats sync.Map
	latencyBuckets  sync.Map // 响应时间分布
	bandwidthStats  struct {
		sync.RWMutex
		window     time.Duration
		lastUpdate time.Time
		current    int64
		history    map[string]int64
	}
	recentRequests *models.RequestQueue
	pathStatsMutex sync.RWMutex
	config         *config.Config
	lastSaveTime   time.Time // 最后一次保存指标的时间
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
		instance.bandwidthStats.window = 5 * time.Minute
		instance.bandwidthStats.lastUpdate = time.Now()
		instance.bandwidthStats.history = make(map[string]int64)

		// 初始化延迟分布桶
		instance.latencyBuckets.Store("<10ms", new(int64))
		instance.latencyBuckets.Store("10-50ms", new(int64))
		instance.latencyBuckets.Store("50-200ms", new(int64))
		instance.latencyBuckets.Store("200-1000ms", new(int64))
		instance.latencyBuckets.Store(">1s", new(int64))

		// 加载历史统计数据
		if err := instance.LoadRecentStats(); err != nil {
			log.Printf("[Metrics] Warning: Failed to load stats: %v", err)
		}

		// 启动数据一致性检查器
		instance.startConsistencyChecker()

		// 启动定时保存任务
		instance.startMetricsSaver()
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
		counter := new(int64)
		*counter = 1
		c.latencyBuckets.Store(bucketKey, counter)
	}

	// 更新路径统计
	c.pathStatsMutex.Lock()
	if value, ok := c.pathStats.Load(path); ok {
		stat, ok := value.(*models.PathStats)
		if ok {
			stat.Requests.Add(1)
			stat.Bytes.Add(bytes)
			stat.LatencySum.Add(int64(latency))
			if status >= 400 {
				stat.Errors.Add(1)
			}
		}
	} else {
		newStat := &models.PathStats{
			Requests:   atomic.Int64{},
			Bytes:      atomic.Int64{},
			LatencySum: atomic.Int64{},
			Errors:     atomic.Int64{},
		}
		newStat.Requests.Store(1)
		newStat.Bytes.Store(bytes)
		newStat.LatencySum.Store(int64(latency))
		if status >= 400 {
			newStat.Errors.Store(1)
		}
		c.pathStats.Store(path, newStat)
	}
	c.pathStatsMutex.Unlock()

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

	// 收集路径统计
	pathStatsMap := make(map[string]interface{})
	c.pathStats.Range(func(key, value interface{}) bool {
		path := key.(string)
		stats, ok := value.(*models.PathStats)
		if !ok {
			return true
		}
		requestCount := stats.Requests.Load()
		if requestCount > 0 {
			latencySum := stats.LatencySum.Load()
			avgLatencyMs := float64(latencySum) / float64(requestCount) / float64(time.Millisecond)

			pathStatsMap[path] = map[string]interface{}{
				"requests":    requestCount,
				"errors":      stats.Errors.Load(),
				"bytes":       stats.Bytes.Load(),
				"latency_sum": latencySum,
				"avg_latency": fmt.Sprintf("%.2fms", avgLatencyMs),
			}
		}
		return true
	})

	// 按请求数降序排序路径
	type pathStat struct {
		Path       string
		Requests   int64
		Errors     int64
		Bytes      int64
		LatencySum int64
		AvgLatency string
	}

	// 限制路径统计的数量，只保留前N个
	maxPaths := 20 // 只保留前20个路径
	var pathStatsList []pathStat

	// 先将pathStatsMap转换为pathStatsList
	for path, statData := range pathStatsMap {
		if stat, ok := statData.(map[string]interface{}); ok {
			pathStatsList = append(pathStatsList, pathStat{
				Path:       path,
				Requests:   stat["requests"].(int64),
				Errors:     stat["errors"].(int64),
				Bytes:      stat["bytes"].(int64),
				LatencySum: stat["latency_sum"].(int64),
				AvgLatency: stat["avg_latency"].(string),
			})
		}
	}

	// 释放pathStatsMap内存
	pathStatsMap = nil

	// 按请求数降序排序，请求数相同时按路径字典序排序
	sort.Slice(pathStatsList, func(i, j int) bool {
		if pathStatsList[i].Requests != pathStatsList[j].Requests {
			return pathStatsList[i].Requests > pathStatsList[j].Requests
		}
		return pathStatsList[i].Path < pathStatsList[j].Path
	})

	// 只保留前maxPaths个
	if len(pathStatsList) > maxPaths {
		pathStatsList = pathStatsList[:maxPaths]
	}

	// 转换为有序的map
	orderedPathStats := make([]map[string]interface{}, len(pathStatsList))
	for i, ps := range pathStatsList {
		orderedPathStats[i] = map[string]interface{}{
			"path":        ps.Path,
			"requests":    ps.Requests,
			"errors":      ps.Errors,
			"bytes":       ps.Bytes,
			"latency_sum": ps.LatencySum,
			"avg_latency": ps.AvgLatency,
			"error_rate":  fmt.Sprintf("%.2f%%", float64(ps.Errors)*100/float64(ps.Requests)),
		}
	}

	// 收集延迟分布
	latencyDistribution := make(map[string]int64)
	c.latencyBuckets.Range(func(key, value interface{}) bool {
		if counter, ok := value.(*int64); ok {
			latencyDistribution[key.(string)] = atomic.LoadInt64(counter)
		} else {
			latencyDistribution[key.(string)] = value.(int64)
		}
		return true
	})

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
		"top_paths":           orderedPathStats,
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
	// 确保data目录存在
	if err := os.MkdirAll("data/metrics", 0755); err != nil {
		return fmt.Errorf("failed to create metrics directory: %v", err)
	}

	// 将统计数据保存到文件
	data, err := json.Marshal(stats) // 使用Marshal而不是MarshalIndent来减少内存使用
	if err != nil {
		return fmt.Errorf("failed to marshal metrics data: %v", err)
	}

	// 写入文件
	filename := fmt.Sprintf("data/metrics/stats_%s.json", time.Now().Format("20060102_150405"))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write metrics file: %v", err)
	}

	// 同时保存一个最新的副本
	if err := os.WriteFile("data/metrics/latest_stats.json", data, 0644); err != nil {
		return fmt.Errorf("failed to write latest metrics file: %v", err)
	}

	// 清理旧文件
	if err := c.cleanupOldMetricsFiles(); err != nil {
		log.Printf("[Metrics] Warning: Failed to cleanup old metrics files: %v", err)
	}

	// 释放内存
	data = nil
	runtime.GC()

	c.lastSaveTime = time.Now()
	log.Printf("[Metrics] Saved metrics to %s", filename)
	return nil
}

// cleanupOldMetricsFiles 清理旧的统计数据文件，只保留最近的N个文件
func (c *Collector) cleanupOldMetricsFiles() error {
	// 默认保留最近的10个文件
	maxFiles := 10

	// 如果配置中有指定，则使用配置的值
	if c.config != nil && c.config.MetricsMaxFiles > 0 {
		maxFiles = c.config.MetricsMaxFiles
	}

	// 读取metrics目录中的所有文件
	files, err := os.ReadDir("data/metrics")
	if err != nil {
		return fmt.Errorf("failed to read metrics directory: %v", err)
	}

	// 过滤出统计数据文件（排除latest_stats.json）
	var statsFiles []os.DirEntry
	for _, file := range files {
		if !file.IsDir() && file.Name() != "latest_stats.json" &&
			strings.HasPrefix(file.Name(), "stats_") && strings.HasSuffix(file.Name(), ".json") {
			statsFiles = append(statsFiles, file)
		}
	}

	// 如果文件数量未超过限制，则不需要清理
	if len(statsFiles) <= maxFiles {
		return nil
	}

	// 获取文件信息并创建带时间戳的文件列表
	type fileInfo struct {
		entry    os.DirEntry
		modTime  time.Time
		fullPath string
	}

	var filesWithInfo []fileInfo
	for _, file := range statsFiles {
		info, err := file.Info()
		if err != nil {
			log.Printf("[Metrics] Warning: Failed to get file info for %s: %v", file.Name(), err)
			continue
		}
		filesWithInfo = append(filesWithInfo, fileInfo{
			entry:    file,
			modTime:  info.ModTime(),
			fullPath: filepath.Join("data/metrics", file.Name()),
		})
	}

	// 释放statsFiles内存
	statsFiles = nil

	// 按修改时间排序文件（从新到旧）
	sort.Slice(filesWithInfo, func(i, j int) bool {
		return filesWithInfo[i].modTime.After(filesWithInfo[j].modTime)
	})

	// 删除多余的旧文件
	for i := maxFiles; i < len(filesWithInfo); i++ {
		if err := os.Remove(filesWithInfo[i].fullPath); err != nil {
			return fmt.Errorf("failed to remove old metrics file %s: %v", filesWithInfo[i].fullPath, err)
		}
		log.Printf("[Metrics] Removed old metrics file: %s", filesWithInfo[i].fullPath)
	}

	// 释放filesWithInfo内存
	filesWithInfo = nil
	runtime.GC()

	return nil
}

// LoadRecentStats 从文件加载最近的统计数据
func (c *Collector) LoadRecentStats() error {
	start := time.Now()
	log.Printf("[Metrics] Loading stats...")

	// 尝试从最新的统计文件加载数据
	data, err := os.ReadFile("data/metrics/latest_stats.json")
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[Metrics] No previous stats found, starting fresh")
			return nil
		}
		return fmt.Errorf("failed to read metrics file: %v", err)
	}

	// 解析JSON数据
	var stats map[string]interface{}
	if err := json.Unmarshal(data, &stats); err != nil {
		return fmt.Errorf("failed to unmarshal metrics data: %v", err)
	}

	// 恢复统计数据
	if totalBytes, ok := stats["total_bytes"].(float64); ok {
		atomic.StoreInt64(&c.totalBytes, int64(totalBytes))
	}

	// 恢复路径统计
	if pathStats, ok := stats["path_stats"].(map[string]interface{}); ok {
		for path, stat := range pathStats {
			if statMap, ok := stat.(map[string]interface{}); ok {
				pathStat := &models.PathStats{}

				if count, ok := statMap["requests"].(float64); ok {
					pathStat.Requests.Store(int64(count))
				}

				if bytes, ok := statMap["bytes"].(float64); ok {
					pathStat.Bytes.Store(int64(bytes))
				}

				if errors, ok := statMap["errors"].(float64); ok {
					pathStat.Errors.Store(int64(errors))
				}

				if latency, ok := statMap["latency_sum"].(float64); ok {
					pathStat.LatencySum.Store(int64(latency))
				}

				c.pathStats.Store(path, pathStat)
			}
		}
	}

	// 恢复状态码统计
	if statusStats, ok := stats["status_codes"].(map[string]interface{}); ok {
		for code, count := range statusStats {
			if countVal, ok := count.(float64); ok {
				codeInt := 0
				if _, err := fmt.Sscanf(code, "%d", &codeInt); err == nil {
					var counter int64 = int64(countVal)
					c.statusCodeStats.Store(codeInt, &counter)
				}
			}
		}
	}

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

	// 验证路径统计
	var totalPathRequests int64
	c.pathStats.Range(func(_, value interface{}) bool {
		stats, ok := value.(*models.PathStats)
		if !ok {
			return true
		}
		requestCount := stats.Requests.Load()
		errorCount := stats.Errors.Load()
		if requestCount < 0 || errorCount < 0 {
			return false
		}
		if errorCount > requestCount {
			return false
		}
		totalPathRequests += requestCount
		return true
	})

	if totalPathRequests != statusCodeTotal {
		return fmt.Errorf("path stats total (%d) does not match status code total (%d)",
			totalPathRequests, statusCodeTotal)
	}

	return nil
}

// GetLastSaveTime 实现 interfaces.MetricsCollector 接口
func (c *Collector) GetLastSaveTime() time.Time {
	return c.lastSaveTime
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

// startMetricsSaver 启动定时保存统计数据的任务
func (c *Collector) startMetricsSaver() {
	// 定义保存间隔，可以根据需要调整
	saveInterval := 15 * time.Minute

	// 如果配置中有指定，则使用配置的间隔
	if c.config != nil && c.config.MetricsSaveInterval > 0 {
		saveInterval = time.Duration(c.config.MetricsSaveInterval) * time.Minute
	}

	ticker := time.NewTicker(saveInterval)
	go func() {
		for range ticker.C {
			func() {
				// 使用匿名函数来确保每次迭代后都能释放内存
				stats := c.GetStats()
				if err := c.SaveMetrics(stats); err != nil {
					log.Printf("[Metrics] Failed to save metrics: %v", err)
				}
				// 释放内存
				stats = nil
				runtime.GC()
			}()
		}
	}()

	// 注册信号处理，在程序退出前保存一次
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[Metrics] Received shutdown signal, saving metrics...")
		stats := c.GetStats()
		if err := c.SaveMetrics(stats); err != nil {
			log.Printf("[Metrics] Failed to save metrics on shutdown: %v", err)
		}
		os.Exit(0)
	}()
}
