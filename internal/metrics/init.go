package metrics

import (
	"log"
	"proxy-go/internal/config"
)

func Init(cfg *config.Config) error {
	// 初始化收集器
	if err := InitCollector(cfg); err != nil {
		log.Printf("[Metrics] 初始化收集器失败: %v", err)
		//继续运行
		return err
	}

	// 初始化指标存储服务
	if err := InitMetricsStorage(cfg); err != nil {
		log.Printf("[Metrics] 初始化指标存储服务失败: %v", err)
		//继续运行
		return err
	}

	log.Printf("[Metrics] 初始化完成")

	return nil
}
