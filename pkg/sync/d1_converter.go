package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ConvertConfigFromFile 从 config.json 文件转换为 ConfigMap 和 ConfigOther
// 仅在 D1 不可用回退到本地 fallback 时使用
func ConvertConfigFromFile(filePath string) ([]ConfigMap, []ConfigOther, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	now := time.Now().UnixMilli()
	var maps []ConfigMap
	var others []ConfigOther

	// 转换 MAP (路径配置)
	if mapData, ok := config["MAP"].(map[string]any); ok {
		for path, value := range mapData {
			mapConfig, ok := value.(map[string]any)
			if !ok {
				continue
			}

			// 提取基本字段
			defaultTarget, _ := mapConfig["DefaultTarget"].(string)

			// Enabled 字段：如果不存在则默认为 true (1)
			enabled := 1
			if enabledVal, ok := mapConfig["Enabled"]; ok {
				if boolVal, ok := enabledVal.(bool); ok {
					if boolVal {
						enabled = 1
					} else {
						enabled = 0
					}
				}
			}

			// 转换 ExtensionMap 为 JSON 字符串
			var extensionRules string
			if extMap, ok := mapConfig["ExtensionMap"]; ok && extMap != nil {
				extJSON, _ := json.Marshal(extMap)
				extensionRules = string(extJSON)
			}

			// 转换 CacheConfig 为 JSON 字符串
			var cacheConfig string
			if cc, ok := mapConfig["CacheConfig"]; ok && cc != nil {
				ccJSON, _ := json.Marshal(cc)
				cacheConfig = string(ccJSON)
			}

			// extra_config: 承载没有专属列的 PathConfig 字段, 避免 D1 上下行丢失。
			// 只持久化非零值, 让历史单源 / 无这些字段的配置保持 extra_config 为空。
			extraConfig := buildExtraConfig(mapConfig)

			maps = append(maps, ConfigMap{
				Path:           path,
				DefaultTarget:  defaultTarget,
				Enabled:        enabled,
				ExtensionRules: extensionRules,
				CacheConfig:    cacheConfig,
				ExtraConfig:    extraConfig,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		}
	}

	// 转换其他配置
	for key, value := range config {
		if key == "MAP" {
			continue // MAP 已经单独处理
		}

		valueJSON, err := json.Marshal(value)
		if err != nil {
			continue
		}

		others = append(others, ConfigOther{
			Key:       key,
			Value:     string(valueJSON),
			UpdatedAt: now,
		})
	}

	return maps, others, nil
}

// extraConfigKeys 是 config_maps 中没有专属列、需通过 extra_config 列持久化的 PathConfig 字段。
// 加新的"无专属列字段"时在此追加, 上下行 (buildExtraConfig / mergeExtraConfig) 自动覆盖。
var extraConfigKeys = []string{"DefaultTargets", "RedirectMode", "CFImageOpt", "RefererBan"}

// buildExtraConfig 从单路径配置中提取无专属列字段, 序列化为 extra_config JSON。
// 只收集存在且非"零值"的字段, 让历史 / 单源配置保持 extra_config 为空, 不污染存量数据。
func buildExtraConfig(mapConfig map[string]any) string {
	extra := make(map[string]any)
	for _, key := range extraConfigKeys {
		v, ok := mapConfig[key]
		if !ok || isZeroValue(v) {
			continue
		}
		extra[key] = v
	}
	if len(extra) == 0 {
		return ""
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return ""
	}
	return string(b)
}

// isZeroValue 判断 JSON 反序列化后的值是否为"空/零", 用于决定是否写入 extra_config。
// 覆盖 nil / false / 空字符串 / 空数组 / 空对象。
func isZeroValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case bool:
		return !val
	case string:
		return val == ""
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	default:
		return false
	}
}
