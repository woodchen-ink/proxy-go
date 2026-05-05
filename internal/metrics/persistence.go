package metrics

import (
	"context"
	"log"
	"proxy-go/internal/utils"
	"proxy-go/pkg/sync"
	"runtime"
	"strconv"
	gosync "sync"
	"sync/atomic"
	"time"
)

// MetricsStorage 指标存储结构（D1 模式）
type MetricsStorage struct {
	collector    *Collector
	saveInterval time.Duration
	stopChan     chan struct{}
	wg           gosync.WaitGroup
	lastSaveTime time.Time
	mutex        gosync.RWMutex
	saveCount    int64 // 已执行保存次数, 用于按频率触发清理任务
}

// NewMetricsStorage 创建新的指标存储
func NewMetricsStorage(collector *Collector, dataDir string, saveInterval time.Duration) *MetricsStorage {
	if saveInterval < time.Minute {
		saveInterval = time.Minute
	}

	return &MetricsStorage{
		collector:    collector,
		saveInterval: saveInterval,
		stopChan:     make(chan struct{}),
	}
}

// Start 启动定时保存任务
func (ms *MetricsStorage) Start() error {
	// 尝试从 D1 加载现有数据
	if err := ms.LoadMetrics(); err != nil {
		log.Printf("[MetricsStorage] 加载指标数据失败: %v", err)
		// 加载失败不影响启动
	}

	ms.wg.Add(1)
	go ms.runSaveTask()
	log.Printf("[MetricsStorage] 指标存储服务已启动，保存间隔: %v", ms.saveInterval)
	return nil
}

// Stop 停止定时保存任务
func (ms *MetricsStorage) Stop() {
	close(ms.stopChan)
	ms.wg.Wait()

	// 在停止前保存一次数据
	if err := ms.SaveMetrics(); err != nil {
		log.Printf("[MetricsStorage] 停止时保存指标数据失败: %v", err)
	}

	log.Printf("[MetricsStorage] 指标存储服务已停止")
}

// runSaveTask 运行定时保存任务
func (ms *MetricsStorage) runSaveTask() {
	defer ms.wg.Done()

	ticker := time.NewTicker(ms.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := ms.SaveMetrics(); err != nil {
				log.Printf("[MetricsStorage] 保存指标数据失败: %v", err)
			}
		case <-ms.stopChan:
			return
		}
	}
}

