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
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// 优化的状态码统计结构
type StatusCodeStats struct {
	mu    sync.RWMutex
	stats map[int]*int64 // 预分配常见状态码
}

// 优化的延迟分布统计
type LatencyBuckets struct {
	lt10ms     int64
	ms10_50    int64
	ms50_200   int64
	ms200_1000 int64
	gt1s       int64
}

// 优化的引用来源统计（使用分片减少锁竞争）
type RefererStats struct {
	shards []*RefererShard
	mask   uint64
}

type RefererShard struct {
	mu   sync.RWMutex
	data map[string]*models.PathMetrics
}

const (
	refererShardCount = 32 // 分片数量，必须是2的幂
)

func NewRefererStats() *RefererStats {
	rs := &RefererStats{
		shards: make([]*RefererShard, refererShardCount),
		mask:   refererShardCount - 1,
	}
	for i := 0; i < refererShardCount; i++ {
		rs.shards[i] = &RefererShard{
			data: make(map[string]*models.PathMetrics),
		}
	}
	return rs
}

func (rs *RefererStats) hash(key string) uint64 {
	// 简单的字符串哈希函数
	var h uint64 = 14695981039346656037
	for _, b := range []byte(key) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

func (rs *RefererStats) getShard(key string) *RefererShard {
	return rs.shards[rs.hash(key)&rs.mask]
}

func (rs *RefererStats) Load(key string) (*models.PathMetrics, bool) {
	shard := rs.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	val, ok := shard.data[key]
	return val, ok
}

func (rs *RefererStats) Store(key string, value *models.PathMetrics) {
	shard := rs.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	shard.data[key] = value
}

func (rs *RefererStats) Delete(key string) {
	shard := rs.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	delete(shard.data, key)
}

func (rs *RefererStats) Range(f func(key string, value *models.PathMetrics) bool) {
	for _, shard := range rs.shards {
		shard.mu.RLock()
		for k, v := range shard.data {
			if !f(k, v) {
				shard.mu.RUnlock()
				return
			}
		}
		shard.mu.RUnlock()
	}
}

func (rs *RefererStats) Cleanup(cutoff int64) int {
	deleted := 0
	for _, shard := range rs.shards {
		shard.mu.Lock()
		for k, v := range shard.data {
			if v.LastAccessTime.Load() < cutoff {
				delete(shard.data, k)
				deleted++
			}
		}
		shard.mu.Unlock()
	}
	return deleted
}

func NewStatusCodeStats() *StatusCodeStats {
	s := &StatusCodeStats{
		stats: make(map[int]*int64),
	}
	// 预分配常见状态码
	commonCodes := []int{200, 201, 204, 301, 302, 304, 400, 401, 403, 404, 429, 500, 502, 503, 504}
	for _, code := range commonCodes {
		counter := new(int64)
		s.stats[code] = counter
	}
	return s
}

func (s *StatusCodeStats) Increment(code int) {
	s.mu.RLock()
	if counter, exists := s.stats[code]; exists {
		s.mu.RUnlock()
		atomic.AddInt64(counter, 1)
		return
	}
	s.mu.RUnlock()

	// 需要创建新的计数器
	s.mu.Lock()
	defer s.mu.Unlock()
	if counter, exists := s.stats[code]; exists {
		atomic.AddInt64(counter, 1)
	} else {
		counter := new(int64)
		*counter = 1
		s.stats[code] = counter
	}
}

func (s *StatusCodeStats) GetStats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]int64)
	for code, counter := range s.stats {
		result[fmt.Sprintf("%d", code)] = atomic.LoadInt64(counter)
	}
	return result
}

