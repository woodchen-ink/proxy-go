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
	// 上报间隔直接决定"其他节点数据能有多新" —— 仪表盘上 peer 的滞后下限就是它。
	//
	// 能从 30 分钟降到 5 分钟, 靠的是另外两处改动: 上报窗口收窄 (见 persistence.go 的
	// timeseriesUploadHours / refererUploadDays) 与五表合并成单请求上报。
	//
	// 卡住这个值的不是请求数 (5 分钟每天才几百次), 而是 **D1 每日写入行数**:
	// 每次上报约 100 行/节点, 5 分钟约 6 万行/天, 免费版上限 10 万。再往下调到
	// 1 分钟会到约 29 万行/天直接超额 —— 除非用 Workers Paid, 否则不要单独降这个值,
	// 要降先把上报改成"只传变脏的桶"
	saveInterval := 5 * time.Minute

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
