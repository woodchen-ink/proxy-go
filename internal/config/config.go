package config

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	configCallbacks []func(*Config)
	callbackMutex   sync.RWMutex
)

type ConfigManager struct {
	config     atomic.Value
	configPath string
	mu         sync.RWMutex
}

func NewConfigManager(configPath string) (*ConfigManager, error) {
	cm := &ConfigManager{
		configPath: configPath,
	}

	// 加载配置
	config, err := cm.loadConfigFromFile()
	if err != nil {
		return nil, err
	}

	// 确保所有路径配置的扩展名规则都已更新
	for path, pc := range config.MAP {
		pc.ProcessExtensionMap()
		config.MAP[path] = pc // 更新回原始map
	}

	cm.config.Store(config)
	log.Printf("[ConfigManager] 配置已加载: %d 个路径映射", len(config.MAP))

	return cm, nil
}

// loadConfigFromFile 从文件加载配置
func (cm *ConfigManager) loadConfigFromFile() (*Config, error) {
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		// 如果文件不存在，创建默认配置
		if os.IsNotExist(err) {
			if createErr := cm.createDefaultConfig(); createErr == nil {
				return cm.loadConfigFromFile() // 重新加载
			} else {
				return nil, createErr
			}
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// 确保 /mirror 系统路径存在
	cm.ensureMirrorPath(&config)

	return &config, nil
}

// ensureMirrorPath 确保 mirror 系统路径存在于配置中
func (cm *ConfigManager) ensureMirrorPath(config *Config) {
	if config.MAP == nil {
		config.MAP = make(map[string]PathConfig)
	}

	// 检查是否已经存在 mirror 功能的路径（DefaultTarget 为 "mirror"）
	hasMirrorPath := false
	for _, pathConfig := range config.MAP {
		if pathConfig.DefaultTarget == "mirror" {
			hasMirrorPath = true
			break
		}
	}

	// 如果不存在 mirror 路径，添加默认的 /mirror 配置
	if !hasMirrorPath {
		config.MAP["/mirror"] = PathConfig{
			DefaultTarget: "mirror", // 特殊标记，表示这是 mirror 功能
			Enabled:       true,
			CacheConfig: &CacheConfig{
				MaxAge:       30,
				CleanupTick:  5,
				MaxCacheSize: 10,
			},
		}
		log.Printf("[ConfigManager] 自动添加 /mirror 系统路径")
	}
}

// createDefaultConfig 创建默认配置文件
func (cm *ConfigManager) createDefaultConfig() error {
	// 创建目录（如果不存在）
	dir := cm.configPath[:strings.LastIndex(cm.configPath, "/")]
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
						Domains:       "a.com,b.com",   // 只对a.com和b.com域名生效
					},
					{
						Extensions:    "jpg,png,webp",
						Target:        "https://img2.example.com",
						SizeThreshold: 2 * 1024 * 1024, // 2MB
						MaxSize:       5 * 1024 * 1024, // 5MB
						Domains:       "b.com",         // 只对b.com域名生效
					},
					{
						Extensions:    "mp4,avi",
						Target:        "https://video.example.com",
						SizeThreshold: 1024 * 1024,      // 1MB
						MaxSize:       50 * 1024 * 1024, // 50MB
						// 不指定Domains，对所有域名生效
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
		Security: SecurityConfig{
			IPBan: IPBanConfig{
				Enabled:                true,
				ErrorThreshold:         60,
				WindowMinutes:          1,
				BanDurationMinutes:     5,
				CleanupIntervalMinutes: 1440,
			},
		},
		Cache: CacheConfig{
			MaxAge:       30, // 30分钟
			CleanupTick:  5,  // 5分钟清理一次
			MaxCacheSize: 10, // 10GB
		},
		MirrorCache: CacheConfig{
			MaxAge:       30, // 30分钟
			CleanupTick:  5,  // 5分钟清理一次
			MaxCacheSize: 10, // 10GB
		},
	}

	// 序列化为JSON
	data, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		return err
	}

	// 写入文件
	return os.WriteFile(cm.configPath, data, 0644)
}

// GetConfig 获取当前配置
func (cm *ConfigManager) GetConfig() *Config {
	return cm.config.Load().(*Config)
}

// UpdateConfig 更新配置
func (cm *ConfigManager) UpdateConfig(newConfig *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 确保所有路径配置的扩展名规则都已更新
	for path, pc := range newConfig.MAP {
		pc.ProcessExtensionMap()
		newConfig.MAP[path] = pc // 更新回原始map
	}

	// 保存到文件
	if err := cm.saveConfigToFile(newConfig); err != nil {
		return err
	}

	// 更新内存中的配置
	cm.config.Store(newConfig)

	// 触发回调
	TriggerCallbacks(newConfig)

	log.Printf("[ConfigManager] 配置已更新: %d 个路径映射", len(newConfig.MAP))
	return nil
}

// saveConfigToFile 保存配置到文件
func (cm *ConfigManager) saveConfigToFile(config *Config) error {
	// 将新配置格式化为JSON
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// 保存到临时文件
	tempFile := cm.configPath + ".tmp"
	if err := os.WriteFile(tempFile, configData, 0644); err != nil {
		return err
	}

	// 重命名临时文件为正式文件
	return os.Rename(tempFile, cm.configPath)
}

// ReloadConfig 重新加载配置文件
func (cm *ConfigManager) ReloadConfig() error {
	config, err := cm.loadConfigFromFile()
	if err != nil {
		return err
	}

	// 确保所有路径配置的扩展名规则都已更新
	for path, pc := range config.MAP {
		pc.ProcessExtensionMap()
		config.MAP[path] = pc // 更新回原始map
	}

	cm.config.Store(config)

	// 触发回调
	TriggerCallbacks(config)

	log.Printf("[ConfigManager] 配置已重新加载: %d 个路径映射", len(config.MAP))
	return nil
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
	for path, pc := range cfg.MAP {
		pc.ProcessExtensionMap()
		cfg.MAP[path] = pc // 更新回原始map
	}

	callbackMutex.RLock()
	defer callbackMutex.RUnlock()
	for _, callback := range configCallbacks {
		callback(cfg)
	}

	// 添加日志
	log.Printf("[Config] 触发了 %d 个配置更新回调", len(configCallbacks))
}

// 为了向后兼容，保留Load函数，但现在它使用ConfigManager
var globalConfigManager *ConfigManager

// Load 加载配置（向后兼容）
func Load(path string) (*Config, error) {
	if globalConfigManager == nil {
		var err error
		globalConfigManager, err = NewConfigManager(path)
		if err != nil {
			return nil, err
		}
	}
	return globalConfigManager.GetConfig(), nil
}

// SetGlobalConfigManager 设置全局配置管理器
func SetGlobalConfigManager(cm *ConfigManager) {
	globalConfigManager = cm
}

// GetConfig 获取当前配置（全局接口）
func GetConfig() *Config {
	if globalConfigManager == nil {
		return nil
	}
	return globalConfigManager.GetConfig()
}

// ReloadConfig 重新加载配置（全局接口）
func ReloadConfig() error {
	if globalConfigManager == nil {
		return nil
	}
	return globalConfigManager.ReloadConfig()
}

// UpdateConfig 更新配置（全局接口）
func UpdateConfig(newConfig *Config) error {
	if globalConfigManager == nil {
		return nil
	}
	return globalConfigManager.UpdateConfig(newConfig)
}
