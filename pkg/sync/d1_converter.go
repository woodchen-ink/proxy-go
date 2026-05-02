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

			maps = append(maps, ConfigMap{
				Path:           path,
				DefaultTarget:  defaultTarget,
				Enabled:        enabled,
				ExtensionRules: extensionRules,
				CacheConfig:    cacheConfig,
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
