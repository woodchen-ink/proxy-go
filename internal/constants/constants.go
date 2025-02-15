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

	// 单位常量
	KB int64 = 1024
	MB int64 = 1024 * KB
)

// UpdateFromConfig 从配置文件更新常量
func UpdateFromConfig(cfg *config.Config) {
	// 空实现,不再需要更新监控相关配置
}
