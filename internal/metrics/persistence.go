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

// 上报窗口: 每次同步重传最近这么多的桶
//
// 只有"当前"那个桶会变 (过去的小时 / 天已经冻结), 窗口存在的意义只是给
// **连续同步失败**留补传余量 —— 节点重启后内存缓冲本来就是空的, 不存在补传需求。
// 所以窗口不必覆盖整个保留期: 48 小时 / 40 天的旧值让每次上报都重传上千行,
// 写入行数才是 D1 上最先触顶的配额, 而不是请求数。
const (
	// timeseriesUploadHours 小时桶上报窗口, 容忍约 6 小时的连续同步失败
	timeseriesUploadHours = 6
	// refererUploadDays 天桶上报窗口, 覆盖跨日边界并留一天余量
	refererUploadDays = 2
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

	// 内存计数恢复完成后对齐重置代际: 若本节点在"上报旧值"与"收到 marker"之间
	// 崩溃过, 刚恢复回来的就是重置前的旧值, 这里把它补清掉
	{
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		catchUpStatsResets(ctx)
		cancel()
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

	// 五张表合成一个请求上报; node_id 由 BulkSave 统一盖章
	//
	// 时间序列只传近 timeseriesUploadHours 小时、referer 只传近 refererUploadDays 天:
	// 过去的桶已冻结不会再变, 窗口只为容忍连续同步失败留补传余量
	payload := &sync.BulkPayload{
		PathStats:    ms.getPathStatsArray(),
		Timeseries:   ms.getTimeseriesPoints(),
		RefererDaily: ms.getRefererDailyPoints(),
	}

	now := time.Now().UnixMilli()
	for code, count := range ms.getStatusCodesMap() {
		payload.StatusCodes = append(payload.StatusCodes, sync.StatusCodeMetric{
			StatusCode: code, Count: count, UpdatedAt: now,
		})
	}
	for bucket, count := range ms.getLatencyDistributionMap() {
		payload.Latency = append(payload.Latency, sync.LatencyMetric{
			Bucket: bucket, Count: count, UpdatedAt: now,
		})
	}

	// 上报同时是一次心跳: 上行捎位点游标, 下行捎比游标新的状态 (重置代际 / 配置)
	state, err := sync.BulkSave(ctx, payload, ResetSeen())
	if err != nil {
		log.Printf("[MetricsStorage] 上报指标失败: %v", err)
	} else if state != nil {
		applyStatsResets(ctx, state.Resets)
		sync.ApplyPulseConfig(state)
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

	// 单请求读回本节点自己的累计统计
	//
	// 必须按 node_id 过滤: 拿全节点合计恢复会让本节点把其他节点的量当成自己的起点,
	// 下个周期又写回 D1, 每次重启放大一轮
	bulk, err := sync.BulkLoadOwn(ctx)
	if err != nil {
		log.Printf("[MetricsStorage] 加载本节点指标失败: %v", err)
	} else if bulk != nil {
		loadedCount := 0
		for _, m := range bulk.StatusCodes {
			code, convErr := strconv.Atoi(m.StatusCode)
			if convErr != nil {
				continue
			}
			ms.collector.statusCodeStats.mu.Lock()
			if _, exists := ms.collector.statusCodeStats.stats[code]; !exists {
				ms.collector.statusCodeStats.stats[code] = new(int64)
			}
			atomic.StoreInt64(ms.collector.statusCodeStats.stats[code], m.Count)
			ms.collector.statusCodeStats.mu.Unlock()
			loadedCount++
		}

		for _, m := range bulk.Latency {
			switch m.Bucket {
			case "lt10ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.lt10ms, m.Count)
			case "10-50ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.ms10_50, m.Count)
			case "50-200ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.ms50_200, m.Count)
			case "200-1000ms":
				atomic.StoreInt64(&ms.collector.latencyBuckets.ms200_1000, m.Count)
			case "gt1s":
				atomic.StoreInt64(&ms.collector.latencyBuckets.gt1s, m.Count)
			}
		}

		log.Printf("[MetricsStorage] 已加载本节点 %d 条状态码 / %d 个延迟桶",
			loadedCount, len(bulk.Latency))
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
// 每次同步上报近 timeseriesUploadHours 小时的桶, D1 端按 (path, ts_hour, node_id) UPSERT,
// 等价于本节点对自身贡献的 idempotent 覆盖, 不会双计
func (ms *MetricsStorage) getTimeseriesPoints() []sync.PathTimeseriesPoint {
	ts := ms.collector.PathTimeSeries()
	if ts == nil {
		return nil
	}
	buckets := ts.SnapshotForUpload(time.Now(), timeseriesUploadHours)
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

// maybePruneTimeseries 低频触发旧数据清理
// 同时清理 path_timeseries (31 天前) 与 referer_daily (35 天前, 留余量)
//
// 按"多久清理一次"换算成保存次数, 而不是写死次数: 上报间隔调整时 (30 分钟 → 10 分钟)
// 若仍固定每 24 次, 清理频率会跟着变成 4 小时一次, 白白多打 D1
func (ms *MetricsStorage) maybePruneTimeseries(ctx context.Context) {
	pruneEvery := int64(12 * time.Hour / ms.saveInterval)
	if pruneEvery < 1 {
		pruneEvery = 1
	}

	ms.saveCount++
	if ms.saveCount%pruneEvery != 0 {
		return
	}
	cutoff := time.Now().Unix()/3600 - 31*24
	if deleted, err := sync.PruneTimeseries(ctx, cutoff); err != nil {
		log.Printf("[MetricsStorage] 清理旧时间序列失败: %v", err)
	} else if deleted > 0 {
		log.Printf("[MetricsStorage] 已清理 %d 条 31 天前的时间序列桶", deleted)
	}

	dayCutoff := time.Now().Unix()/86400 - 35
	if deleted, err := sync.PruneRefererDaily(ctx, dayCutoff); err != nil {
		log.Printf("[MetricsStorage] 清理旧 referer 天序列失败: %v", err)
	} else if deleted > 0 {
		log.Printf("[MetricsStorage] 已清理 %d 条 35 天前的 referer 天序列桶", deleted)
	}

	// 重置代际 marker 同窗口清理: 35 天足够覆盖任何离线节点的补应用需求
	// (离线超过窗口的节点, 其 D1 行早已被发起端删除, 恢复时天然是重置后状态)
	resetCutoff := time.Now().Add(-35 * 24 * time.Hour).UnixMilli()
	if err := sync.PruneStatsResets(ctx, resetCutoff); err != nil {
		log.Printf("[MetricsStorage] 清理旧重置代际失败: %v", err)
	}
}

// getRefererDailyPoints 把 RefererDailySeries 当前快照转换为 sync.RefererDailyPoint
// 每次同步上报近 refererUploadDays 天的桶, D1 端按 (host, ts_date, node_id) UPSERT MAX
func (ms *MetricsStorage) getRefererDailyPoints() []sync.RefererDailyPoint {
	rd := ms.collector.RefererDaily()
	if rd == nil {
		return nil
	}
	buckets := rd.SnapshotForUpload(time.Now(), refererUploadDays)
	if len(buckets) == 0 {
		return nil
	}
	nodeID := sync.NodeID()
	now := time.Now().UnixMilli()
	out := make([]sync.RefererDailyPoint, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, sync.RefererDailyPoint{
			Host:      b.Host,
			TsDate:    b.Date,
			NodeID:    nodeID,
			Requests:  b.Requests,
			Bytes:     b.Bytes,
			Errors:    b.Errors,
			UpdatedAt: now,
		})
	}
	return out
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
			BytesSaved:       pm.BytesSaved,
			TotalLatencyNs:   pm.TotalLatency,
			LastAccessTime:   pm.LastAccessTime,
			UpdatedAt:        now,
		})
	}

	return result
}
