package config

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
func NewPathConfig(target interface{}) PathConfig {
	switch v := target.(type) {
	case string:
		// 简单字符串格式
		return PathConfig{
			DefaultTarget: v,
		}
	case map[string]interface{}:
		// 完整配置格式
		config := PathConfig{
			DefaultTarget: v["DefaultTarget"].(string),
		}
		if extMap, ok := v["ExtensionMap"].(map[string]interface{}); ok {
			config.ExtensionMap = make(map[string]string)
			for ext, target := range extMap {
				config.ExtensionMap[ext] = target.(string)
			}
		}
		return config
	default:
		// 处理异常情况
		return PathConfig{}
	}
}
