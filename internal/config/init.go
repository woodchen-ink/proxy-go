package config

import "log"

func Init(configPath string) (*ConfigManager, error) {
	log.Printf("[Config] 初始化配置管理器...")

	configManager, err := NewConfigManager(configPath)
	if err != nil {
		log.Printf("[Config] 初始化配置管理器失败: %v", err)
		return nil, err
	}

	log.Printf("[Config] 配置管理器初始化成功")
	return configManager, nil
}
