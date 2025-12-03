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
)

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

// ConfigSyncCallback 配置同步回调（在config包中使用）
func ConfigSyncCallback() {
	if !IsServiceEnabled() {
		log.Printf("[Sync] Config sync skipped - service not enabled")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := SyncConfigOnly(ctx); err != nil {
			log.Printf("[Sync] Warning: Config sync to D1 failed: %v", err)
		} else {
			log.Printf("[Sync] Config synced to D1 successfully")
		}
	}()
}

// ============================================
// Metrics 同步接口（供 metrics 包调用）
// ============================================

// SaveStatusCodes 保存状态码统计到 D1
func SaveStatusCodes(ctx context.Context, statusCodes map[string]int64) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}

	return globalSyncService.manager.SaveStatusCodes(ctx, statusCodes)
}

// LoadStatusCodes 从 D1 加载状态码统计
func LoadStatusCodes(ctx context.Context) (map[string]int64, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	return globalSyncService.manager.LoadStatusCodes(ctx)
}

// SaveLatencyDistribution 保存延迟分布到 D1
func SaveLatencyDistribution(ctx context.Context, distribution map[string]int64) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}

	return globalSyncService.manager.SaveLatencyDistribution(ctx, distribution)
}

// LoadLatencyDistribution 从 D1 加载延迟分布
func LoadLatencyDistribution(ctx context.Context) (map[string]int64, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	return globalSyncService.manager.LoadLatencyDistribution(ctx)
}

// LoadPathStats 从 D1 加载路径统计
func LoadPathStats(ctx context.Context) ([]PathStat, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	// 调用 D1Client 的 GetPathStats 方法
	return globalSyncService.manager.storage.GetPathStats(ctx, "")
}
