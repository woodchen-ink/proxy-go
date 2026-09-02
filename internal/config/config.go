package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	configCallbacks []func(*Config)
	// syncCallbacks 只在**本地发起**的配置变更时触发 (用于回写 D1)。
	// 从 D1 心跳热应用远端配置时不触发, 否则 A 改 → B 收 → B 又传, 形成回声循环
	syncCallbacks []func(*Config)
	callbackMutex sync.RWMutex
)

type ConfigManager struct {
	config     atomic.Value
	configPath string
	mu         sync.RWMutex
	// snapshotMu 串行化快照文件写入 (tmp+rename), 避免并发写同名临时文件
	snapshotMu sync.Mutex
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

	// 规范化配置 (nil → 默认值, 旧字段补全)
	cm.normalizeConfig(config)

	// 确保所有路径配置的扩展名规则都已更新
	for path, pc := range config.MAP {
		pc.ProcessExtensionMap()
		config.MAP[path] = pc // 更新回原始map
	}

	// 应用环境变量覆盖
	cm.applyEnvOverrides(config)

	cm.config.Store(config)
	log.Printf("[ConfigManager] 配置已加载: %d 个路径映射", len(config.MAP))

	return cm, nil
}

// NewConfigManagerFromData 从已有的配置数据创建 ConfigManager（D1 模式）
func NewConfigManagerFromData(configPath string, configData map[string]any) (*ConfigManager, error) {
	cm := &ConfigManager{
		configPath: configPath,
	}

	// 将 map[string]any 转换为 Config 结构
	config, err := cm.convertMapToConfig(configData)
	if err != nil {
		return nil, err
	}

	// 规范化配置
	cm.normalizeConfig(config)

	// 确保 /mirror 系统路径存在
	cm.ensureMirrorPath(config)

	// 确保所有路径配置的扩展名规则都已更新
	for path, pc := range config.MAP {
		pc.ProcessExtensionMap()
		config.MAP[path] = pc
	}

	// 应用环境变量覆盖
	cm.applyEnvOverrides(config)

	cm.config.Store(config)
	log.Printf("[ConfigManager] 从 D1 加载配置: %d 个路径映射", len(config.MAP))

	return cm, nil
}

// applyEnvOverrides 应用环境变量覆盖配置
func (cm *ConfigManager) applyEnvOverrides(config *Config) {
	// FAVICON_URL 环境变量覆盖
	if faviconURL := os.Getenv("FAVICON_URL"); faviconURL != "" {
		config.FaviconURL = faviconURL
		log.Printf("[ConfigManager] 使用环境变量 FAVICON_URL: %s", faviconURL)
	}

	// 安全配置环境变量覆盖
	if threshold := os.Getenv("SECURITY_404_THRESHOLD"); threshold != "" {
		if val, err := strconv.Atoi(threshold); err == nil {
			config.Security.IPBan.ErrorThreshold = val
			log.Printf("[ConfigManager] 使用环境变量 SECURITY_404_THRESHOLD: %d", val)
		}
	}

	if enabled := os.Getenv("SECURITY_IPBAN_ENABLED"); enabled != "" {
		if enabled == "false" || enabled == "0" {
			config.Security.IPBan.Enabled = false
			log.Printf("[ConfigManager] 使用环境变量禁用 IP 封禁")
		} else if enabled == "true" || enabled == "1" {
			config.Security.IPBan.Enabled = true
			log.Printf("[ConfigManager] 使用环境变量启用 IP 封禁")
		}
	}

	if window := os.Getenv("SECURITY_WINDOW_MINUTES"); window != "" {
		if val, err := strconv.Atoi(window); err == nil {
			config.Security.IPBan.WindowMinutes = val
			log.Printf("[ConfigManager] 使用环境变量 SECURITY_WINDOW_MINUTES: %d", val)
		}
	}

	if duration := os.Getenv("SECURITY_BAN_DURATION"); duration != "" {
		if val, err := strconv.Atoi(duration); err == nil {
			config.Security.IPBan.BanDurationMinutes = val
			log.Printf("[ConfigManager] 使用环境变量 SECURITY_BAN_DURATION: %d", val)
		}
	}
}

