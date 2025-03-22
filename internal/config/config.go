package config

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

func NewConfigManager(configPath string) (*ConfigManager, error) {
	cm := &ConfigManager{
		configPath: configPath,
	}

	// 加载配置
	config, err := Load(configPath)
	if err != nil {
		return nil, err
	}

	// 确保所有路径配置的扩展名规则都已更新
	for _, pathConfig := range config.MAP {
		pathConfig.ProcessExtensionMap()
	}

	cm.config.Store(config)
	log.Printf("[ConfigManager] 配置已加载: %d 个路径映射", len(config.MAP))

	return cm, nil
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
				// 添加新式扩展名规则映射示例
				ExtensionMap: []ExtRuleConfig{
					{
						Extensions:    "jpg,png,webp",
						Target:        "https://img1.example.com",
						SizeThreshold: 500 * 1024,      // 500KB
						MaxSize:       2 * 1024 * 1024, // 2MB
					},
					{
						Extensions:    "jpg,png,webp",
						Target:        "https://img2.example.com",
						SizeThreshold: 2 * 1024 * 1024, // 2MB
						MaxSize:       5 * 1024 * 1024, // 5MB
					},
				},
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
	// 确保所有路径配置的扩展名规则都已更新
	for _, pathConfig := range cfg.MAP {
		pathConfig.ProcessExtensionMap()
	}

	callbackMutex.RLock()
	defer callbackMutex.RUnlock()
	for _, callback := range configCallbacks {
		callback(cfg)
	}

	// 添加日志
	log.Printf("[Config] 触发了 %d 个配置更新回调", len(configCallbacks))
}

// Update 更新配置并触发回调
func (c *configImpl) Update(newConfig *Config) {
	c.Lock()
	defer c.Unlock()

	// 确保所有路径配置的扩展名规则都已更新
	for _, pathConfig := range newConfig.MAP {
		pathConfig.ProcessExtensionMap()
	}

	// 更新配置
	c.MAP = newConfig.MAP
	c.Compression = newConfig.Compression

	// 触发回调
	for _, callback := range c.onConfigUpdate {
		callback(newConfig)
	}

	// 添加日志
	log.Printf("[Config] 配置已更新: %d 个路径映射", len(newConfig.MAP))
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

	// 确保所有路径配置的扩展名规则都已更新
	for _, pathConfig := range config.MAP {
		pathConfig.ProcessExtensionMap()
	}

	cm.config.Store(config)
	log.Printf("[ConfigManager] 配置已加载: %d 个路径映射", len(config.MAP))
	return nil
}

func (cm *ConfigManager) GetConfig() *Config {
	return cm.config.Load().(*Config)
}
