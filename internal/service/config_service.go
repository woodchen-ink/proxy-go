package service

import (
	"fmt"
	"net/url"
	"os"
	"proxy-go/internal/config"
)

type ConfigService struct {
	configManager *config.ConfigManager
}

func NewConfigService(configManager *config.ConfigManager) *ConfigService {
	return &ConfigService{
		configManager: configManager,
	}
}

// GetConfig 获取当前配置
func (s *ConfigService) GetConfig() ([]byte, error) {
	configData, err := os.ReadFile("data/config.json")
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}
	return configData, nil
}

// SaveConfig 保存并验证配置
func (s *ConfigService) SaveConfig(newConfig *config.Config) error {
	// 验证新配置
	if err := s.validateConfig(newConfig); err != nil {
		return fmt.Errorf("配置验证失败: %v", err)
	}

	// 使用ConfigManager更新配置
	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return fmt.Errorf("更新配置失败: %v", err)
	}

	// 添加日志
	fmt.Printf("[Config] 配置已更新: %d 个路径映射\n", len(newConfig.MAP))

	return nil
}

// validateConfig 验证配置
func (s *ConfigService) validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}

	// 验证MAP配置
	if cfg.MAP == nil {
		return fmt.Errorf("MAP配置不能为空")
	}

	for path, pathConfig := range cfg.MAP {
		if path == "" {
			return fmt.Errorf("路径不能为空")
		}
		if pathConfig.DefaultTarget == "" {
			return fmt.Errorf("路径 %s 的默认目标不能为空", path)
		}
		if _, err := url.Parse(pathConfig.DefaultTarget); err != nil {
			return fmt.Errorf("路径 %s 的默认目标URL无效: %v", path, err)
		}
	}

	return nil
}