// convertMapToConfig 将 map[string]any 转换为 Config 结构
func (cm *ConfigManager) convertMapToConfig(data map[string]any) (*Config, error) {
	// 先序列化为 JSON，再反序列化为 Config
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return nil, err
	}

	// 确保 MAP 不为 nil
	if config.MAP == nil {
		config.MAP = make(map[string]PathConfig)
	}

	return &config, nil
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

	// 规范化配置（向后兼容）
	cm.normalizeConfig(&config)

	// 确保 /mirror 系统路径存在
	cm.ensureMirrorPath(&config)

	return &config, nil
}

// normalizeConfig 规范化配置，确保向后兼容
// 要点:
//  1. PathConfig.Enabled 旧配置默认 true
//  2. SecurityConfig.RefererBan.Hosts / PathConfig.RefererBan.Hosts 把 nil 归一为空切片,
//     避免 D1 / JSON round-trip 把空数组变 null 后, 前端访问 .length / .map 时崩溃
func (cm *ConfigManager) normalizeConfig(config *Config) {
	// 遍历所有路径配置，确保 Enabled 字段有默认值
	for path, pathConfig := range config.MAP {
		// 如果 Enabled 为 false 且 DefaultTarget 不为空
		// 说明这是旧配置（JSON unmarshal 默认值为 false）
		// 我们将其设置为 true（默认启用）
		if !pathConfig.Enabled && (pathConfig.DefaultTarget != "" || len(pathConfig.DefaultTargets) > 0) {
			pathConfig.Enabled = true
		}
		if pathConfig.RefererBan != nil {
			if pathConfig.RefererBan.Hosts == nil {
				pathConfig.RefererBan.Hosts = []string{}
			}
			if pathConfig.RefererBan.Mode == "" {
				pathConfig.RefererBan.Mode = RefererModeBlacklist
			}
		}
		// 归一多源列表: 剔除空白/空项; 保证 DefaultTarget 与 DefaultTargets[0] 语义一致,
		// 让依赖 DefaultTarget 的逻辑 (mirror 标记判断 / 展示) 在多源配置下不破。
		normalizeTargets(&pathConfig)
		config.MAP[path] = pathConfig
	}

	// 全局 RefererBan 也归一
	if config.Security.RefererBan.Hosts == nil {
		config.Security.RefererBan.Hosts = []string{}
	}
}

// normalizeTargets 归一单路径的多源配置
//  1. 剔除 DefaultTargets 中的空白/空项
//  2. DefaultTargets 非空且 DefaultTarget 为空时, 用 DefaultTargets[0] 回填 DefaultTarget
//     (mirror 标记判断、前端展示等仍按 DefaultTarget 读单值, 不能让它落空)
//  3. 仅 1 个源时清空 DefaultTargets, 只保留 DefaultTarget, 让配置保持精简
func normalizeTargets(pc *PathConfig) {
	if len(pc.DefaultTargets) > 0 {
		cleaned := make([]string, 0, len(pc.DefaultTargets))
		for _, t := range pc.DefaultTargets {
			if t = strings.TrimSpace(t); t != "" {
				cleaned = append(cleaned, t)
			}
		}
		pc.DefaultTargets = cleaned
	}

	if len(pc.DefaultTargets) > 0 {
		if pc.DefaultTarget == "" {
			pc.DefaultTarget = pc.DefaultTargets[0]
		}
		// 单源不必保留列表, 退化为 DefaultTarget 单字段
		if len(pc.DefaultTargets) == 1 {
			pc.DefaultTargets = nil
		}
	}
}

