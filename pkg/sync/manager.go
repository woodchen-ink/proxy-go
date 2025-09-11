package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync/atomic"
	"time"
)

// Manager 同步管理器实现
type Manager struct {
	storage       CloudStorage
	config        *Config
	syncInterval  time.Duration
	status        atomic.Value // SyncStatus
	stopChan      chan struct{}
	eventChan     chan SyncEvent
	configLoader  ConfigLoader
	metricsLoader MetricsLoader
	dataSync      *DirectorySync
	faviconSync   *DirectorySync
}

// ConfigLoader 配置加载器接口
type ConfigLoader interface {
	LoadConfig() (any, error)
	SaveConfig(config any) error
	GetConfigVersion() string
}

// MetricsLoader 统计数据加载器接口
type MetricsLoader interface {
	LoadMetrics() (any, error)
	SaveMetrics(metrics any) error
	GetLastUpdate() time.Time
}

// NewManager 创建新的同步管理器
func NewManager(storage CloudStorage, config *Config, configLoader ConfigLoader, metricsLoader MetricsLoader) *Manager {
	m := &Manager{
		storage:       storage,
		config:        config,
		syncInterval:  10 * time.Minute,
		stopChan:      make(chan struct{}),
		eventChan:     make(chan SyncEvent, 100),
		configLoader:  configLoader,
		metricsLoader: metricsLoader,
	}
	
	// 初始化目录同步器
	m.dataSync = NewDirectorySync(storage, config, "data")
	m.faviconSync = NewDirectorySync(storage, config, "favicon")
	
	// 初始化状态
	m.status.Store(SyncStatus{
		IsRunning: false,
	})
	
	return m
}

// Start 启动同步服务
func (m *Manager) Start(ctx context.Context) error {
	status := m.status.Load().(SyncStatus)
	if status.IsRunning {
		return fmt.Errorf("sync manager already running")
	}
	
	// 更新状态
	status.IsRunning = true
	m.status.Store(status)
	
	// 发送启动事件
	m.sendEvent(SyncEvent{
		Type:      SyncEventStart,
		Timestamp: time.Now(),
		Message:   "Sync manager started",
	})
	
	// 初始同步
	if err := m.SyncNow(ctx); err != nil {
		log.Printf("Initial sync failed: %v", err)
	}
	
	// 启动定时同步
	go m.syncLoop(ctx)
	
	return nil
}

// Stop 停止同步服务
func (m *Manager) Stop() error {
	status := m.status.Load().(SyncStatus)
	if !status.IsRunning {
		return fmt.Errorf("sync manager not running")
	}
	
	close(m.stopChan)
	
	// 更新状态
	status.IsRunning = false
	m.status.Store(status)
	
	return nil
}

// SyncNow 立即同步
func (m *Manager) SyncNow(ctx context.Context) error {
	log.Println("Starting full directory sync...")
	
	// 更新状态
	status := m.status.Load().(SyncStatus)
	status.LastSync = time.Now()
	
	// 执行data目录同步
	if err := m.dataSync.SyncDirectory(ctx); err != nil {
		status.LastError = err.Error()
		m.status.Store(status)
		
		m.sendEvent(SyncEvent{
			Type:      SyncEventError,
			Timestamp: time.Now(),
			Message:   "Data directory sync failed",
			Error:     err,
		})
		
		return err
	}
	
	// 执行favicon目录同步
	if err := m.faviconSync.SyncDirectory(ctx); err != nil {
		log.Printf("Warning: Favicon sync failed: %v", err)
		// favicon同步失败不中断整个同步过程
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
		Message:   "Full directory sync completed successfully",
	})
	
	log.Println("Full directory sync completed")
	return nil
}

// UploadConfig 上传配置
func (m *Manager) UploadConfig(ctx context.Context, config any) error {
	return m.uploadConfig(ctx, config, nil)
}

// SyncConfigOnly 仅同步主配置文件（快速同步）
func (m *Manager) SyncConfigOnly(ctx context.Context) error {
	log.Println("Starting config-only sync...")
	
	// 配置文件路径
	configPath := m.config.ConfigPath + "/config.json"
	
	// 比较版本
	remoteVersion, remoteTime, err := m.storage.GetVersion(ctx, configPath)
	if err != nil {
		log.Printf("Failed to get remote version (will try to upload): %v", err)
		// 如果获取远程版本失败，尝试上传本地配置
		return m.uploadLocalConfig(ctx)
	}
	
	localVersion := m.configLoader.GetConfigVersion()
	
	// 更新状态
	status := m.status.Load().(SyncStatus)
	status.LocalVersion = localVersion
	status.RemoteVersion = remoteVersion
	status.LastSync = time.Now()
	
	// 比较版本决定同步方向
	if shouldUpload(localVersion, remoteVersion, time.Time{}, remoteTime) {
		// 上传本地配置
		if err := m.uploadLocalConfig(ctx); err != nil {
			status.LastError = err.Error()
			m.status.Store(status)
			return err
		}
		
		m.sendEvent(SyncEvent{
			Type:      SyncEventUpload,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Uploaded config (version: %s)", localVersion),
		})
	} else if remoteVersion != "" && remoteVersion != localVersion {
		// 下载远程配置
		if err := m.downloadRemoteConfig(ctx); err != nil {
			status.LastError = err.Error()
			m.status.Store(status)
			return err
		}
		
		m.sendEvent(SyncEvent{
			Type:      SyncEventDownload,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Downloaded config (version: %s)", remoteVersion),
		})
	}
	
	// 清除错误
	status.LastError = ""
	m.status.Store(status)
	
	log.Println("Config-only sync completed")
	return nil
}

