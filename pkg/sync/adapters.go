package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"proxy-go/internal/config"
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
	cfg, err := ca.loadConfigFromFile()
	if err != nil {
		return nil, err
	}

	// 确保返回的不是nil
	if cfg == nil {
		return nil, fmt.Errorf("loaded config is nil")
	}

	return cfg, nil
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
