package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"time"
)

// ConfigAdapter 配置适配器
type ConfigAdapter struct {
	configPath string
}

// NewConfigAdapter 创建配置适配器
func NewConfigAdapter(configPath string) *ConfigAdapter {
	return &ConfigAdapter{
		configPath: configPath,
	}
}

// LoadConfig 加载配置
func (ca *ConfigAdapter) LoadConfig() (any, error) {
	// 优先使用全局配置管理器
	if globalConfig := config.GetConfig(); globalConfig != nil {
		return globalConfig, nil
	}

	// 如果全局配置管理器未初始化，直接从文件加载
	return ca.loadConfigFromFile()
}

// loadConfigFromFile 直接从文件加载配置
func (ca *ConfigAdapter) loadConfigFromFile() (*config.Config, error) {
	data, err := os.ReadFile(ca.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，返回空配置
			return &config.Config{}, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// SaveConfig 保存配置
func (ca *ConfigAdapter) SaveConfig(configData any) error {
	// 类型断言或转换
	var cfg *config.Config

	switch v := configData.(type) {
	case *config.Config:
		cfg = v
	case config.Config:
		cfg = &v
	case map[string]interface{}:
		// 从map转换为Config结构
		jsonData, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal config map: %w", err)
		}

		cfg = &config.Config{}
		if err := json.Unmarshal(jsonData, cfg); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	default:
		return fmt.Errorf("unsupported config type: %T", configData)
	}

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(ca.configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// 序列化配置
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 原子写入
	tempPath := ca.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}

	if err := os.Rename(tempPath, ca.configPath); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	// 重新加载配置到内存
	if err := config.ReloadConfig(); err != nil {
		log.Printf("Failed to reload config after sync: %v", err)
	}

	log.Printf("Config synced and reloaded successfully")
	return nil
}

// GetConfigVersion 获取配置版本（返回文件修改时间的Unix时间戳字符串）
func (ca *ConfigAdapter) GetConfigVersion() string {
	fileInfo, err := os.Stat(ca.configPath)
	if err != nil {
		// 文件不存在，返回0
		return "0"
	}

	// 返回文件修改时间的Unix时间戳
	return fmt.Sprintf("%d", fileInfo.ModTime().Unix())
}

// 移除了 IsNewNode 和 isDefaultConfig 方法，因为启动时统一只下载，不再需要新节点检测

// MetricsAdapter 统计数据适配器
type MetricsAdapter struct {
	metricsDir string
}

// NewMetricsAdapter 创建统计数据适配器
func NewMetricsAdapter(metricsDir string) *MetricsAdapter {
	return &MetricsAdapter{
		metricsDir: metricsDir,
	}
}

// LoadMetrics 加载统计数据
func (ma *MetricsAdapter) LoadMetrics() (any, error) {
	storage := metrics.GetMetricsStorage()
	if storage == nil {
		return nil, fmt.Errorf("metrics storage not initialized")
	}

	collector := metrics.GetCollector()
	if collector == nil {
		return nil, fmt.Errorf("metrics collector not initialized")
	}

	// 获取当前统计数据
	stats := collector.GetStats()

	// 转换为可序列化的格式
	metricsData := map[string]interface{}{
		"status_code_stats": stats["status_code_stats"],
		"latency_stats":     stats["latency_stats"],
		"timestamp":         time.Now(),
	}

	return metricsData, nil
}

// SaveMetrics 保存统计数据（增量更新）
func (ma *MetricsAdapter) SaveMetrics(metricsData any) error {
	storage := metrics.GetMetricsStorage()
	if storage == nil {
		return fmt.Errorf("metrics storage not initialized")
	}

	// 类型断言
	data, ok := metricsData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid metrics data format")
	}

	// 增量更新状态码统计
	if statusCodeStats, exists := data["status_code_stats"]; exists {
		if err := ma.mergeStatusCodeStats(statusCodeStats); err != nil {
			log.Printf("Failed to merge status code stats: %v", err)
		}
	}

	// 增量更新延迟统计
	if latencyStats, exists := data["latency_stats"]; exists {
		if err := ma.mergeLatencyStats(latencyStats); err != nil {
			log.Printf("Failed to merge latency stats: %v", err)
		}
	}

	// 保存合并后的数据到本地
	if err := storage.SaveMetrics(); err != nil {
		return fmt.Errorf("failed to save merged metrics: %w", err)
	}

	log.Printf("Metrics synced and merged successfully")
	return nil
}

// GetLastUpdate 获取最后更新时间
func (ma *MetricsAdapter) GetLastUpdate() time.Time {
	storage := metrics.GetMetricsStorage()
	if storage == nil {
		return time.Time{}
	}

	return storage.GetLastSaveTime()
}

// mergeStatusCodeStats 合并状态码统计（增量更新）
func (ma *MetricsAdapter) mergeStatusCodeStats(remoteStats interface{}) error {
	collector := metrics.GetCollector()
	if collector == nil {
		return fmt.Errorf("metrics collector not available")
	}

	statsMap, ok := remoteStats.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid status code stats format")
	}

	// 这里简化处理，直接用远程数据覆盖
	// 在实际应用中，你可能需要更复杂的合并逻辑
	for codeStr, countVal := range statsMap {
		var count int64
		switch v := countVal.(type) {
		case float64:
			count = int64(v)
		case int64:
			count = v
		case int:
			count = int64(v)
		default:
			continue
		}

		// 只有当远程计数更大时才更新
		if code, err := parseStatusCode(codeStr); err == nil {
			collector.RecordStatusCodeBatch(code, count)
		}
	}

	return nil
}

// mergeLatencyStats 合并延迟统计（增量更新）
func (ma *MetricsAdapter) mergeLatencyStats(remoteStats interface{}) error {
	// 延迟统计通常是累积的，这里简化处理
	// 实际应用中可能需要更复杂的合并逻辑
	log.Printf("Latency stats merge not implemented yet")
	return nil
}

// parseStatusCode 解析状态码字符串
func parseStatusCode(codeStr string) (int, error) {
	var code int
	if _, err := fmt.Sscanf(codeStr, "%d", &code); err != nil {
		return 0, err
	}
	return code, nil
}

// SyncDataWithTimestamp 带时间戳的同步数据
type SyncDataWithTimestamp struct {
	SyncData
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateVersionedSyncData 创建带版本的同步数据
func CreateVersionedSyncData(config any, metrics any, configVersion string) *SyncDataWithTimestamp {
	now := time.Now()
	return &SyncDataWithTimestamp{
		SyncData: SyncData{
			Version:   configVersion,
			Timestamp: now,
			Config:    config,
			Metrics:   metrics,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}
