package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"
)

// D1Manager D1同步管理器（简化版本，不支持目录同步）
type D1Manager struct {
	storage      *D1Client
	configLoader ConfigLoader
	status       atomic.Value // SyncStatus
	stopChan     chan struct{}
	eventChan    chan SyncEvent
}

// NewD1Manager 创建D1同步管理器
func NewD1Manager(storage *D1Client, configLoader ConfigLoader) *D1Manager {
	m := &D1Manager{
		storage:      storage,
		configLoader: configLoader,
		stopChan:     make(chan struct{}),
		eventChan:    make(chan SyncEvent, 100),
	}

	// 初始化状态
	m.status.Store(SyncStatus{
		IsRunning: false,
	})

	return m
}

// Start 启动同步服务
func (m *D1Manager) Start(ctx context.Context) error {
	status := m.status.Load().(SyncStatus)
	if status.IsRunning {
		return fmt.Errorf("D1 sync manager already running")
	}

	// 更新状态
	status.IsRunning = true
	m.status.Store(status)

	// 发送启动事件
	m.sendEvent(SyncEvent{
		Type:      SyncEventStart,
		Timestamp: time.Now(),
		Message:   "D1 sync manager started",
	})

	log.Printf("[D1Sync] Skipping initial sync at startup (already downloaded config)")

	// 启动定时同步（仅同步config.json）
	go m.syncLoop(ctx)

	return nil
}

// Stop 停止同步服务
func (m *D1Manager) Stop() error {
	status := m.status.Load().(SyncStatus)
	if !status.IsRunning {
		return fmt.Errorf("D1 sync manager not running")
	}

	close(m.stopChan)

	// 更新状态
	status.IsRunning = false
	m.status.Store(status)

	return nil
}

// SyncNow 立即同步所有数据
func (m *D1Manager) SyncNow(ctx context.Context) error {
	log.Println("[D1Sync] Starting full data sync...")

	// 更新状态
	status := m.status.Load().(SyncStatus)
	status.LastSync = time.Now()

	// 同步配置
	if err := m.syncConfigToD1(ctx); err != nil {
		status.LastError = err.Error()
		m.status.Store(status)

		m.sendEvent(SyncEvent{
			Type:      SyncEventError,
			Timestamp: time.Now(),
			Message:   "Config sync failed",
			Error:     err,
		})

		return err
	}

	// 同步路径统计
	if err := m.syncPathStatsToD1(ctx); err != nil {
		log.Printf("[D1Sync] Warning: Path stats sync failed: %v", err)
		// 不中断流程
	}

	// 同步封禁IP
	if err := m.syncBannedIPsToD1(ctx); err != nil {
		log.Printf("[D1Sync] Warning: Banned IPs sync failed: %v", err)
		// 不中断流程
	}

	// 获取本地版本信息
	localVersion := m.configLoader.GetConfigVersion()
	status.LocalVersion = localVersion
	status.RemoteVersion = localVersion // 同步后版本应该一致

	// 清除错误
	status.LastError = ""
	m.status.Store(status)

	m.sendEvent(SyncEvent{
		Type:      SyncEventSuccess,
		Timestamp: time.Now(),
		Message:   "Full D1 sync completed successfully",
	})

	log.Println("[D1Sync] Full sync completed")
	return nil
}

// UploadConfig 上传配置（转换为列式存储）
func (m *D1Manager) UploadConfig(ctx context.Context, config any) error {
	// 保存配置到临时文件以便转换
	tempFile := "data/config.json.temp"
	configJson, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(tempFile, configJson, 0644); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}
	defer os.Remove(tempFile)

	// 转换为列式存储格式
	maps, others, err := ConvertConfigFromFile(tempFile)
	if err != nil {
		return fmt.Errorf("failed to convert config: %w", err)
	}

	// 批量上传 ConfigMaps
	if len(maps) > 0 {
		if err := m.storage.BatchUpsertConfigMaps(ctx, maps); err != nil {
			return fmt.Errorf("failed to upload config maps: %w", err)
		}
		log.Printf("[D1Sync] Uploaded %d config maps", len(maps))
	}

	// 批量上传 ConfigOther
	if len(others) > 0 {
		if err := m.storage.BatchUpsertConfigOther(ctx, others); err != nil {
			return fmt.Errorf("failed to upload config other: %w", err)
		}
		log.Printf("[D1Sync] Uploaded %d other configs", len(others))
	}

	return nil
}

// GetSyncStatus 获取同步状态
func (m *D1Manager) GetSyncStatus() SyncStatus {
	return m.status.Load().(SyncStatus)
}

// GetEventChannel 获取事件通道
func (m *D1Manager) GetEventChannel() <-chan SyncEvent {
	return m.eventChan
}

// syncLoop 同步循环
func (m *D1Manager) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.SyncNow(ctx); err != nil {
				log.Printf("[D1Sync] Scheduled sync failed: %v", err)
				m.sendEvent(SyncEvent{
					Type:      SyncEventError,
					Timestamp: time.Now(),
					Message:   "Scheduled sync failed",
					Error:     err,
				})
			}
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// syncConfigToD1 同步配置到D1
func (m *D1Manager) syncConfigToD1(ctx context.Context) error {
	config, err := m.configLoader.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load local config: %w", err)
	}

	return m.UploadConfig(ctx, config)
}

