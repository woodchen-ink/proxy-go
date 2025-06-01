package config

import (
	"strings"
	"sync"
)

type Config struct {
	MAP         map[string]PathConfig `json:"MAP"` // 路径映射配置
	Compression CompressionConfig     `json:"Compression"`
}

type PathConfig struct {
	DefaultTarget string          `json:"DefaultTarget"` // 默认目标URL
	ExtensionMap  []ExtRuleConfig `json:"ExtensionMap"`  // 扩展名映射规则
	ExtRules      []ExtensionRule `json:"-"`             // 内部使用，存储处理后的扩展名规则
	RedirectMode  bool            `json:"RedirectMode"`  // 是否使用302跳转模式

	// 缓存相关字段（不参与JSON序列化）
	matcherCache    interface{}  `json:"-"` // 缓存的ExtensionMatcher，使用interface{}避免循环导入
	matcherCacheMux sync.RWMutex `json:"-"` // 缓存读写锁
	cacheValid      bool         `json:"-"` // 缓存是否有效
}

// ExtensionRule 表示一个扩展名映射规则（内部使用）
type ExtensionRule struct {
	Extensions    []string // 支持的扩展名列表
	Target        string   // 目标服务器
	SizeThreshold int64    // 最小阈值
	MaxSize       int64    // 最大阈值
	RedirectMode  bool     // 是否使用302跳转模式
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
	RedirectMode  bool   `json:"RedirectMode"`  // 是否使用302跳转模式
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
			RedirectMode:  rule.RedirectMode,
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

	// 清除缓存，因为规则已经改变
	p.InvalidateMatcherCache()
}

// InvalidateMatcherCache 清除ExtensionMatcher缓存
func (p *PathConfig) InvalidateMatcherCache() {
	p.matcherCacheMux.Lock()
	defer p.matcherCacheMux.Unlock()
	p.matcherCache = nil
	p.cacheValid = false
}

// GetMatcherCache 获取缓存的ExtensionMatcher
func (p *PathConfig) GetMatcherCache() (interface{}, bool) {
	p.matcherCacheMux.RLock()
	defer p.matcherCacheMux.RUnlock()
	return p.matcherCache, p.cacheValid
}

// SetMatcherCache 设置ExtensionMatcher缓存
func (p *PathConfig) SetMatcherCache(matcher interface{}) {
	p.matcherCacheMux.Lock()
	defer p.matcherCacheMux.Unlock()
	p.matcherCache = matcher
	p.cacheValid = true
}

// GetProcessedExtTarget 快速获取扩展名对应的目标URL，如果存在返回true
func (p *PathConfig) GetProcessedExtTarget(ext string) (string, bool) {
	if p.ExtRules == nil {
		return "", false
	}

	for _, rule := range p.ExtRules {
		for _, e := range rule.Extensions {
			if e == ext {
				return rule.Target, true
			}
		}
	}

	return "", false
}

// GetProcessedExtRule 获取扩展名对应的完整规则信息，包括RedirectMode
func (p *PathConfig) GetProcessedExtRule(ext string) (*ExtensionRule, bool) {
	if p.ExtRules == nil {
		return nil, false
	}

	for _, rule := range p.ExtRules {
		for _, e := range rule.Extensions {
			if e == ext {
				return &rule, true
			}
		}
	}

	return nil, false
}