// Collector 指标收集器
type Collector struct {
	startTime       time.Time
	activeRequests  int64
	totalBytes      int64
	latencySum      int64
	maxLatency      int64 // 最大响应时间
	minLatency      int64 // 最小响应时间
	statusCodeStats *StatusCodeStats
	latencyBuckets  *LatencyBuckets // 使用结构体替代 sync.Map
	refererStats    *RefererStats   // 使用分片哈希表
	pathStats       *RefererStats   // 路径统计（复用RefererStats的分片结构）
	bandwidthStats  struct {
		sync.RWMutex
		window     time.Duration
		lastUpdate time.Time
		current    int64
		history    map[string]int64
	}
	recentRequests *models.RequestQueue
	config         *config.Config

	// 新增：当前会话统计
	sessionRequests int64 // 当前会话的请求数（不包含历史数据）

	// 新增：基于时间窗口的请求统计
	requestsWindow struct {
		sync.RWMutex
		window     time.Duration // 时间窗口大小（5分钟）
		buckets    []int64       // 时间桶，每个桶统计10秒内的请求数
		bucketSize time.Duration // 每个桶的时间长度（10秒）
		lastUpdate time.Time     // 最后更新时间
		current    int64         // 当前桶的请求数
	}
}

type RequestMetric struct {
	Path       string
	Status     int
	Latency    time.Duration
	Bytes      int64
	ClientIP   string
	Request    *http.Request
	CacheHit   bool  // 是否缓存命中
	BytesSaved int64 // 通过缓存节省的字节数
}

var requestChan chan RequestMetric

var (
	instance *Collector
	once     sync.Once
)