// ensureMirrorPath 确保 mirror 系统路径存在于配置中
// 返回 true 表示配置被修改，false 表示未修改
func (cm *ConfigManager) ensureMirrorPath(config *Config) bool {
	if config.MAP == nil {
		config.MAP = make(map[string]PathConfig)
	}

	// 检查是否已经存在 mirror 功能的路径（DefaultTarget 为 MirrorTargetMarker）
	hasMirrorPath := false
	for _, pathConfig := range config.MAP {
		if pathConfig.DefaultTarget == MirrorTargetMarker {
			hasMirrorPath = true
			break
		}
	}

	// 如果不存在 mirror 路径，添加默认的 /mirror 配置
	if !hasMirrorPath {
		config.MAP[MirrorPathKey] = PathConfig{
			DefaultTarget: MirrorTargetMarker, // 特殊标记，表示这是 mirror 功能
			Enabled:       true,
			CacheConfig: &CacheConfig{
				MaxAge:       DefaultCacheMaxAge,
				CleanupTick:  DefaultCacheCleanupTick,
				MaxCacheSize: DefaultCacheMaxSizeGB,
			},
		}
		log.Printf("[ConfigManager] 自动添加 /mirror 系统路径")
		return true
	}

	return false
}

// createDefaultConfig 创建默认配置文件 (仅本地模式首次运行走到这里)
//
// 刻意不放任何样例路径: 历史上这里会伪造 "/test → localhost:8080" 之类的假配置,
// 与生产兜底文件身份重叠 —— 一旦 D1 拉取失败, 节点会顶着一份"看起来正常"的假配置
// 对外服务, 甚至把它播种进 D1。空 MAP 是诚实的状态: 没配置就是没配置, 由后台添加。
// 参考样例见仓库的 data/config.example.json
func (cm *ConfigManager) createDefaultConfig() error {
	// 创建目录（如果不存在）
	dir := cm.configPath[:strings.LastIndex(cm.configPath, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	defaultConfig := Config{
		MAP: map[string]PathConfig{},
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
			MaxAge:       DefaultCacheMaxAge,
			CleanupTick:  DefaultCacheCleanupTick,
			MaxCacheSize: DefaultCacheMaxSizeGB,
		},
		MirrorCache: CacheConfig{
			MaxAge:       DefaultCacheMaxAge,
			CleanupTick:  DefaultCacheCleanupTick,
			MaxCacheSize: DefaultCacheMaxSizeGB,
		},
	}

	// 序列化为JSON
	data, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		return err
	}

	log.Printf("[ConfigManager] 本地配置文件不存在, 已生成空白默认配置 (0 个路径映射), 请通过后台添加路径")

	// 写入文件
	return os.WriteFile(cm.configPath, data, 0644)
}

// GetConfig 获取当前配置
func (cm *ConfigManager) GetConfig() *Config {
	return cm.config.Load().(*Config)
}

// UpdateConfig 更新配置 (本地发起: 后台保存 / 从 D1 手动拉取后落地)
func (cm *ConfigManager) UpdateConfig(newConfig *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 规范化 (nil 字段归一)
	cm.normalizeConfig(newConfig)

	// 确保所有路径配置的扩展名规则都已更新
	for path, pc := range newConfig.MAP {
		pc.ProcessExtensionMap()
		newConfig.MAP[path] = pc // 更新回原始map
	}

	// 更新内存中的配置
	cm.config.Store(newConfig)

	// 触发回调（会自动同步到 D1）
	TriggerCallbacks(newConfig)

	// 刷新本地快照: 兜底文件始终保持为"本节点见过的最新配置"
	cm.SaveSnapshot()

	log.Printf("[ConfigManager] 配置已更新: %d 个路径映射", len(newConfig.MAP))
	return nil
}

