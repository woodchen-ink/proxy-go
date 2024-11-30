package config

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"time"
)

type ConfigManager struct {
	config     atomic.Value
	configPath string
}

func NewConfigManager(path string) *ConfigManager {
	cm := &ConfigManager{configPath: path}
	cm.loadConfig()
	go cm.watchConfig()
	return cm
}

func (cm *ConfigManager) watchConfig() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		cm.loadConfig()
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func (cm *ConfigManager) loadConfig() error {
	config, err := Load(cm.configPath)
	if err != nil {
		return err
	}
	cm.config.Store(config)
	return nil
}

func (cm *ConfigManager) GetConfig() *Config {
	return cm.config.Load().(*Config)
}
