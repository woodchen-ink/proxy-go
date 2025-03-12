package config

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config 配置结构体
type configImpl struct {
	sync.RWMutex
	Config
	// 配置更新回调函数
	onConfigUpdate []func(*Config)
}

var (
	instance        *configImpl
	once            sync.Once
	configCallbacks []func(*Config)
	callbackMutex   sync.RWMutex
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

// Load 加载配置
func Load(path string) (*Config, error) {
	var err error
	once.Do(func() {
		instance = &configImpl{}
		err = instance.reload(path)
		// 如果文件不存在，创建默认配置并重新加载
		if err != nil && os.IsNotExist(err) {
			if createErr := createDefaultConfig(path); createErr == nil {
				err = instance.reload(path)
			} else {
				err = createErr
			}
		}
	})
	return &instance.Config, err
}

// createDefaultConfig 创建默认配置文件
func createDefaultConfig(path string) error {
	// 创建目录（如果不存在）
	dir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// 创建默认配置
	defaultConfig := Config{
		MAP: map[string]PathConfig{
			"/": {
				DefaultTarget: "http://localhost:8080",
			},
		},
		Compression: CompressionConfig{
			Gzip: CompressorConfig{
				Enabled: true,
				Level:   6,
			},
			Brotli: CompressorConfig{
				Enabled: true,
				Level:   6,
			},
		},
	}

	// 序列化为JSON
	data, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		return err
	}

	// 写入文件
	return os.WriteFile(path, data, 0644)
}

// RegisterUpdateCallback 注册配置更新回调函数
func RegisterUpdateCallback(callback func(*Config)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	configCallbacks = append(configCallbacks, callback)
}

// TriggerCallbacks 触发所有回调
func TriggerCallbacks(cfg *Config) {
	callbackMutex.RLock()
	defer callbackMutex.RUnlock()
	for _, callback := range configCallbacks {
		callback(cfg)
	}
}

// Update 更新配置并触发回调
func (c *configImpl) Update(newConfig *Config) {
	c.Lock()
	defer c.Unlock()

	// 更新配置
	c.MAP = newConfig.MAP
	c.Compression = newConfig.Compression

	// 触发回调
	for _, callback := range c.onConfigUpdate {
		callback(newConfig)
	}
}

// reload 重新加载配置文件
func (c *configImpl) reload(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var newConfig Config
	if err := json.Unmarshal(data, &newConfig); err != nil {
		return err
	}

	c.Update(&newConfig)
	return nil
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