// SaveMetrics 保存指标数据到 D1
func (ms *MetricsStorage) SaveMetrics() error {
	if !sync.IsEnabled() {
		return nil // D1 未启用，跳过保存
	}

	start := time.Now()
	log.Printf("[MetricsStorage] 开始保存指标数据到 D1...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 获取状态码统计
	statusCodes := ms.getStatusCodesMap()
	if len(statusCodes) > 0 {
		if err := sync.SaveStatusCodes(ctx, statusCodes); err != nil {
			log.Printf("[MetricsStorage] 保存状态码统计失败: %v", err)
		}
	}

	// 获取延迟分布
	latencyDist := ms.getLatencyDistributionMap()
	if len(latencyDist) > 0 {
		if err := sync.SaveLatencyDistribution(ctx, latencyDist); err != nil {
			log.Printf("[MetricsStorage] 保存延迟分布失败: %v", err)
		}
	}

	// 获取路径统计
	pathStats := ms.getPathStatsArray()
	if len(pathStats) > 0 {
		if err := sync.SavePathStats(ctx, pathStats); err != nil {
			log.Printf("[MetricsStorage] 保存路径统计失败: %v", err)
		}
	}

	// 上报路径时间序列 (近 48 小时, 跨节点聚合由 D1 完成)
	tsPoints := ms.getTimeseriesPoints()
	if len(tsPoints) > 0 {
		if err := sync.SavePathTimeseries(ctx, tsPoints); err != nil {
			log.Printf("[MetricsStorage] 保存路径时间序列失败: %v", err)
		}
	}

	// 清理 31 天之前的旧时间序列 (低频, 每 24 次保存触发一次)
	ms.maybePruneTimeseries(ctx)

	// 更新最后保存时间
	ms.mutex.Lock()
	ms.lastSaveTime = time.Now()
	ms.mutex.Unlock()

	// 强制进行一次GC
	runtime.GC()

	// 打印内存使用情况
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	log.Printf("[MetricsStorage] 指标数据保存完成，耗时: %v, 内存使用: %s",
		time.Since(start), utils.FormatBytes(int64(mem.Alloc)))
	return nil
}

// LoadMetrics 从 D1 加载指标数据
func (ms *MetricsStorage) LoadMetrics() error {
	if !sync.IsEnabled() {
		return nil // D1 未启用，跳过加载
	}

	start := time.Now()
	log.Printf("[MetricsStorage] 开始从 D1 加载指标数据...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 加载状态码统计
	statusCodes, err := sync.LoadStatusCodes(ctx)
	if err != nil {
		log.Printf("[MetricsStorage] 加载状态码统计失败: %v", err)
	} else if len(statusCodes) > 0 {
		loadedCount := 0
		for codeStr, count := range statusCodes {
			if code, err := strconv.Atoi(codeStr); err == nil {
				ms.collector.statusCodeStats.mu.Lock()
				if _, exists := ms.collector.statusCodeStats.stats[code]; !exists {
					ms.collector.statusCodeStats.stats[code] = new(int64)
				}
				atomic.StoreInt64(ms.collector.statusCodeStats.stats[code], count)
				ms.collector.statusCodeStats.mu.Unlock()
				loadedCount++
			}
		}
		log.Printf("[MetricsStorage] 成功加载了 %d 条状态码统计", loadedCount)
	}

	// 加载延迟分布
	latencyDist, err := sync.LoadLatencyDistribution(ctx)
	if err != nil {
		log.Printf("[MetricsStorage] 加载延迟分布失败: %v", err)
	} else if len(latencyDist) > 0 {
		for bucket, count := range latencyDist {
			switch bucket {
			case "lt10ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.lt10ms, count)
			case "10-50ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.ms10_50, count)
			case "50-200ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.ms50_200, count)
			case "200-1000ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.ms200_1000, count)
			case "gt1s":
				atomic.StoreInt64(&ms.collector.latencyBuckets.gt1s, count)
			}
		}
		log.Printf("[MetricsStorage] 加载了延迟分布数据")
	}

	// 强制进行一次GC
	runtime.GC()

	// 打印内存使用情况
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	log.Printf("[MetricsStorage] 指标数据加载完成，耗时: %v, 内存使用: %s",
		time.Since(start), utils.FormatBytes(int64(mem.Alloc)))
	return nil
}

// GetLastSaveTime 获取最后保存时间
func (ms *MetricsStorage) GetLastSaveTime() time.Time {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.lastSaveTime
}

// getStatusCodesMap 获取状态码统计 map
func (ms *MetricsStorage) getStatusCodesMap() map[string]int64 {
	result := make(map[string]int64)

	ms.collector.statusCodeStats.mu.RLock()
	defer ms.collector.statusCodeStats.mu.RUnlock()

	for code, countPtr := range ms.collector.statusCodeStats.stats {
		if countPtr != nil {
			count := atomic.LoadInt64(countPtr)
			if count > 0 {
				result[strconv.Itoa(code)] = count
			}
		}
	}

	return result
}

// getLatencyDistributionMap 获取延迟分布 map
func (ms *MetricsStorage) getLatencyDistributionMap() map[string]int64 {
	return map[string]int64{
		"lt10ms":     atomic.LoadInt64(&ms.collector.latencyBuckets.lt10ms),
		"10-50ms":    atomic.LoadInt64(&ms.collector.latencyBuckets.ms10_50),
		"50-200ms":   atomic.LoadInt64(&ms.collector.latencyBuckets.ms50_200),
		"200-1000ms": atomic.LoadInt64(&ms.collector.latencyBuckets.ms200_1000),
		"gt1s":       atomic.LoadInt64(&ms.collector.latencyBuckets.gt1s),
	}
}

// getTimeseriesPoints 把 PathTimeSeries 当前快照转换为 sync.PathTimeseriesPoint
//
// 每次同步上报近 48 小时的全部桶, D1 端按 (path, ts_hour, node_id) UPSERT,
// 等价于本节点对自身贡献的 idempotent 覆盖, 不会双计
func (ms *MetricsStorage) getTimeseriesPoints() []sync.PathTimeseriesPoint {
	ts := ms.collector.PathTimeSeries()
	if ts == nil {
		return nil
	}
	buckets := ts.SnapshotForUpload(time.Now(), 48)
	if len(buckets) == 0 {
		return nil
	}
	nodeID := sync.NodeID()
	now := time.Now().UnixMilli()
	out := make([]sync.PathTimeseriesPoint, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, sync.PathTimeseriesPoint{
			Path:      b.Path,
			TsHour:    b.Hour,
			NodeID:    nodeID,
			Requests:  b.Requests,
			Bytes:     b.Bytes,
			Errors:    b.Errors,
			UpdatedAt: now,
		})
	}
	return out
}

// maybePruneTimeseries 每 N 次保存触发一次旧数据清理 (默认每 24 次, 即每 12 小时左右)
func (ms *MetricsStorage) maybePruneTimeseries(ctx context.Context) {
	ms.saveCount++
	if ms.saveCount%24 != 0 {
		return
	}
	cutoff := time.Now().Unix()/3600 - 31*24
	if deleted, err := sync.PruneTimeseries(ctx, cutoff); err != nil {
		log.Printf("[MetricsStorage] 清理旧时间序列失败: %v", err)
	} else if deleted > 0 {
		log.Printf("[MetricsStorage] 已清理 %d 条 31 天前的时间序列桶", deleted)
	}
}

// getPathStatsArray 获取路径统计数组（转换为 sync.PathStat 格式）
func (ms *MetricsStorage) getPathStatsArray() []sync.PathStat {
	pathMetrics := ms.collector.GetPathStats()
	if len(pathMetrics) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()
	result := make([]sync.PathStat, 0, len(pathMetrics))

	for _, pm := range pathMetrics {
		result = append(result, sync.PathStat{
			Path:             pm.Path,
			RequestCount:     pm.RequestCount,
			ErrorCount:       pm.ErrorCount,
			BytesTransferred: pm.BytesTransferred,
			Status2xx:        pm.Status2xx,
			Status3xx:        pm.Status3xx,
			Status4xx:        pm.Status4xx,
			Status5xx:        pm.Status5xx,
			CacheHits:        pm.CacheHits,
			CacheMisses:      pm.CacheMisses,
			CacheHitRate:     pm.CacheHitRate,
			BytesSaved:       pm.BytesSaved,
			AvgLatency:       pm.AvgLatency,
			LastAccessTime:   pm.LastAccessTime,
			UpdatedAt:        now,
		})
	}

	return result
}
