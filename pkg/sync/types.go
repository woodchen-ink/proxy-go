package sync

import (
	"context"
	"time"
)

// SyncData 同步数据结构
type SyncData struct {
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Config    any       `json:"config"`
	Metrics   any       `json:"metrics,omitempty"`
}

// Config S3同步配置
type Config struct {
	Endpoint        string `json:"endpoint"`         // S3端点
	Bucket          string `json:"bucket"`           // 存储桶名称
	Region          string `json:"region"`           // 地区
	AccessKeyID     string `json:"access_key_id"`    // 访问密钥ID
	SecretAccessKey string `json:"secret_access_key"` // 访问密钥
	UsePathStyle    bool   `json:"use_path_style"`   // 是否使用Path Style URL
	ConfigPath      string `json:"config_path"`      // 配置自定义保存路径
}

// CloudStorage 云存储接口
type CloudStorage interface {
	// Upload 上传数据到云端
	Upload(ctx context.Context, key string, data []byte) error
	
	// Download 从云端下载数据
	Download(ctx context.Context, key string) ([]byte, error)
	
	// GetVersion 获取文件版本信息
	GetVersion(ctx context.Context, key string) (string, time.Time, error)
	
	// ListVersions 列出对象版本
	ListVersions(ctx context.Context, key string) ([]VersionInfo, error)
}

// VersionInfo 版本信息
type VersionInfo struct {
	Version    string    `json:"version"`
	Timestamp  time.Time `json:"timestamp"`
	ETag       string    `json:"etag"`
	Size       int64     `json:"size"`
	IsLatest   bool      `json:"is_latest"`
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
	
	// DownloadConfig 下载配置
	DownloadConfig(ctx context.Context) (any, error)
	
	// GetSyncStatus 获取同步状态
	GetSyncStatus() SyncStatus
}

// SyncStatus 同步状态
type SyncStatus struct {
	LastSync    time.Time `json:"last_sync"`
	LastError   string    `json:"last_error,omitempty"`
	IsRunning   bool      `json:"is_running"`
	LocalVersion  string  `json:"local_version"`
	RemoteVersion string  `json:"remote_version"`
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