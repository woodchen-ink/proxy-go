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
	manager       SyncManager
	configAdapter *ConfigAdapter
	metricsAdapter *MetricsAdapter
	isEnabled     bool
	mutex         sync.RWMutex
}

// InitSyncService 初始化同步服务
func InitSyncService() error {
	globalSyncMutex.Lock()
	defer globalSyncMutex.Unlock()
	
	// 检查是否启用同步
	if !IsEnabled() {
		log.Printf("[Sync] Sync service disabled (no S3 config)")
		return nil
	}
	
	// 从环境变量创建配置
	syncConfig, err := NewConfigFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create sync config: %w", err)
	}
	
	// 创建S3客户端
	s3Client, err := NewS3Client(syncConfig)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}
	
	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := s3Client.TestConnection(ctx); err != nil {
		return fmt.Errorf("failed to test S3 connection: %w", err)
	}
	
	// 创建适配器
	configAdapter := NewConfigAdapter("data/config.json")
	metricsAdapter := NewMetricsAdapter("data/metrics")
	
	// 创建同步管理器
	manager := NewManager(s3Client, syncConfig, configAdapter, metricsAdapter)
	
	// 创建同步服务
	globalSyncService = &SyncService{
		manager:        manager,
		configAdapter:  configAdapter,
		metricsAdapter: metricsAdapter,
		isEnabled:      true,
	}
	
	log.Printf("[Sync] Sync service initialized successfully")
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
	
	// 启动事件监听
	go globalSyncService.handleEvents()
	
	log.Printf("[Sync] Sync service started")
	return nil
}

// StopSyncService 停止同步服务
func StopSyncService() error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()
	
	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}
	
	if err := globalSyncService.manager.Stop(); err != nil {
		return fmt.Errorf("failed to stop sync manager: %w", err)
	}
	
	log.Printf("[Sync] Sync service stopped")
	return nil
}

// SyncNow 立即同步（完整目录同步）
func SyncNow(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()
	
	if globalSyncService == nil || !globalSyncService.isEnabled {
		return fmt.Errorf("sync service not available")
	}
	
	return globalSyncService.manager.SyncNow(ctx)
}

// SyncConfigOnly 仅同步主配置文件（快速同步）
func SyncConfigOnly(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()
	
	if globalSyncService == nil || !globalSyncService.isEnabled {
		return fmt.Errorf("sync service not available")
	}
	
	if manager, ok := globalSyncService.manager.(*Manager); ok {
		return manager.SyncConfigOnly(ctx)
	}
	
	return fmt.Errorf("sync manager type mismatch")
}

// UploadConfig 上传配置（在配置更新时调用）
func UploadConfig(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()
	
	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil // 不返回错误，只是不执行同步
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

// handleEvents 处理同步事件
func (s *SyncService) handleEvents() {
	if manager, ok := s.manager.(*Manager); ok {
		for event := range manager.GetEventChannel() {
			switch event.Type {
			case SyncEventStart:
				log.Printf("[Sync] %s", event.Message)
			case SyncEventSuccess:
				log.Printf("[Sync] %s", event.Message)
			case SyncEventUpload:
				log.Printf("[Sync] %s", event.Message)
			case SyncEventDownload:
				log.Printf("[Sync] %s", event.Message)
			case SyncEventError:
				log.Printf("[Sync] Error: %s", event.Message)
				if event.Error != nil {
					log.Printf("[Sync] Error details: %v", event.Error)
				}
			}
		}
	}
}

// ConfigSyncCallback 配置同步回调（在config包中使用）
func ConfigSyncCallback() {
	if !IsServiceEnabled() {
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	go func() {
		// 配置变更时使用快速同步，只同步主配置文件
		if err := SyncConfigOnly(ctx); err != nil {
			log.Printf("[Sync] Failed to sync config after change: %v", err)
		} else {
			log.Printf("[Sync] Config synced after change")
		}
	}()
}