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

const (
	refererShardCount = 32 // 分片数量，必须是2的幂
)

// Collector 指标收集器
type Collector struct {
	startTime        time.Time
	activeRequests   int64
	totalBytes       int64
	latencySum       int64
	maxLatency       int64 // 最大响应时间
	minLatency       int64 // 最小响应时间
	statusCodeStats  *StatusCodeStats
	latencyBuckets   *LatencyBuckets     // 使用结构体替代 sync.Map
	refererStats     *RefererStats       // 使用分片哈希表
	pathStats        *RefererStats       // 路径统计（复用RefererStats的分片结构）
	refererDaySeries *RefererDailySeries // referer host 天级时间序列, 供 D1 上报
	bandwidthStats   struct {
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

	// 新增：路径统计持久化存储
	pathStatsStorage *PathStatsStorage

	// 新增：路径维度的小时级时间序列, 供仪表盘趋势图使用
	pathTimeSeries *PathTimeSeries

	// 节点维度 (不分路径) 的分钟 / 小时序列: 流量、回源、命中、下载速度
	nodeSeries *NodeSeries

	// 异步通道丢弃事件计数（channel 满时累加）
	droppedMetrics int64

	// 优雅停止信号与等待组
	stopOnce sync.Once
	stopChan chan struct{}
	stopWG   sync.WaitGroup
}

type RequestMetric struct {
	FullPath    string // 完整路径,用于最近请求显示
	StatsPrefix string // 路径前缀,用于路径统计聚合
	Status      int
	Latency     time.Duration
	Bytes       int64
	ClientIP    string
	Request     *http.Request
	CacheHit    bool  // 是否缓存命中
	BytesSaved  int64 // 通过缓存节省的字节数
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
			startTime:        time.Now(),
			recentRequests:   models.NewRequestQueue(100),
			config:           cfg,
			minLatency:       math.MaxInt64,
			statusCodeStats:  NewStatusCodeStats(),
			latencyBuckets:   &LatencyBuckets{},
			refererStats:     NewRefererStats(),
			pathStats:        NewRefererStats(), // 初始化路径统计
			refererDaySeries: NewRefererDailySeries(),
			pathStatsStorage: NewPathStatsStorage("data/path_stats.json"),
			pathTimeSeries:   NewPathTimeSeries(),
			nodeSeries:       NewNodeSeries(),
			stopChan:         make(chan struct{}),
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

		// latencyBuckets 各字段零值即初始状态, 无需额外初始化

		// 加载持久化的路径统计数据
		if err := instance.loadPathStats(); err != nil {
			log.Printf("[Collector] 加载路径统计数据失败: %v", err)
		}

		// 初始化异步指标收集通道
		requestChan = make(chan RequestMetric, 10000)
		instance.startAsyncMetricsUpdater()

		// 启动数据一致性检查器
		instance.startConsistencyChecker()

		// 启动定期清理任务
		instance.startCleanupTask()

		// 启动定期持久化任务
		instance.startPersistenceTask()
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
// fullPath: 完整请求路径,用于最近请求显示
// statsPrefix: 路径前缀,用于路径统计聚合
func (c *Collector) RecordRequest(fullPath, statsPrefix string, status int, latency time.Duration, bytes int64, clientIP string, r *http.Request) {
	metric := RequestMetric{
		FullPath:    fullPath,
		StatsPrefix: statsPrefix,
		Status:      status,
		Latency:     latency,
		Bytes:       bytes,
		ClientIP:    clientIP,
		Request:     r,
	}
	select {
	case requestChan <- metric:
		// ok
	default:
		c.recordDrop()
	}
}

// RecordRequestWithCache 记录带缓存信息的请求（异步写入channel）
// fullPath: 完整请求路径,用于最近请求显示
// statsPrefix: 路径前缀,用于路径统计聚合
func (c *Collector) RecordRequestWithCache(fullPath, statsPrefix string, status int, latency time.Duration, bytes int64, clientIP string, r *http.Request, cacheHit bool, bytesSaved int64) {
	metric := RequestMetric{
		FullPath:    fullPath,
		StatsPrefix: statsPrefix,
		Status:      status,
		Latency:     latency,
		Bytes:       bytes,
		ClientIP:    clientIP,
		Request:     r,
		CacheHit:    cacheHit,
		BytesSaved:  bytesSaved,
	}
	select {
	case requestChan <- metric:
		// ok
	default:
		c.recordDrop()
	}
}

// recordDrop 记录一次指标事件丢弃，并按水位触发告警日志
func (c *Collector) recordDrop() {
	dropped := atomic.AddInt64(&c.droppedMetrics, 1)
	// 每 1000 次丢弃打一次警告，避免日志风暴
	if dropped%1000 == 0 {
		log.Printf("[Metrics] WARN 指标通道丢弃累计 %d 次，channel 已饱和（容量=%d, 当前=%d）",
			dropped, cap(requestChan), len(requestChan))
	}
}

// Stop 通知所有后台 goroutine 退出，并等待其完成（最长 wait 超时）
func (c *Collector) Stop(wait time.Duration) {
	c.stopOnce.Do(func() {
		close(c.stopChan)
	})
	done := make(chan struct{})
	go func() {
		c.stopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.Printf("[Metrics] all background tasks stopped")
	case <-time.After(wait):
		log.Printf("[Metrics] WARN background tasks did not stop within %s", wait)
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
		// 纳秒累计原值, 供跨节点合并时重算加权平均延迟 (平均值本身不能相加)
		"latency_sum": atomic.LoadInt64(&c.latencySum),
		"latency_stats": map[string]interface{}{
			"min":          fmt.Sprintf("%.2fms", float64(minLatency)/float64(time.Millisecond)),
			"max":          fmt.Sprintf("%.2fms", float64(maxLatency)/float64(time.Millisecond)),
			"distribution": latencyDistribution,
		},
		"bandwidth_history":        bandwidthHistory,
		"current_bandwidth":        utils.FormatBytes(int64(c.getCurrentBandwidth())) + "/s",
		"current_session_requests": sessionRequests,
		"dropped_metrics":          atomic.LoadInt64(&c.droppedMetrics),
		"metrics_chan_capacity":    cap(requestChan),
		"metrics_chan_pending":     len(requestChan),
	}
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

// 添加定期检查数据一致性的功能
func (c *Collector) startConsistencyChecker() {
	c.stopWG.Add(1)
	go func() {
		defer c.stopWG.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Metrics] consistency checker panic: %v", r)
			}
		}()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := c.validateLoadedData(); err != nil {
					log.Printf("[Metrics] Data consistency check failed: %v", err)
				}
			case <-c.stopChan:
				return
			}
		}
	}()
}