// InitCollector 初始化收集器
func InitCollector(cfg *config.Config) error {
	once.Do(func() {
		instance = &Collector{
			startTime:       time.Now(),
			recentRequests:  models.NewRequestQueue(100),
			config:          cfg,
			minLatency:      math.MaxInt64,
			statusCodeStats: NewStatusCodeStats(),
			latencyBuckets:  &LatencyBuckets{},
			refererStats:    NewRefererStats(),
			pathStats:       NewRefererStats(), // 初始化路径统计
		}

		// 初始化带宽统计
		instance.bandwidthStats.window = time.Minute
		instance.bandwidthStats.lastUpdate = time.Now()
		instance.bandwidthStats.history = make(map[string]int64)

		// 初始化请求窗口统计（5分钟窗口，10秒一个桶，共30个桶）
		instance.requestsWindow.window = 5 * time.Minute
		instance.requestsWindow.bucketSize = 10 * time.Second
		bucketCount := int(instance.requestsWindow.window / instance.requestsWindow.bucketSize)
		instance.requestsWindow.buckets = make([]int64, bucketCount)
		instance.requestsWindow.lastUpdate = time.Now()

		// 初始化延迟分布桶
		buckets := []string{"lt10ms", "10-50ms", "50-200ms", "200-1000ms", "gt1s"}
		for _, bucket := range buckets {
			counter := new(int64)
			*counter = 0
			// 根据 bucket 名称设置对应的桶计数器
			switch bucket {
			case "lt10ms":
				instance.latencyBuckets.lt10ms = atomic.LoadInt64(counter)
			case "10-50ms":
				instance.latencyBuckets.ms10_50 = atomic.LoadInt64(counter)
			case "50-200ms":
				instance.latencyBuckets.ms50_200 = atomic.LoadInt64(counter)
			case "200-1000ms":
				instance.latencyBuckets.ms200_1000 = atomic.LoadInt64(counter)
			case "gt1s":
				instance.latencyBuckets.gt1s = atomic.LoadInt64(counter)
			}
		}

		// 初始化异步指标收集通道
		requestChan = make(chan RequestMetric, 10000)
		instance.startAsyncMetricsUpdater()

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

// RecordRequest 记录请求（异步写入channel）
func (c *Collector) RecordRequest(path string, status int, latency time.Duration, bytes int64, clientIP string, r *http.Request) {
	metric := RequestMetric{
		Path:     path,
		Status:   status,
		Latency:  latency,
		Bytes:    bytes,
		ClientIP: clientIP,
		Request:  r,
	}
	select {
	case requestChan <- metric:
		// ok
	default:
		// channel 满了，丢弃或降级处理
	}
}

// RecordRequestWithCache 记录带缓存信息的请求（异步写入channel）
func (c *Collector) RecordRequestWithCache(path string, status int, latency time.Duration, bytes int64, clientIP string, r *http.Request, cacheHit bool, bytesSaved int64) {
	metric := RequestMetric{
		Path:       path,
		Status:     status,
		Latency:    latency,
		Bytes:      bytes,
		ClientIP:   clientIP,
		Request:    r,
		CacheHit:   cacheHit,
		BytesSaved: bytesSaved,
	}
	select {
	case requestChan <- metric:
		// ok
	default:
		// channel 满了，丢弃或降级处理
	}
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
	var totalErrors int64
	statusCodeStats := c.statusCodeStats.GetStats()
	for statusCode, count := range statusCodeStats {
		totalRequests += count
		// 计算错误数（4xx和5xx状态码）
		if code, err := strconv.Atoi(statusCode); err == nil && code >= 400 {
			totalErrors += count
		}
	}

	avgLatency := float64(0)
	if totalRequests > 0 {
		avgLatency = float64(atomic.LoadInt64(&c.latencySum)) / float64(totalRequests)
	}

	// 计算错误率
	errorRate := float64(0)
	if totalRequests > 0 {
		errorRate = float64(totalErrors) / float64(totalRequests)
	}

	// 计算当前会话的请求数（基于本次启动后的实际请求）
	sessionRequests := atomic.LoadInt64(&c.sessionRequests)

	// 计算最近5分钟的平均每秒请求数
	requestsPerSecond := c.getRecentRequestsPerSecond()

	// 收集状态码统计（已经在上面获取了）

	// 收集引用来源统计
	var refererMetrics []*models.PathMetrics
	refererCount := 0
	c.refererStats.Range(func(key string, value *models.PathMetrics) bool {
		stats := value
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
	latencyDistribution["lt10ms"] = atomic.LoadInt64(&c.latencyBuckets.lt10ms)
	latencyDistribution["10-50ms"] = atomic.LoadInt64(&c.latencyBuckets.ms10_50)
	latencyDistribution["50-200ms"] = atomic.LoadInt64(&c.latencyBuckets.ms50_200)
	latencyDistribution["200-1000ms"] = atomic.LoadInt64(&c.latencyBuckets.ms200_1000)
	latencyDistribution["gt1s"] = atomic.LoadInt64(&c.latencyBuckets.gt1s)

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
		"total_requests":      totalRequests,
		"total_errors":        totalErrors,
		"error_rate":          errorRate,
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
		"bandwidth_history":        bandwidthHistory,
		"current_bandwidth":        utils.FormatBytes(int64(c.getCurrentBandwidth())) + "/s",
		"current_session_requests": sessionRequests,
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
	statusStats := c.statusCodeStats.GetStats()
	for _, count := range statusStats {
		if count < 0 {
			return fmt.Errorf("invalid negative status code count")
		}
		statusCodeTotal += count
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

// updateRequestsWindow 更新请求窗口统计
func (c *Collector) updateRequestsWindow(count int64) {
	c.requestsWindow.Lock()
	defer c.requestsWindow.Unlock()

	now := time.Now()

	// 如果是第一次调用，初始化时间
	if c.requestsWindow.lastUpdate.IsZero() {
		c.requestsWindow.lastUpdate = now
	}

	// 计算当前时间桶的索引
	timeSinceLastUpdate := now.Sub(c.requestsWindow.lastUpdate)

	// 如果时间跨度超过桶大小，需要移动到新桶
	if timeSinceLastUpdate >= c.requestsWindow.bucketSize {
		bucketsToMove := int(timeSinceLastUpdate / c.requestsWindow.bucketSize)

		if bucketsToMove >= len(c.requestsWindow.buckets) {
			// 如果移动的桶数超过总桶数，清空所有桶
			for i := range c.requestsWindow.buckets {
				c.requestsWindow.buckets[i] = 0
			}
		} else {
			// 向右移动桶数据（新数据在索引0）
			copy(c.requestsWindow.buckets[bucketsToMove:], c.requestsWindow.buckets[:len(c.requestsWindow.buckets)-bucketsToMove])
			// 清空前面的桶
			for i := 0; i < bucketsToMove; i++ {
				c.requestsWindow.buckets[i] = 0
			}
		}

		// 更新时间为当前桶的开始时间
		c.requestsWindow.lastUpdate = now.Truncate(c.requestsWindow.bucketSize)
	}

	// 将请求数加到第一个桶（当前时间桶）
	if len(c.requestsWindow.buckets) > 0 {
		c.requestsWindow.buckets[0] += count
	}
}

// getRecentRequestsPerSecond 获取最近5分钟的平均每秒请求数
func (c *Collector) getRecentRequestsPerSecond() float64 {
	c.requestsWindow.RLock()
	defer c.requestsWindow.RUnlock()

	// 统计所有桶的总请求数
	var totalRequests int64
	for _, bucket := range c.requestsWindow.buckets {
		totalRequests += bucket
	}

	// 计算实际的时间窗口（可能不满5分钟）
	now := time.Now()
	actualWindow := c.requestsWindow.window

	// 如果程序运行时间不足5分钟，使用实际运行时间
	if runTime := now.Sub(c.startTime); runTime < c.requestsWindow.window {
		actualWindow = runTime
	}

	if actualWindow.Seconds() == 0 {
		return 0
	}

	return float64(totalRequests) / actualWindow.Seconds()
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

// startCleanupTask 启动定期清理任务
func (c *Collector) startCleanupTask() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			<-ticker.C
			oneDayAgo := time.Now().Add(-24 * time.Hour).Unix()

			// 清理超过24小时的引用来源统计
			deletedCount := c.refererStats.Cleanup(oneDayAgo)

			if deletedCount > 0 {
				log.Printf("[Collector] 已清理 %d 条过期的引用来源统计", deletedCount)
			}

			// 强制GC
			runtime.GC()
		}
	}()
}

// 异步批量处理请求指标
func (c *Collector) startAsyncMetricsUpdater() {
	go func() {
		batch := make([]RequestMetric, 0, 1000)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case metric := <-requestChan:
				batch = append(batch, metric)
				if len(batch) >= 1000 {
					c.updateMetricsBatch(batch)
					batch = batch[:0]
				}
			case <-ticker.C:
				if len(batch) > 0 {
					c.updateMetricsBatch(batch)
					batch = batch[:0]
				}
			}
		}
	}()
}

// 批量更新指标
func (c *Collector) updateMetricsBatch(batch []RequestMetric) {
	for _, m := range batch {
		// 增加当前会话请求计数
		atomic.AddInt64(&c.sessionRequests, 1)

		// 更新请求窗口统计
		c.updateRequestsWindow(1)

		// 更新状态码统计
		c.statusCodeStats.Increment(m.Status)

		// 更新总字节数和带宽统计
		atomic.AddInt64(&c.totalBytes, m.Bytes)
		c.updateBandwidthStats(m.Bytes)

		// 更新延迟统计
		atomic.AddInt64(&c.latencySum, int64(m.Latency))
		latencyNanos := int64(m.Latency)
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
		latencyMs := m.Latency.Milliseconds()
		switch {
		case latencyMs < 10:
			atomic.AddInt64(&c.latencyBuckets.lt10ms, 1)
		case latencyMs < 50:
			atomic.AddInt64(&c.latencyBuckets.ms10_50, 1)
		case latencyMs < 200:
			atomic.AddInt64(&c.latencyBuckets.ms50_200, 1)
		case latencyMs < 1000:
			atomic.AddInt64(&c.latencyBuckets.ms200_1000, 1)
		default:
			atomic.AddInt64(&c.latencyBuckets.gt1s, 1)
		}

		// 记录路径统计
		var pathMetrics *models.PathMetrics
		if existingMetrics, ok := c.pathStats.Load(m.Path); ok {
			pathMetrics = existingMetrics
		} else {
			pathMetrics = &models.PathMetrics{Path: m.Path}
			c.pathStats.Store(m.Path, pathMetrics)
		}

		pathMetrics.AddRequest()
		if m.Status >= 400 {
			pathMetrics.AddError()
		}
		pathMetrics.AddBytes(m.Bytes)
		pathMetrics.AddLatency(m.Latency.Nanoseconds())
		pathMetrics.LastAccessTime.Store(time.Now().Unix())

		// 更新状态码统计
		switch {
		case m.Status >= 200 && m.Status < 300:
			pathMetrics.Status2xx.Add(1)
		case m.Status >= 300 && m.Status < 400:
			pathMetrics.Status3xx.Add(1)
		case m.Status >= 400 && m.Status < 500:
			pathMetrics.Status4xx.Add(1)
		case m.Status >= 500:
			pathMetrics.Status5xx.Add(1)
		}

		// 更新缓存统计
		if m.CacheHit {
			pathMetrics.CacheHits.Add(1)
			pathMetrics.BytesSaved.Add(m.BytesSaved)
		} else {
			pathMetrics.CacheMisses.Add(1)
		}

		// 记录引用来源
		if m.Request != nil {
			referer := m.Request.Referer()
			if referer != "" {
				var refererMetrics *models.PathMetrics
				if existingMetrics, ok := c.refererStats.Load(referer); ok {
					refererMetrics = existingMetrics
				} else {
					refererMetrics = &models.PathMetrics{Path: referer}
					c.refererStats.Store(referer, refererMetrics)
				}

				refererMetrics.AddRequest()
				if m.Status >= 400 {
					refererMetrics.AddError()
				}
				refererMetrics.AddBytes(m.Bytes)
				refererMetrics.AddLatency(m.Latency.Nanoseconds())
				// 更新最后访问时间
				refererMetrics.LastAccessTime.Store(time.Now().Unix())
			}
		}

		// 更新最近请求记录
		c.recentRequests.Push(models.RequestLog{
			Time:      time.Now(),
			Path:      m.Path,
			Status:    m.Status,
			Latency:   int64(m.Latency),
			BytesSent: m.Bytes,
			ClientIP:  m.ClientIP,
		})
	}
}

// RecordStatusCodeBatch 批量记录状态码（用于同步）
func (c *Collector) RecordStatusCodeBatch(code int, count int64) {
	c.statusCodeStats.mu.RLock()
	if counter, exists := c.statusCodeStats.stats[code]; exists {
		c.statusCodeStats.mu.RUnlock()
		atomic.AddInt64(counter, count)
		return
	}
	c.statusCodeStats.mu.RUnlock()

	// 需要创建新的计数器
	c.statusCodeStats.mu.Lock()
	defer c.statusCodeStats.mu.Unlock()
	if counter, exists := c.statusCodeStats.stats[code]; exists {
		atomic.AddInt64(counter, count)
	} else {
		counter := new(int64)
		*counter = count
		c.statusCodeStats.stats[code] = counter
	}
}

// GetPathStats 获取所有路径的统计信息
func (c *Collector) GetPathStats() []models.PathMetricsJSON {
	var pathMetrics []*models.PathMetrics
	c.pathStats.Range(func(key string, value *models.PathMetrics) bool {
		stats := value
		requestCount := stats.GetRequestCount()
		if requestCount > 0 {
			totalLatency := stats.GetTotalLatency()
			avgLatencyMs := float64(totalLatency) / float64(requestCount) / float64(time.Millisecond)
			stats.AvgLatency = fmt.Sprintf("%.2fms", avgLatencyMs)
			pathMetrics = append(pathMetrics, stats)
		}
		return true
	})

	// 按请求数降序排序
	sort.Slice(pathMetrics, func(i, j int) bool {
		countI := pathMetrics[i].GetRequestCount()
		countJ := pathMetrics[j].GetRequestCount()
		if countI != countJ {
			return countI > countJ
		}
		return pathMetrics[i].Path < pathMetrics[j].Path
	})

	// 转换为 JSON 格式
	result := make([]models.PathMetricsJSON, len(pathMetrics))
	for i, metric := range pathMetrics {
		result[i] = metric.ToJSON()
	}

	return result
}

// GetPathStatByPath 获取指定路径的统计信息
func (c *Collector) GetPathStatByPath(path string) *models.PathMetricsJSON {
	if stats, ok := c.pathStats.Load(path); ok {
		requestCount := stats.GetRequestCount()
		if requestCount > 0 {
			totalLatency := stats.GetTotalLatency()
			avgLatencyMs := float64(totalLatency) / float64(requestCount) / float64(time.Millisecond)
			stats.AvgLatency = fmt.Sprintf("%.2fms", avgLatencyMs)
			result := stats.ToJSON()
			return &result
		}
	}
	return nil
}
