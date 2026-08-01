package sync

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

var (
	globalSyncService *SyncService
	globalSyncMutex   sync.RWMutex

	// 配置同步防抖：3 秒窗口内的多次调用合并为一次
	configSyncDebounceMu sync.Mutex
	configSyncTimer      *time.Timer
	configSyncPending    bool
)

const configSyncDebounceWindow = 3 * time.Second

// SyncService 同步服务
type SyncService struct {
	manager       *D1Manager
	configAdapter *ConfigAdapter
	isEnabled     bool
	mutex         sync.RWMutex
}

// InitSyncService 初始化同步服务
func InitSyncService() error {
	globalSyncMutex.Lock()
	defer globalSyncMutex.Unlock()

	// 检查是否启用同步
	if !IsEnabled() {
		log.Printf("[Sync] Sync service disabled (no D1 config)")
		return nil
	}

	// 创建适配器
	configAdapter := NewConfigAdapter("data/config.json")

	log.Printf("[Sync] Initializing D1 sync service...")

	// 创建D1配置
	d1Config, err := NewD1ConfigFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create D1 config: %w", err)
	}

	// 创建D1客户端
	d1Client := NewD1Client(d1Config.Endpoint, d1Config.Token)

	// 创建D1管理器
	manager := NewD1Manager(d1Client, configAdapter)

	// 创建同步服务
	globalSyncService = &SyncService{
		manager:       manager,
		configAdapter: configAdapter,
		isEnabled:     true,
	}

	log.Printf("[Sync] D1 sync service initialized (endpoint: %s)", d1Config.Endpoint)

	// 启动期由 Go 推 schema 到 worker, 已应用的自动跳过
	// 失败不阻塞启动 (D1 临时不可达时主服务仍能跑, 下次重启再尝试)
	migCtx, migCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer migCancel()
	if mr, err := runMigrations(migCtx, d1Client); err != nil {
		log.Printf("[Sync] Auto-migration skipped (will retry on next restart): %v", err)
	} else if len(mr.Applied) > 0 {
		log.Printf("[Sync] Auto-migration applied %d new schema(s): %v (skipped %d already-applied)", len(mr.Applied), mr.Applied, len(mr.Skipped))
	} else {
		log.Printf("[Sync] Auto-migration: schema up to date (%d migrations tracked)", len(mr.Skipped))
	}

	return nil
}

// StartSyncService 启动同步服务
func StartSyncService(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		log.Printf("[Sync] Sync service not available or disabled")
		return nil
	}

	if err := globalSyncService.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start sync manager: %w", err)
	}

	log.Printf("[Sync] Sync service started")
	return nil
}

// StopSyncService 停止同步服务（退出前执行一次完整同步）
func StopSyncService() error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}

	// 退出前执行一次完整同步
	log.Printf("[Sync] Performing final sync before shutdown...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := globalSyncService.manager.SyncNow(ctx); err != nil {
		log.Printf("[Sync] Warning: Final sync failed: %v", err)
	} else {
		log.Printf("[Sync] Final sync completed successfully")
	}

	if err := globalSyncService.manager.Stop(); err != nil {
		return fmt.Errorf("failed to stop sync manager: %w", err)
	}

	log.Printf("[Sync] Sync service stopped")
	return nil
}

// SyncNow 立即同步
func SyncNow(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return fmt.Errorf("sync service not available")
	}

	return globalSyncService.manager.SyncNow(ctx)
}

// DownloadConfigOnly 下载配置（启动时使用）
// 返回: 配置数据，是否使用了本地配置，错误
func DownloadConfigOnly(ctx context.Context) (map[string]any, bool, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, false, fmt.Errorf("sync service not available")
	}

	return globalSyncService.manager.downloadConfigWithFallback(ctx)
}

// SyncConfigOnly 同步配置
func SyncConfigOnly(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return fmt.Errorf("sync service not available")
	}

	return globalSyncService.manager.SyncNow(ctx)
}

// UploadConfig 上传配置
func UploadConfig(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}

	config, err := globalSyncService.configAdapter.LoadConfig()
	if err != nil {
		return err
	}
	return globalSyncService.manager.UploadConfig(ctx, config)
}