// DownloadConfig 下载配置
func (m *Manager) DownloadConfig(ctx context.Context) (any, error) {
	configPath := m.config.ConfigPath + "/config.json"
	data, err := m.storage.Download(ctx, configPath)
	if err != nil {
		return nil, err
	}
	
	var syncData SyncData
	if err := json.Unmarshal(data, &syncData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sync data: %w", err)
	}
	
	return syncData.Config, nil
}

// GetSyncStatus 获取同步状态
func (m *Manager) GetSyncStatus() SyncStatus {
	return m.status.Load().(SyncStatus)
}

// GetEventChannel 获取事件通道
func (m *Manager) GetEventChannel() <-chan SyncEvent {
	return m.eventChan
}

// syncLoop 同步循环
func (m *Manager) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(m.syncInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if err := m.SyncNow(ctx); err != nil {
				log.Printf("Scheduled sync failed: %v", err)
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

// uploadLocalConfig 上传本地配置
func (m *Manager) uploadLocalConfig(ctx context.Context) error {
	config, err := m.configLoader.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load local config: %w", err)
	}
	
	var metrics any
	if m.metricsLoader != nil {
		metrics, _ = m.metricsLoader.LoadMetrics()
	}
	
	return m.uploadConfig(ctx, config, metrics)
}

// uploadConfig 上传配置和统计数据
func (m *Manager) uploadConfig(ctx context.Context, config any, metrics any) error {
	// 上传配置文件
	configData := SyncData{
		Version:   m.configLoader.GetConfigVersion(),
		Timestamp: time.Now(),
		Config:    config,
	}
	
	configJson, err := json.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal config data: %w", err)
	}
	
	configPath := m.config.ConfigPath + "/config.json"
	if err := m.storage.Upload(ctx, configPath, configJson); err != nil {
		return fmt.Errorf("failed to upload config: %w", err)
	}
	
	// 上传统计数据（如果有）
	if metrics != nil {
		metricsData := SyncData{
			Version:   m.configLoader.GetConfigVersion(),
			Timestamp: time.Now(),
			Metrics:   metrics,
		}
		
		metricsJson, err := json.Marshal(metricsData)
		if err != nil {
			log.Printf("Failed to marshal metrics data: %v", err)
			return nil // 统计数据上传失败不影响配置同步
		}
		
		metricsPath := m.config.ConfigPath + "/metrics.json"
		if err := m.storage.Upload(ctx, metricsPath, metricsJson); err != nil {
			log.Printf("Failed to upload metrics: %v", err)
			// 统计数据上传失败不影响配置同步
		}
	}
	
	return nil
}

// downloadRemoteConfig 下载远程配置
func (m *Manager) downloadRemoteConfig(ctx context.Context) error {
	// 下载配置文件
	configPath := m.config.ConfigPath + "/config.json"
	configData, err := m.storage.Download(ctx, configPath)
	if err != nil {
		return fmt.Errorf("failed to download remote config: %w", err)
	}
	
	var configSyncData SyncData
	if err := json.Unmarshal(configData, &configSyncData); err != nil {
		return fmt.Errorf("failed to unmarshal config data: %w", err)
	}
	
	// 保存配置
	if err := m.configLoader.SaveConfig(configSyncData.Config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	
	// 尝试下载统计数据（可选）
	if m.metricsLoader != nil {
		metricsPath := m.config.ConfigPath + "/metrics.json"
		metricsData, err := m.storage.Download(ctx, metricsPath)
		if err != nil {
			log.Printf("No remote metrics found or failed to download: %v", err)
		} else {
			var metricsSyncData SyncData
			if err := json.Unmarshal(metricsData, &metricsSyncData); err != nil {
				log.Printf("Failed to unmarshal metrics data: %v", err)
			} else if metricsSyncData.Metrics != nil {
				if err := m.metricsLoader.SaveMetrics(metricsSyncData.Metrics); err != nil {
					log.Printf("Failed to save metrics: %v", err)
				}
			}
		}
	}
	
	return nil
}

// sendEvent 发送事件
func (m *Manager) sendEvent(event SyncEvent) {
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

// shouldUpload 判断是否应该上传本地配置
func shouldUpload(localVersion, remoteVersion string, localTime, remoteTime time.Time) bool {
	// 如果远程版本为空，上传本地
	if remoteVersion == "" {
		return true
	}
	
	// 将版本字符串转换为数字进行比较（Unix时间戳）
	localTimestamp, err1 := parseVersionTimestamp(localVersion)
	remoteTimestamp, err2 := parseVersionTimestamp(remoteVersion)
	
	// 如果解析失败，使用时间比较作为回退
	if err1 != nil || err2 != nil {
		return localTime.After(remoteTime)
	}
	
	// 比较时间戳，本地更新则上传
	return localTimestamp > remoteTimestamp
}

// parseVersionTimestamp 解析版本字符串为Unix时间戳
func parseVersionTimestamp(version string) (int64, error) {
	if version == "" || version == "0" {
		return 0, nil
	}
	
	var timestamp int64
	if _, err := fmt.Sscanf(version, "%d", &timestamp); err != nil {
		return 0, err
	}
	
	return timestamp, nil
}