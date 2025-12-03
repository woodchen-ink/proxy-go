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
	isEnabled     bool
	mutex         sync.RWMutex
}

// InitSyncService 初始化同步服务
func InitSyncService() error {
	globalSyncMutex.Lock()
	defer globalSyncMutex.Unlock()

	// 检查是否启用同步
	if !IsEnabled() {
		log.Printf("[Sync] Sync service disabled (no sync config)")
		return nil
	}

	// 创建适配器
	configAdapter := NewConfigAdapter("data/config.json")

	// 根据同步类型创建不同的管理器
	syncType := GetSyncType()
	var manager SyncManager

	switch syncType {
	case SyncTypeD1:
		log.Printf("[Sync] Initializing D1 sync service...")

		// 创建D1配置
		d1Config, err := NewD1ConfigFromEnv()
		if err != nil {
			return fmt.Errorf("failed to create D1 config: %w", err)
		}

		// 创建D1客户端
		d1Client := NewD1Client(d1Config.Endpoint, d1Config.Token)

		// 创建D1管理器
		manager = NewD1Manager(d1Client, configAdapter)

		log.Printf("[Sync] D1 sync service initialized (endpoint: %s)", d1Config.Endpoint)

	case SyncTypeS3:
		log.Printf("[Sync] Initializing S3 sync service (legacy mode)...")

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

		// 创建S3管理器
		manager = NewManager(s3Client, syncConfig, configAdapter)

		log.Printf("[Sync] S3 sync service initialized")

	default:
		return fmt.Errorf("unknown sync type: %s", syncType)
	}

	// 创建同步服务
	globalSyncService = &SyncService{
		manager:       manager,
		configAdapter: configAdapter,
		isEnabled:     true,
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
		// 即使同步失败也继续停止服务
	} else {
		log.Printf("[Sync] Final sync completed successfully")
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

// DownloadConfigOnly 仅下载配置（启动时使用）
// 返回: 配置数据，是否使用了本地配置，错误
// 如果远程不存在配置，则上传本地配置作为初始版本
func DownloadConfigOnly(ctx context.Context) (map[string]any, bool, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, false, fmt.Errorf("sync service not available")
	}

	// 支持 S3 Manager (保持原有行为，从文件加载)
	if manager, ok := globalSyncService.manager.(*Manager); ok {
		err := manager.downloadConfigWithFallback(ctx)
		if err != nil {
			return nil, false, err
		}
		// S3 模式下从文件加载配置
		config, loadErr := globalSyncService.configAdapter.LoadConfig()
		if loadErr != nil {
			return nil, false, loadErr
		}
		configMap, ok := config.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("config is not map[string]any")
		}
		return configMap, false, nil
	}

	// 支持 D1 Manager
	if manager, ok := globalSyncService.manager.(*D1Manager); ok {
		return manager.downloadConfigWithFallback(ctx)
	}

	return nil, false, fmt.Errorf("sync manager type mismatch")
}

// SyncConfigOnly 仅同步主配置文件（快速同步）
func SyncConfigOnly(ctx context.Context) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return fmt.Errorf("sync service not available")
	}

	// 支持 S3 Manager
	if manager, ok := globalSyncService.manager.(*Manager); ok {
		return manager.SyncConfigOnly(ctx)
	}

	// D1 Manager 使用 SyncNow（D1不区分快速同步和完整同步）
	if manager, ok := globalSyncService.manager.(*D1Manager); ok {
		return manager.SyncNow(ctx)
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

// IsEnabled 检查同步功能是否启用（基于环境变量）
func IsEnabled() bool {
	return IsConfigComplete()
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
		log.Printf("[Sync] Config sync skipped - service not enabled")
		return
	}
	
	go func() {
		// 在goroutine内部创建context，避免过早取消
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		// 配置变更时使用快速同步，只同步主配置文件
		if err := SyncConfigOnly(ctx); err != nil {
			// S3同步失败不影响本地配置，只记录警告日志
			log.Printf("[Sync] Warning: Config sync to cloud failed (local config saved successfully): %v", err)
		} else {
			log.Printf("[Sync] Config synced to cloud successfully")
		}
	}()
}