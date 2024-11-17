package config

import "encoding/json"

type Config struct {
	MAP         map[string]PathConfig `json:"MAP"` // 改为使用PathConfig
	Compression CompressionConfig     `json:"Compression"`
	FixedPaths  []FixedPathConfig     `json:"FixedPaths"`
}

type PathConfig struct {
	DefaultTarget string            `json:"DefaultTarget"` // 默认回源地址
	ExtensionMap  map[string]string `json:"ExtensionMap"`  // 特定后缀的回源地址
}

type CompressionConfig struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}

type FixedPathConfig struct {
	Path       string `json:"Path"`
	TargetHost string `json:"TargetHost"`
	TargetURL  string `json:"TargetURL"`
}

// 添加一个辅助方法来处理字符串到 PathConfig 的转换
func (c *Config) UnmarshalJSON(data []byte) error {
	// 创建一个临时结构来解析原始JSON
	type TempConfig struct {
		MAP         map[string]json.RawMessage `json:"MAP"`
		Compression CompressionConfig          `json:"Compression"`
		FixedPaths  []FixedPathConfig          `json:"FixedPaths"`
	}

	var temp TempConfig
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// 初始化 MAP
	c.MAP = make(map[string]PathConfig)

	// 处理每个路径配置
	for key, raw := range temp.MAP {
		// 尝试作为字符串解析
		var strValue string
		if err := json.Unmarshal(raw, &strValue); err == nil {
			c.MAP[key] = PathConfig{
				DefaultTarget: strValue,
			}
			continue
		}

		// 如果不是字符串，尝试作为PathConfig解析
		var pathConfig PathConfig
		if err := json.Unmarshal(raw, &pathConfig); err != nil {
			return err
		}
		c.MAP[key] = pathConfig
	}

	// 复制其他字段
	c.Compression = temp.Compression
	c.FixedPaths = temp.FixedPaths

	return nil
}
