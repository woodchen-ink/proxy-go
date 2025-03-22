package config

import (
	"strings"
)

type Config struct {
	MAP         map[string]PathConfig `json:"MAP"` // 路径映射配置
	Compression CompressionConfig     `json:"Compression"`
}

type PathConfig struct {
	DefaultTarget string          `json:"DefaultTarget"` // 默认目标URL
	ExtensionMap  []ExtRuleConfig `json:"ExtensionMap"`  // 扩展名映射规则
	ExtRules      []ExtensionRule `json:"-"`             // 内部使用，存储处理后的扩展名规则
}

// ExtensionRule 表示一个扩展名映射规则（内部使用）
type ExtensionRule struct {
	Extensions    []string // 支持的扩展名列表
	Target        string   // 目标服务器
	SizeThreshold int64    // 最小阈值
	MaxSize       int64    // 最大阈值
}

type CompressionConfig struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}

// 扩展名映射配置结构
type ExtRuleConfig struct {
	Extensions    string `json:"Extensions"`    // 逗号分隔的扩展名
	Target        string `json:"Target"`        // 目标服务器
	SizeThreshold int64  `json:"SizeThreshold"` // 最小阈值
	MaxSize       int64  `json:"MaxSize"`       // 最大阈值
}

// 处理扩展名映射的方法
func (p *PathConfig) ProcessExtensionMap() {
	p.ExtRules = nil

	if p.ExtensionMap == nil {
		return
	}

	// 处理扩展名规则
	for _, rule := range p.ExtensionMap {
		extRule := ExtensionRule{
			Target:        rule.Target,
			SizeThreshold: rule.SizeThreshold,
			MaxSize:       rule.MaxSize,
		}

		// 处理扩展名列表
		for _, ext := range strings.Split(rule.Extensions, ",") {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				extRule.Extensions = append(extRule.Extensions, ext)
			}
		}

		if len(extRule.Extensions) > 0 {
			p.ExtRules = append(p.ExtRules, extRule)
		}
	}
}
