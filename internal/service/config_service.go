package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"proxy-go/internal/config"
	"proxy-go/pkg/sync"
	"time"
)

type ConfigService struct {
	configManager *config.ConfigManager
}

func NewConfigService(configManager *config.ConfigManager) *ConfigService {
	return &ConfigService{
		configManager: configManager,
	}
}

// GetConfig 获取当前配置（从内存）
func (s *ConfigService) GetConfig() ([]byte, error) {
	// 从 ConfigManager 获取当前配置
	cfg := s.configManager.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("配置未初始化")
	}

	// 序列化为 JSON
	configData, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %v", err)
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

// PullConfigFromD1 从 D1 拉取配置并更新本地
func (s *ConfigService) PullConfigFromD1() ([]byte, error) {
	// 检查 D1 同步是否启用
	if !sync.IsEnabled() {
		return nil, fmt.Errorf("D1 同步未启用")
	}

	// 从 D1 下载配置
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	configData, usedLocal, err := sync.DownloadConfigOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("从 D1 下载配置失败: %v", err)
	}

	if usedLocal {
		return nil, fmt.Errorf("D1 配置为空，无法拉取")
	}

	// 将 map[string]any 转换为 Config 结构
	configJson, err := json.Marshal(configData)
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %v", err)
	}

	var newConfig config.Config
	if err := json.Unmarshal(configJson, &newConfig); err != nil {
		return nil, fmt.Errorf("解析配置失败: %v", err)
	}

	// 验证配置
	if err := s.validateConfig(&newConfig); err != nil {
		return nil, fmt.Errorf("配置验证失败: %v", err)
	}

	// 更新本地配置
	if err := s.configManager.UpdateConfig(&newConfig); err != nil {
		return nil, fmt.Errorf("更新本地配置失败: %v", err)
	}

	fmt.Printf("[Config] 从 D1 拉取配置成功: %d 个路径映射\n", len(newConfig.MAP))

	// 返回配置数据
	return configJson, nil
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