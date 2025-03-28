package metrics

import (
	"log"
	"path/filepath"
	"proxy-go/internal/config"
	"time"
)

var (
	metricsStorage *MetricsStorage
)

// InitMetricsStorage 初始化指标存储服务
func InitMetricsStorage(cfg *config.Config) error {

	// 创建指标存储服务
	dataDir := filepath.Join("data", "metrics")
	saveInterval := 30 * time.Minute // 默认30分钟保存一次，减少IO操作

	metricsStorage = NewMetricsStorage(GetCollector(), dataDir, saveInterval)

	// 启动指标存储服务
	if err := metricsStorage.Start(); err != nil {
		log.Printf("[Metrics] 启动指标存储服务失败: %v", err)
		return err
	}

	log.Printf("[Metrics] 指标存储服务已初始化，保存间隔: %v", saveInterval)
	return nil
}

// StopMetricsStorage 停止指标存储服务
func StopMetricsStorage() {
	if metricsStorage != nil {
		metricsStorage.Stop()
		log.Printf("[Metrics] 指标存储服务已停止")
	}
}

// GetMetricsStorage 获取指标存储服务实例
func GetMetricsStorage() *MetricsStorage {
	return metricsStorage
}
