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
	if err := m.syncFileToD1(ctx, "data/path_stats.json"); err != nil {
		log.Printf("[D1Sync] Warning: Path stats sync failed: %v", err)
		// 不中断流程
	}

	// 同步封禁IP
	if err := m.syncFileToD1(ctx, "data/banned_ips.json"); err != nil {
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

// UploadConfig 上传配置
func (m *D1Manager) UploadConfig(ctx context.Context, config any) error {
	// 直接上传配置JSON
	configJson, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := m.storage.Upload(ctx, "data/config.json", configJson); err != nil {
		return fmt.Errorf("failed to upload config to D1: %w", err)
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

// syncFileToD1 同步文件到D1
func (m *D1Manager) syncFileToD1(ctx context.Context, filePath string) error {
	// 读取文件内容
	data, err := readFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// 上传到D1
	if err := m.storage.Upload(ctx, filePath, data); err != nil {
		return fmt.Errorf("failed to upload %s to D1: %w", filePath, err)
	}

	return nil
}

// downloadConfigWithFallback 下载配置，如果远程不存在则上传本地配置
func (m *D1Manager) downloadConfigWithFallback(ctx context.Context) error {
	log.Printf("[D1Sync] Checking for remote config...")

	// 尝试下载远程配置
	configData, err := m.storage.Download(ctx, "data/config.json")
	if err != nil {
		log.Printf("[D1Sync] Remote config not found, uploading local config as initial version: %v", err)

		// 远程不存在，上传本地配置作为初始版本
		config, loadErr := m.configLoader.LoadConfig()
		if loadErr != nil {
			return fmt.Errorf("failed to load local config: %w", loadErr)
		}

		if uploadErr := m.UploadConfig(ctx, config); uploadErr != nil {
			return fmt.Errorf("failed to upload initial config: %w", uploadErr)
		}

		log.Printf("[D1Sync] Successfully uploaded local config as initial version")
		return nil
	}

	// 远程存在，保存到本地
	log.Printf("[D1Sync] Remote config found, downloading...")

	var config map[string]any
	if err := json.Unmarshal(configData, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config JSON: %w", err)
	}

	if err := m.configLoader.SaveConfig(config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("[D1Sync] Successfully downloaded remote config")
	return nil
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

// readFile 读取文件内容（辅助函数）
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
