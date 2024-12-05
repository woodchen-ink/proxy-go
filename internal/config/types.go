package config

import (
	"encoding/json"
	"strings"
	"time"
)

type Config struct {
	MAP         map[string]PathConfig `json:"MAP"` // 改为使用PathConfig
	Compression CompressionConfig     `json:"Compression"`
	FixedPaths  []FixedPathConfig     `json:"FixedPaths"`
	Metrics     MetricsConfig         `json:"Metrics"`
}

type PathConfig struct {
	DefaultTarget   string            `json:"DefaultTarget"` // 默认回源地址
	ExtensionMap    map[string]string `json:"ExtensionMap"`  // 特定后缀的回源地址
	SizeThreshold   int64             `json:"SizeThreshold"` // 文件大小阈值(字节)，超过此大小才使用ExtensionMap
	processedExtMap map[string]string // 内部使用，存储拆分后的映射
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

type MetricsConfig struct {
	Password      string `json:"Password"`
	TokenExpiry   int    `json:"TokenExpiry"`
	FeishuWebhook string `json:"FeishuWebhook"`
	// 监控告警配置
	Alert struct {
		WindowSize     int           `json:"WindowSize"`     // 监控窗口数量
		WindowInterval time.Duration `json:"WindowInterval"` // 每个窗口时间长度
		DedupeWindow   time.Duration `json:"DedupeWindow"`   // 告警去重时间窗口
		MinRequests    int64         `json:"MinRequests"`    // 触发告警的最小请求数
		ErrorRate      float64       `json:"ErrorRate"`      // 错误率告警阈值
		AlertInterval  time.Duration `json:"AlertInterval"`  // 告警间隔时间
	} `json:"Alert"`
	// 延迟告警配置
	Latency struct {
		SmallFileSize  int64         `json:"SmallFileSize"`  // 小文件阈值
		MediumFileSize int64         `json:"MediumFileSize"` // 中等文件阈值
		LargeFileSize  int64         `json:"LargeFileSize"`  // 大文件阈值
		SmallLatency   time.Duration `json:"SmallLatency"`   // 小文件最大延迟
		MediumLatency  time.Duration `json:"MediumLatency"`  // 中等文件最大延迟
		LargeLatency   time.Duration `json:"LargeLatency"`   // 大文件最大延迟
		HugeLatency    time.Duration `json:"HugeLatency"`    // 超大文件最大延迟
	} `json:"Latency"`
	// 加载配置
	Load struct {
		RetryCount    int           `json:"retry_count"`
		RetryInterval time.Duration `json:"retry_interval"`
		Timeout       time.Duration `json:"timeout"`
	} `json:"load"`
	// 保存配置
	Save struct {
		MinInterval     time.Duration `json:"min_interval"`
		MaxInterval     time.Duration `json:"max_interval"`
		DefaultInterval time.Duration `json:"default_interval"`
	} `json:"save"`
	// 验证配置
	Validation struct {
		MaxErrorRate     float64 `json:"max_error_rate"`
		MaxDataDeviation float64 `json:"max_data_deviation"`
	} `json:"validation"`
}

// 添加一个辅助方法来处理字符串到 PathConfig 的转换
func (c *Config) UnmarshalJSON(data []byte) error {
	// 创建一个临时结构来解析原始JSON
	type TempConfig struct {
		MAP         map[string]json.RawMessage `json:"MAP"`
		Compression CompressionConfig          `json:"Compression"`
		FixedPaths  []FixedPathConfig          `json:"FixedPaths"`
		Metrics     MetricsConfig              `json:"Metrics"`
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
			pathConfig := PathConfig{
				DefaultTarget: strValue,
			}
			pathConfig.ProcessExtensionMap() // 处理扩展名映射
			c.MAP[key] = pathConfig
			continue
		}

		// 如果不是字符串，尝试作为PathConfig解析
		var pathConfig PathConfig
		if err := json.Unmarshal(raw, &pathConfig); err != nil {
			return err
		}
		pathConfig.ProcessExtensionMap() // 处理扩展名映射
		c.MAP[key] = pathConfig
	}

	// 复制其他字段
	c.Compression = temp.Compression
	c.FixedPaths = temp.FixedPaths
	c.Metrics = temp.Metrics

	return nil
}

// 添加处理扩展名映射的方法
func (p *PathConfig) ProcessExtensionMap() {
	if p.ExtensionMap == nil {
		return
	}

	p.processedExtMap = make(map[string]string)
	for exts, target := range p.ExtensionMap {
		// 分割扩展名
		for _, ext := range strings.Split(exts, ",") {
			ext = strings.TrimSpace(ext) // 移除可能的空格
			if ext != "" {
				p.processedExtMap[ext] = target
			}
		}
	}
}

// 添加获取目标URL的方法
func (p *PathConfig) GetTargetForExt(ext string) string {
	if p.processedExtMap == nil {
		p.ProcessExtensionMap()
	}
	if target, exists := p.processedExtMap[ext]; exists {
		return target
	}
	return p.DefaultTarget
}

// 添加检查扩展名是否存在的方法
func (p *PathConfig) GetExtensionTarget(ext string) (string, bool) {
	if p.processedExtMap == nil {
		p.ProcessExtensionMap()
	}
	target, exists := p.processedExtMap[ext]
	return target, exists
}