// bandwidthHistoryKeyFmt key 采用 "YYYY-MM-DD HH:MM"，保证字典序 == 时间序，不会跨年冲突
const bandwidthHistoryKeyFmt = "2006-01-02 15:04"

// startCleanupTask 启动定期清理任务
func (c *Collector) startCleanupTask() {
	c.stopWG.Add(1)
	go func() {
		defer c.stopWG.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Metrics] cleanup task panic: %v", r)
			}
		}()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				oneDayAgo := time.Now().Add(-24 * time.Hour).Unix()
				deletedCount := c.refererStats.Cleanup(oneDayAgo)
				if deletedCount > 0 {
					log.Printf("[Collector] 已清理 %d 条过期的引用来源统计", deletedCount)
				}
				runtime.GC()
			case <-c.stopChan:
				return
			}
		}
	}()
}

// 异步批量处理请求指标
func (c *Collector) startAsyncMetricsUpdater() {
	c.stopWG.Add(1)
	go func() {
		defer c.stopWG.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Metrics] async updater panic: %v", r)
			}
		}()
		batch := make([]RequestMetric, 0, 1000)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		flush := func() {
			if len(batch) > 0 {
				c.updateMetricsBatch(batch)
				batch = batch[:0]
			}
		}
		for {
			select {
			case metric := <-requestChan:
				batch = append(batch, metric)
				if len(batch) >= 1000 {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-c.stopChan:
				// 退出前排空 channel 中残留指标，避免数据丢失
				for {
					select {
					case metric := <-requestChan:
						batch = append(batch, metric)
					default:
						flush()
						return
					}
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

		// 记录路径维度的小时级时间序列 (按前缀聚合, 与 pathStats 保持一致)
		c.pathTimeSeries.Record(m.StatsPrefix, m.Bytes, m.Status >= 400, time.Now())
		c.nodeSeries.record(time.Now(), nodeSample{
			bytes:     m.Bytes,
			latencyNs: m.Latency.Nanoseconds(),
			isError:   m.Status >= 400,
			cacheHit:  m.CacheHit,
		})

		// 记录路径统计(使用路径前缀)
		var pathMetrics *models.PathMetrics
		if existingMetrics, ok := c.pathStats.Load(m.StatsPrefix); ok {
			pathMetrics = existingMetrics
		} else {
			pathMetrics = &models.PathMetrics{Path: m.StatsPrefix}
			c.pathStats.Store(m.StatsPrefix, pathMetrics)
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

		// 记录引用来源 (URL 维度 24h 累计 + host 维度天级时间序列)
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

				// 同时按 host 维度落入天级桶, 供 D1 上报近 30 天数据
				if host := extractRefererHost(referer); host != "" {
					c.refererDaySeries.Record(host, m.Bytes, m.Status >= 400, time.Now())
				}
			}
		}

		// 更新最近请求记录(使用完整路径), Referer 仅在请求头存在时填入
		var referer, host string
		if m.Request != nil {
			referer = m.Request.Header.Get("Referer")
			host = m.Request.Host
		}
		c.recentRequests.Push(models.RequestLog{
			Time:      time.Now(),
			Host:      host,
			Path:      m.FullPath,
			Status:    m.Status,
			Latency:   int64(m.Latency),
			BytesSent: m.Bytes,
			ClientIP:  m.ClientIP,
			Referer:   referer,
		})
	}
}

// PathTimeSeries 返回路径时间序列容器, 供同步 / 查询模块使用
func (c *Collector) PathTimeSeries() *PathTimeSeries {
	return c.pathTimeSeries
}

// NodeSeries 返回节点维度序列, 供实时快照与 D1 上报使用
func (c *Collector) NodeSeries() *NodeSeries {
	return c.nodeSeries
}

// RefererDaily 返回引用来源 host 天级时间序列容器, 供同步 / 查询模块使用
func (c *Collector) RefererDaily() *RefererDailySeries {
	return c.refererDaySeries
}