// ApplyRemoteData 热应用来自 D1 心跳的远端配置
//
// 与 UpdateConfig 的区别只有一个: 触发本地回调 (matcher 重建 / 组件刷新) 但**不触发
// 回写 D1 的 sync 回调** —— 这份配置本来就来自 D1, 回传就是回声循环。
// 处理流程对齐启动时的 NewConfigManagerFromData (含 mirror 路径与环境变量覆盖),
// 保证同一份 D1 数据从哪个入口进来行为都一致
func (cm *ConfigManager) ApplyRemoteData(configData map[string]any) error {
	newConfig, err := cm.convertMapToConfig(configData)
	if err != nil {
		return err
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.normalizeConfig(newConfig)
	cm.ensureMirrorPath(newConfig)
	for path, pc := range newConfig.MAP {
		pc.ProcessExtensionMap()
		newConfig.MAP[path] = pc
	}
	cm.applyEnvOverrides(newConfig)

	cm.config.Store(newConfig)
	triggerLocalCallbacks(newConfig)
	cm.SaveSnapshot()

	log.Printf("[ConfigManager] 已热应用远端配置: %d 个路径映射", len(newConfig.MAP))
	return nil
}

// SaveSnapshot 把当前内存配置原子写入本地兜底文件
//
// 让 data/config.json 的身份从"可能是样例"变成"最近一次成功同步的线上配置":
// 之后 D1 不可达时的兜底才真正可信。失败只记日志 —— 快照是可用性增强, 不是主链路
func (cm *ConfigManager) SaveSnapshot() {
	cfg, ok := cm.config.Load().(*Config)
	if !ok || cfg == nil {
		return
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("[ConfigManager] 序列化配置快照失败: %v", err)
		return
	}

	cm.snapshotMu.Lock()
	defer cm.snapshotMu.Unlock()

	if dir := filepath.Dir(cm.configPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[ConfigManager] 创建快照目录失败: %v", err)
			return
		}
	}

	tempPath := cm.configPath + ".snapshot.tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		log.Printf("[ConfigManager] 写入配置快照失败: %v", err)
		return
	}
	if err := os.Rename(tempPath, cm.configPath); err != nil {
		log.Printf("[ConfigManager] 落盘配置快照失败: %v", err)
	}
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

// RegisterUpdateCallback 注册配置更新回调函数 (本地与远端应用都会触发)
func RegisterUpdateCallback(callback func(*Config)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	configCallbacks = append(configCallbacks, callback)
}

// RegisterSyncCallback 注册"回写 D1"回调, 只在本地发起的配置变更时触发
//
// 与 RegisterUpdateCallback 分开注册, 是心跳热应用不产生回声循环的关键:
// ApplyRemoteData 只跑普通回调, 不跑这里注册的回调
func RegisterSyncCallback(callback func(*Config)) {
	callbackMutex.Lock()
	defer callbackMutex.Unlock()
	syncCallbacks = append(syncCallbacks, callback)
}

// TriggerCallbacks 触发所有回调 (普通 + 回写 D1), 用于本地发起的配置变更
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
	for _, callback := range syncCallbacks {
		callback(cfg)
	}

	// 添加日志
	log.Printf("[Config] 触发了 %d 个配置更新回调", len(configCallbacks)+len(syncCallbacks))
}

// triggerLocalCallbacks 只触发普通回调, 用于远端配置热应用 (不回写 D1)
func triggerLocalCallbacks(cfg *Config) {
	callbackMutex.RLock()
	defer callbackMutex.RUnlock()
	for _, callback := range configCallbacks {
		callback(cfg)
	}
	log.Printf("[Config] 远端配置应用: 触发了 %d 个本地回调", len(configCallbacks))
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

// ApplyRemoteConfig 热应用远端配置（全局接口, 供 D1 心跳调用）
//
// 管理器未初始化时必须报错而不是静默成功: 调用方 (pulse) 会在成功后推进版本位点
// 并解除配置只读, 静默成功会让这两个状态在配置根本没落地时被错误推进
func ApplyRemoteConfig(configData map[string]any) error {
	if globalConfigManager == nil {
		return fmt.Errorf("config manager not initialized")
	}
	return globalConfigManager.ApplyRemoteData(configData)
}
