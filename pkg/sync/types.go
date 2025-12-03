package sync

import (
	"context"
	"time"
)

// ConfigLoader 配置加载器接口
type ConfigLoader interface {
	LoadConfig() (any, error)
	SaveConfig(config any) error
	GetConfigVersion() string
}

// SyncManager 同步管理器接口
type SyncManager interface {
	// Start 启动同步服务
	Start(ctx context.Context) error

	// Stop 停止同步服务
	Stop() error

	// SyncNow 立即同步
	SyncNow(ctx context.Context) error

	// UploadConfig 上传配置
	UploadConfig(ctx context.Context, config any) error

	// GetSyncStatus 获取同步状态
	GetSyncStatus() SyncStatus
}

// SyncStatus 同步状态
type SyncStatus struct {
	LastSync      time.Time `json:"last_sync"`
	LastError     string    `json:"last_error,omitempty"`
	IsRunning     bool      `json:"is_running"`
	LocalVersion  string    `json:"local_version"`
	RemoteVersion string    `json:"remote_version"`
}

// SyncEvent 同步事件
type SyncEvent struct {
	Type      SyncEventType `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	Message   string        `json:"message"`
	Error     error         `json:"error,omitempty"`
}

// SyncEventType 同步事件类型
type SyncEventType string

const (
	SyncEventStart    SyncEventType = "start"
	SyncEventSuccess  SyncEventType = "success"
	SyncEventError    SyncEventType = "error"
	SyncEventUpload   SyncEventType = "upload"
	SyncEventDownload SyncEventType = "download"
)