// GetSyncStatus 获取同步状态
func GetSyncStatus() *SyncStatus {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return &SyncStatus{
			IsRunning: false,
			LastError: "sync service not available",
		}
	}

	status := globalSyncService.manager.GetSyncStatus()
	return &status
}

// IsServiceEnabled 检查同步服务是否启用
func IsServiceEnabled() bool {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	return globalSyncService != nil && globalSyncService.isEnabled
}

// IsEnabled 检查同步功能是否启用（基于环境变量）
func IsEnabled() bool {
	return IsD1ConfigComplete()
}

// ConfigSyncCallback 配置同步回调 (仅本地发起的配置变更触发, 见 config.RegisterSyncCallback)
// 短时间内连续调用会被合并为一次，避免用户快速点击导致 goroutine 风暴
func ConfigSyncCallback() {
	if !IsServiceEnabled() {
		log.Printf("[Sync] Config sync skipped - service not enabled")
		return
	}

	// 本地改动即视为持有比 D1 更新的配置: 立刻推进位点, 挡住上传完成前
	// 恰好到达的旧版本心跳把这份改动回灌覆盖
	AdvanceConfigCursor(time.Now().UnixMilli())

	configSyncDebounceMu.Lock()
	defer configSyncDebounceMu.Unlock()

	if configSyncTimer != nil {
		configSyncTimer.Stop()
	}
	configSyncPending = true
	configSyncTimer = time.AfterFunc(configSyncDebounceWindow, func() {
		configSyncDebounceMu.Lock()
		if !configSyncPending {
			configSyncDebounceMu.Unlock()
			return
		}
		configSyncPending = false
		configSyncDebounceMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := SyncConfigOnly(ctx); err != nil {
			log.Printf("[Sync] Warning: Config sync to D1 failed: %v", err)
		} else {
			log.Printf("[Sync] Config synced to D1 successfully (debounced)")
		}
	})
}

// FlushConfigSync 立即触发等待中的配置同步（用于关闭流程）
func FlushConfigSync() {
	configSyncDebounceMu.Lock()
	if configSyncTimer != nil {
		configSyncTimer.Stop()
	}
	if !configSyncPending {
		configSyncDebounceMu.Unlock()
		return
	}
	configSyncPending = false
	configSyncDebounceMu.Unlock()

	if !IsServiceEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := SyncConfigOnly(ctx); err != nil {
		log.Printf("[Sync] Warning: Final config sync to D1 failed: %v", err)
	} else {
		log.Printf("[Sync] Final config synced to D1 before shutdown")
	}
}

// ============================================
// Metrics 同步接口（供 metrics 包调用）
// ============================================

// LoadOwnPathStats 只加载本节点上报的路径统计, 用于启动恢复内存计数
//
// 必须按节点过滤: 拿全节点合计恢复会让本节点把其他节点的量当成自己的起点继续累加,
// 下次上报又把这个虚高值写回 D1, 每次重启滚一轮雪球
func LoadOwnPathStats(ctx context.Context) ([]PathStat, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	return globalSyncService.manager.storage.GetPathStats(ctx, MetricsQuery{NodeID: NodeID()})
}

// DeleteOwnPathStats 删除本节点在 D1 中的路径统计行, prefix 为空时删除本节点全部行
//
// 重置统计时必须调用: 上报逻辑会跳过请求数为 0 的路径, 单靠清空内存不会覆盖 D1 里的旧值,
// 节点下次重启从 D1 恢复时会把重置前的数字又读回来
//
// 只删本节点的行: 其他节点的内存里仍有各自的累计值, 删了它们的行也会在下个上报周期重新写回
func DeleteOwnPathStats(ctx context.Context, prefix string) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}

	stmt := SQLStatement{
		SQL:    "DELETE FROM path_stats WHERE node_id = ?",
		Params: []any{NodeID()},
	}
	if prefix != "" {
		stmt.SQL = "DELETE FROM path_stats WHERE node_id = ? AND (path = ? OR path LIKE ?)"
		stmt.Params = []any{NodeID(), prefix, prefix + "/%"}
	}

	_, err := globalSyncService.manager.storage.ExecuteSQLBatch(ctx, []SQLStatement{stmt})
	return err
}

