package initapp

import (
	"encoding/json"
	"log"
	"os"
)

// 旧配置结构
type OldPathConfig struct {
	DefaultTarget string      `json:"DefaultTarget"`
	ExtensionMap  interface{} `json:"ExtensionMap"`
	SizeThreshold int64       `json:"SizeThreshold,omitempty"`
	MaxSize       int64       `json:"MaxSize,omitempty"`
	Path          string      `json:"Path,omitempty"`
}

// 新配置结构
type NewPathConfig struct {
	DefaultTarget string          `json:"DefaultTarget"`
	ExtensionMap  []ExtRuleConfig `json:"ExtensionMap"`
}

type ExtRuleConfig struct {
	Extensions    string `json:"Extensions"`
	Target        string `json:"Target"`
	SizeThreshold int64  `json:"SizeThreshold"`
	MaxSize       int64  `json:"MaxSize"`
}

type CompressionConfig struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}

type Config struct {
	MAP         map[string]interface{} `json:"MAP"`
	Compression CompressionConfig      `json:"Compression"`
}

// MigrateConfig 检查并迁移配置文件，确保使用新格式
func MigrateConfig(configPath string) error {
	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	// 解析为通用配置结构
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	configChanged := false

	// 遍历所有路径配置
	for path, rawPathConfig := range config.MAP {
		// 将接口转换为JSON
		rawData, err := json.Marshal(rawPathConfig)
		if err != nil {
			log.Printf("[Init] 无法序列化路径 %s 的配置: %v", path, err)
			continue
		}

		// 尝试解析为旧格式
		var oldPathConfig OldPathConfig
		if err := json.Unmarshal(rawData, &oldPathConfig); err != nil {
			log.Printf("[Init] 无法解析路径 %s 的配置: %v", path, err)
			continue
		}

		// 创建新格式配置
		newPathConfig := NewPathConfig{
			DefaultTarget: oldPathConfig.DefaultTarget,
			ExtensionMap:  []ExtRuleConfig{},
		}

		// 检查ExtensionMap类型
		if oldPathConfig.ExtensionMap != nil {
			// 尝试将ExtensionMap解析为旧格式的map
			oldFormatMap := make(map[string]string)
			if rawExtMap, err := json.Marshal(oldPathConfig.ExtensionMap); err == nil {
				if json.Unmarshal(rawExtMap, &oldFormatMap) == nil && len(oldFormatMap) > 0 {
					// 是旧格式的map，转换为数组
					for exts, target := range oldFormatMap {
						rule := ExtRuleConfig{
							Extensions:    exts,
							Target:        target,
							SizeThreshold: oldPathConfig.SizeThreshold,
							MaxSize:       oldPathConfig.MaxSize,
						}
						newPathConfig.ExtensionMap = append(newPathConfig.ExtensionMap, rule)
					}
					configChanged = true
					log.Printf("[Init] 路径 %s 的配置已从旧版格式迁移到新版格式", path)
				}
			}

			// 尝试将ExtensionMap解析为新格式的数组
			if len(newPathConfig.ExtensionMap) == 0 {
				var newFormatArray []ExtRuleConfig
				if rawExtMap, err := json.Marshal(oldPathConfig.ExtensionMap); err == nil {
					if json.Unmarshal(rawExtMap, &newFormatArray) == nil {
						newPathConfig.ExtensionMap = newFormatArray
					}
				}
			}
		}

		// 更新配置
		config.MAP[path] = newPathConfig
	}

	// 如果有配置变更，保存回文件
	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return err
	}

	if configChanged {
		log.Printf("[Init] 配置文件已成功迁移到新格式并保存: %s", configPath)
	} else {
		log.Printf("[Init] 配置文件格式已规范化: %s", configPath)
	}

	return nil
}