// syncPathStatsToD1 同步路径统计到D1
func (m *D1Manager) syncPathStatsToD1(ctx context.Context) error {
	filePath := "data/path_stats.json"

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("[D1Sync] Path stats file not found, skipping")
		return nil
	}

	// 转换为列式存储格式
	stats, err := ConvertPathStatsFromFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to convert path stats: %w", err)
	}

	if len(stats) == 0 {
		log.Printf("[D1Sync] No path stats to sync")
		return nil
	}

	// 批量上传
	if err := m.storage.BatchUpsertPathStats(ctx, stats); err != nil {
		return fmt.Errorf("failed to upload path stats: %w", err)
	}

	log.Printf("[D1Sync] Uploaded %d path stats", len(stats))
	return nil
}

// syncBannedIPsToD1 同步封禁IP到D1
func (m *D1Manager) syncBannedIPsToD1(ctx context.Context) error {
	filePath := "data/banned_ips.json"

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("[D1Sync] Banned IPs file not found, skipping")
		return nil
	}

	// 转换为列式存储格式
	bans, err := ConvertBannedIPsFromFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to convert banned IPs: %w", err)
	}

	if len(bans) > 0 {
		// 批量上传当前封禁
		if err := m.storage.BatchUpsertBannedIPs(ctx, bans); err != nil {
			return fmt.Errorf("failed to upload banned IPs: %w", err)
		}
		log.Printf("[D1Sync] Uploaded %d banned IPs", len(bans))
	}

	// 注意: 历史记录暂不同步，避免重复写入
	// 如果需要同步历史记录，可以在 Worker 端添加历史记录批量插入接口

	return nil
}

// downloadConfigWithFallback 下载配置，如果远程不存在则上传本地配置
// 返回: 远程配置数据（如果存在），是否使用了本地配置，错误
func (m *D1Manager) downloadConfigWithFallback(ctx context.Context) (map[string]any, bool, error) {
	log.Printf("[D1Sync] Checking for remote config...")

	// 尝试下载远程 ConfigMaps 和 ConfigOther
	maps, mapsErr := m.storage.GetConfigMaps(ctx, false)
	others, othersErr := m.storage.GetConfigOther(ctx, "")

	// 判断远程是否有有效配置：
	// 1. API调用失败（mapsErr/othersErr != nil）表示网络或权限问题
	// 2. API调用成功但返回空数据（len == 0）表示远程数据库为空
	// 两种情况都应该上传本地配置作为初始版本
	remoteHasConfig := (mapsErr == nil && len(maps) > 0) || (othersErr == nil && len(others) > 0)

	if !remoteHasConfig {
		// 远程配置为空或API调用失败
		if mapsErr != nil || othersErr != nil {
			log.Printf("[D1Sync] Remote config check failed (maps: %v, others: %v), uploading local config", mapsErr, othersErr)
		} else {
			log.Printf("[D1Sync] Remote config is empty, uploading local config as initial version")
		}

		// 加载本地配置
		localConfigAny, loadErr := m.configLoader.LoadConfig()
		if loadErr != nil {
			return nil, false, fmt.Errorf("failed to load local config: %w", loadErr)
		}

		// 类型断言
		localConfig, ok := localConfigAny.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("local config is not map[string]any")
		}

		// 上传本地配置作为初始版本
		if uploadErr := m.UploadConfig(ctx, localConfigAny); uploadErr != nil {
			log.Printf("[D1Sync] Warning: failed to upload initial config: %v", uploadErr)
			// 即使上传失败，也返回本地配置
		} else {
			log.Printf("[D1Sync] Successfully uploaded local config as initial version")
		}

		return localConfig, true, nil
	}

	// 远程存在有效配置，重建配置对象
	log.Printf("[D1Sync] Remote config found (%d maps, %d others), downloading...", len(maps), len(others))

	config := make(map[string]any)

	// 重建 MAP 配置
	if mapsErr == nil && len(maps) > 0 {
		mapConfig := make(map[string]any)
		for _, cm := range maps {
			pathConfig := map[string]any{
				"DefaultTarget": cm.DefaultTarget,
				"Enabled":       cm.IsEnabled(), // 使用方法转换为 bool
			}

			// 解析 ExtensionRules JSON
			if cm.ExtensionRules != "" {
				var extRules map[string]any
				if err := json.Unmarshal([]byte(cm.ExtensionRules), &extRules); err == nil {
					pathConfig["ExtensionMap"] = extRules
				}
			}

			// 解析 CacheConfig JSON
			if cm.CacheConfig != "" {
				var cacheConf map[string]any
				if err := json.Unmarshal([]byte(cm.CacheConfig), &cacheConf); err == nil {
					pathConfig["CacheConfig"] = cacheConf
				}
			}

			mapConfig[cm.Path] = pathConfig
		}
		config["MAP"] = mapConfig
	}

	// 重建其他配置
	if othersErr == nil {
		for _, other := range others {
			var value any
			if err := json.Unmarshal([]byte(other.Value), &value); err == nil {
				config[other.Key] = value
			}
		}
	}

	// 不再保存到本地文件，直接返回配置数据
	log.Printf("[D1Sync] Successfully downloaded remote config (%d maps, %d others)",
		len(maps), len(others))
	return config, false, nil
}

// sendEvent 发送事件
func (m *D1Manager) sendEvent(event SyncEvent) {
	select {
	case m.eventChan <- event:
	default:
		// 如果通道满了，丢弃最旧的事件
		select {
		case <-m.eventChan:
		default:
		}
		m.eventChan <- event
	}
}

