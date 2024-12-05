package constants

import (
	"proxy-go/internal/config"
	"time"
)

var (
	// 缓存相关
	CacheTTL     = 5 * time.Minute // 缓存过期时间
	MaxCacheSize = 10000           // 最大缓存大小

	// 指标相关
	MetricsInterval = 5 * time.Minute // 指标收集间隔
	MaxPathsStored  = 1000            // 最大存储路径数
	MaxRecentLogs   = 1000            // 最大最近日志数

	// 监控告警相关
	AlertWindowSize           = 12               // 监控窗口数量
	AlertWindowInterval       = 5 * time.Minute  // 每个窗口时间长度
	AlertDedupeWindow         = 15 * time.Minute // 告警去重时间窗口
	AlertNotifyInterval       = 24 * time.Hour   // 告警通知间隔
	MinRequestsForAlert int64 = 10               // 触发告警的最小请求数
	ErrorRateThreshold        = 0.8              // 错误率告警阈值

	// 延迟告警阈值
	SmallFileSize  int64 = 1 * MB   // 小文件阈值
	MediumFileSize int64 = 10 * MB  // 中等文件阈值
	LargeFileSize  int64 = 100 * MB // 大文件阈值

	SmallFileLatency  = 5 * time.Second   // 小文件最大延迟
	MediumFileLatency = 10 * time.Second  // 中等文件最大延迟
	LargeFileLatency  = 50 * time.Second  // 大文件最大延迟
	HugeFileLatency   = 300 * time.Second // 超大文件最大延迟 (5分钟)

	// 单位常量
	KB int64 = 1024
	MB int64 = 1024 * KB

	// 数据验证相关
	MaxErrorRate     = 0.8  // 最大错误率
	MaxDataDeviation = 0.01 // 最大数据偏差(1%)

	// 性能监控阈值
	MaxRequestsPerMinute int64 = 1000              // 每分钟最大请求数
	MaxBytesPerMinute    int64 = 100 * 1024 * 1024 // 每分钟最大流量 (100MB)
	MaxSaveInterval            = 15 * time.Minute  // 最大保存间隔
)

// UpdateFromConfig 从配置文件更新常量
func UpdateFromConfig(cfg *config.Config) {
	// 告警配置
	if cfg.Metrics.Alert.WindowSize > 0 {
		AlertWindowSize = cfg.Metrics.Alert.WindowSize
	}
	if cfg.Metrics.Alert.WindowInterval > 0 {
		AlertWindowInterval = cfg.Metrics.Alert.WindowInterval
	}
	if cfg.Metrics.Alert.DedupeWindow > 0 {
		AlertDedupeWindow = cfg.Metrics.Alert.DedupeWindow
	}
	if cfg.Metrics.Alert.MinRequests > 0 {
		MinRequestsForAlert = cfg.Metrics.Alert.MinRequests
	}
	if cfg.Metrics.Alert.ErrorRate > 0 {
		ErrorRateThreshold = cfg.Metrics.Alert.ErrorRate
	}
	if cfg.Metrics.Alert.AlertInterval > 0 {
		AlertNotifyInterval = cfg.Metrics.Alert.AlertInterval
	}

	// 延迟告警配置
	if cfg.Metrics.Latency.SmallFileSize > 0 {
		SmallFileSize = cfg.Metrics.Latency.SmallFileSize
	}
	if cfg.Metrics.Latency.MediumFileSize > 0 {
		MediumFileSize = cfg.Metrics.Latency.MediumFileSize
	}
	if cfg.Metrics.Latency.LargeFileSize > 0 {
		LargeFileSize = cfg.Metrics.Latency.LargeFileSize
	}
	if cfg.Metrics.Latency.SmallLatency > 0 {
		SmallFileLatency = cfg.Metrics.Latency.SmallLatency
	}
	if cfg.Metrics.Latency.MediumLatency > 0 {
		MediumFileLatency = cfg.Metrics.Latency.MediumLatency
	}
	if cfg.Metrics.Latency.LargeLatency > 0 {
		LargeFileLatency = cfg.Metrics.Latency.LargeLatency
	}
	if cfg.Metrics.Latency.HugeLatency > 0 {
		HugeFileLatency = cfg.Metrics.Latency.HugeLatency
	}

	// 数据验证相关
	if cfg.Metrics.Validation.MaxErrorRate > 0 {
		MaxErrorRate = cfg.Metrics.Validation.MaxErrorRate
	}
	if cfg.Metrics.Validation.MaxDataDeviation > 0 {
		MaxDataDeviation = cfg.Metrics.Validation.MaxDataDeviation
	}

	// 性能监控阈值
	if cfg.Metrics.Performance.MaxRequestsPerMinute > 0 {
		MaxRequestsPerMinute = cfg.Metrics.Performance.MaxRequestsPerMinute
	}
	if cfg.Metrics.Performance.MaxBytesPerMinute > 0 {
		MaxBytesPerMinute = cfg.Metrics.Performance.MaxBytesPerMinute
	}
	if cfg.Metrics.Performance.MaxSaveInterval > 0 {
		MaxSaveInterval = cfg.Metrics.Performance.MaxSaveInterval
	}
}
