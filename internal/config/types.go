package config

import (
	"encoding/json"
	"strings"
)

type Config struct {
	MAP         map[string]PathConfig `json:"MAP"` // 改为使用PathConfig
	Compression CompressionConfig     `json:"Compression"`
}

type PathConfig struct {
	Path            string            `json:"Path"`
	DefaultTarget   string            `json:"DefaultTarget"`
	ExtensionMap    map[string]string `json:"ExtensionMap"`
	SizeThreshold   int64             `json:"SizeThreshold"` // 最小文件大小阈值
	MaxSize         int64             `json:"MaxSize"`       // 最大文件大小阈值
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

// 添加一个辅助方法来处理字符串到 PathConfig 的转换
func (c *Config) UnmarshalJSON(data []byte) error {
	// 创建一个临时结构来解析原始JSON
	type TempConfig struct {
		MAP         map[string]json.RawMessage `json:"MAP"`
		Compression CompressionConfig          `json:"Compression"`
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

	return nil
}

// 添加处理扩展名映射的方法
func (p *PathConfig) ProcessExtensionMap() {
	if p.ExtensionMap == nil {
		p.processedExtMap = nil
		return
	}

	// 重新创建processedExtMap，确保它是最新的
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