// 节点列表不再单独查询, 由 BulkFetch 的结果直接派生, 见 internal/metrics/cluster.go

// BulkSave 单请求上报本节点全部指标并完成一次心跳交换, 统一在此打上 node_id
//
// 合并前五张表各打一个 POST; 合并后一次往返, 请求数降到五分之一,
// 也让上报间隔有余量往下调 (间隔越短, 其他节点在仪表盘上越新)。
//
// resetSeen 是本节点已应用的重置代际位点; 返回的 PulseState 里带比游标新的
// 下行状态 (配置 / 重置 marker), 由调用方 (MetricsStorage) 分发应用
func BulkSave(ctx context.Context, payload *BulkPayload, resetSeen int64) (*PulseState, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled || payload == nil {
		return nil, nil
	}

	nodeID := NodeID()
	for i := range payload.PathStats {
		payload.PathStats[i].NodeID = nodeID
	}
	for i := range payload.StatusCodes {
		payload.StatusCodes[i].NodeID = nodeID
	}
	for i := range payload.Latency {
		payload.Latency[i].NodeID = nodeID
	}
	for i := range payload.Timeseries {
		payload.Timeseries[i].NodeID = nodeID
	}
	for i := range payload.RefererDaily {
		payload.RefererDaily[i].NodeID = nodeID
	}
	payload.Cursors = &PulseCursors{
		ConfigVersion: ConfigCursor(),
		ResetSeen:     resetSeen,
	}

	state, err := globalSyncService.manager.storage.BulkUpload(ctx, payload)
	if err != nil {
		return nil, err
	}

	log.Printf("[D1Sync] Bulk saved (node=%s): %d path stats, %d status codes, %d latency, %d ts, %d referer",
		nodeID, len(payload.PathStats), len(payload.StatusCodes), len(payload.Latency),
		len(payload.Timeseries), len(payload.RefererDaily))
	return state, nil
}

// BulkLoadOwn 单请求读回本节点自己的累计统计, 用于启动恢复内存计数
func BulkLoadOwn(ctx context.Context) (*BulkMetrics, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	return globalSyncService.manager.storage.BulkFetch(ctx, MetricsQuery{NodeID: NodeID()})
}

// BulkLoadByNode 单请求读回全部节点的原始行 + 其他节点的状态码/延迟合计
//
// path_stats 用 by_node 拿原始行 (仪表盘要各节点明细), 而 status_codes / latency
// 没有分节点展示需求, worker 端对同一 scope 已按 exclude_node 语义聚合
func BulkLoadByNode(ctx context.Context) (*BulkMetrics, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	return globalSyncService.manager.storage.BulkFetch(ctx, MetricsQuery{
		ByNode:      true,
		ExcludeNode: NodeID(),
	})
}

// LoadPathTimeseries 拉取 [minHour, maxHour] 范围的跨节点聚合数据
func LoadPathTimeseries(ctx context.Context, minHour, maxHour int64) ([]AggregatedHourPoint, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}
	return globalSyncService.manager.storage.GetAggregatedTimeseries(ctx, minHour, maxHour)
}

// PruneTimeseries 清理 cutoffHour 之前的旧桶, 返回删除条数
func PruneTimeseries(ctx context.Context, cutoffHour int64) (int, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return 0, nil
	}
	return globalSyncService.manager.storage.PruneTimeseries(ctx, cutoffHour)
}

// LoadRefererDaily 拉取 [minDate, maxDate] 范围的跨节点聚合数据
func LoadRefererDaily(ctx context.Context, minDate, maxDate int64) ([]AggregatedRefererDayPoint, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}
	return globalSyncService.manager.storage.GetAggregatedRefererDaily(ctx, minDate, maxDate)
}

// PruneRefererDaily 清理 cutoffDate 之前的天级桶, 返回删除条数
func PruneRefererDaily(ctx context.Context, cutoffDate int64) (int, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return 0, nil
	}
	return globalSyncService.manager.storage.PruneRefererDaily(ctx, cutoffDate)
}
