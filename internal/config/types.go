package config

import (
	"strconv"
	"strings"
)

// MirrorTargetMarker 是 /mirror 系统路径的 DefaultTarget 哨兵值 (非真实 URL)。
// 真正的 mirror 转发走 "/mirror/" 前缀路由 (与 MAP 配置无关), 这个 MAP 条目只是
// ConfigManager.ensureMirrorPath 自动补的占位符, 供后台展示/挂缓存配置用。
// 校验配置时必须识别并跳过它, 否则会被当成非法 URL 拒绝。
const MirrorTargetMarker = "mirror"

// MirrorPathKey 是 /mirror 系统路径在 MAP 中的 key。
// mirror 转发本身走 "/mirror/" 前缀路由, 不经过 MAP 匹配, 但缓存类配置挂在这个条目上。
const MirrorPathKey = "/mirror"

type Config struct {
	MAP         map[string]PathConfig `json:"MAP"` // 路径映射配置
	Compression CompressionConfig     `json:"Compression"`
	Security    SecurityConfig        `json:"Security"`    // 安全配置
	Cache       CacheConfig           `json:"Cache"`       // 缓存配置
	MirrorCache CacheConfig           `json:"MirrorCache"` // 镜像缓存配置
	FaviconURL  string                `json:"FaviconURL"`  // Favicon URL (可选)，支持环境变量 FAVICON_URL 覆盖
	CDN         CDNConfig             `json:"CDN"`         // 外部 CDN 缓存清理配置 (Cloudflare / EdgeOne 等)
}

// CDNConfig 外部 CDN 缓存清理配置
// 允许配置多个 provider, 但同时只能启用一个 (Enabled=true 唯一)
type CDNConfig struct {
	Providers []CDNProvider `json:"Providers"`
}

// CDNProvider 单个 CDN 厂商配置
// Type 取值: "cloudflare" / "edgeone"
// Credentials 按 Type 解释 (cloudflare: apiToken/zoneId; edgeone: secretId/secretKey/zoneId)
type CDNProvider struct {
	ID          string            `json:"ID"`
	Name        string            `json:"Name"`
	Type        string            `json:"Type"`
	Enabled     bool              `json:"Enabled"`
	Credentials map[string]string `json:"Credentials"`
}

type PathConfig struct {
	DefaultTarget string `json:"DefaultTarget"` // 默认目标URL (单源; 多源时等于 DefaultTargets[0])
	// DefaultTargets 有序回源列表, 第一个为主源, 后续为备源。
	// 请求失败时按序回落 (failover): 连接级失败 / 5xx 网关错误 / 404 等触发切换到下一个源。
	// 为空时退化为单源, 使用 DefaultTarget。取源统一走 GetTargets(), 不要直接读这两个字段。
	DefaultTargets []string        `json:"DefaultTargets,omitempty"`
	ExtensionMap   []ExtRuleConfig `json:"ExtensionMap"` // 扩展名映射规则
	ExtRules       []ExtensionRule `json:"-"`            // 内部使用，存储处理后的扩展名规则
	RedirectMode   bool            `json:"RedirectMode"` // 是否使用302跳转模式
	Enabled        bool            `json:"Enabled"`      // 是否启用此路径映射，默认true
	CacheConfig    *CacheConfig    `json:"CacheConfig"`  // 独立缓存配置，为nil则使用全局配置
	// RefererBan 路径级引用来源黑/白名单。黑名单模式与全局 SecurityConfig.RefererBan (始终黑名单) 叠加, 任一命中即拒;
	// 白名单模式为路径独占语义, 只放行命中 Hosts 的 Referer, 不与全局黑名单做"任一命中"的宽松叠加, 全局命中依然照常拒绝
	RefererBan *RefererBanConfig `json:"RefererBan"`
	// RefererRedirect 路径级引用来源 302 重定向: 命中指定 host 名单时把请求 302 分流到另一个目标前缀,
	// 用于"某引用站不走当前 CDN, 改走另一个 CDN"的场景。命中重定向优先于 RefererBan (跳走就不再判封禁)。
	RefererRedirect *RefererRedirectConfig `json:"RefererRedirect"`
	// CFImageOpt 启用 Cloudflare Images 友好的缓存键策略:
	// 图片请求按 Accept 头中的图片格式 (avif/webp/jpeg/...) + 标准化后的浏览器类型分桶缓存,
	// 避免不同设备拿到对方格式。源站不做格式协商时, 务必保持 false,
	// 此时所有请求共享同一份原文件缓存, 命中率显著更高。
	CFImageOpt bool `json:"CFImageOpt"`
}

// ExtensionRule 表示一个扩展名映射规则（内部使用）
type ExtensionRule struct {
	Extensions    []string // 支持的扩展名列表
	Target        string   // 目标服务器
	SizeThreshold int64    // 最小阈值
	MaxSize       int64    // 最大阈值
	RedirectMode  bool     // 是否使用302跳转模式
	Domains       []string // 支持的域名列表，为空表示匹配所有域名
}

type CompressionConfig struct {
	Gzip   CompressorConfig `json:"Gzip"`
	Brotli CompressorConfig `json:"Brotli"`
}

type CompressorConfig struct {
	Enabled bool `json:"Enabled"`
	Level   int  `json:"Level"`
}

type SecurityConfig struct {
	IPBan      IPBanConfig      `json:"IPBan"`      // IP封禁配置
	RefererBan RefererBanConfig `json:"RefererBan"` // 引用来源 (Referer host) 黑名单, 全局生效
}

// RefererBanConfig 引用来源 host 黑白名单
// Hosts 中的条目按 host 后缀语义匹配 (e.g. "x.com" 同时命中 "x.com" / "foo.x.com" / "a.b.x.com")
// BlockEmpty 控制空 Referer 是否一并拦截; 默认 false (放行), 避免误伤 curl / 直接访问 / Telegram 预览等场景;
// 该开关在黑白两种模式下语义一致 (是否拒绝空 Referer), 不随 Mode 改变
// Mode 取值 "blacklist" (默认, 命中 Hosts 即拒) / "whitelist" (只有命中 Hosts 才放行, 用于资源仅供自己站点引用的强隔离场景);
// 空字符串按 "blacklist" 处理, 兼容历史配置
type RefererBanConfig struct {
	Enabled    bool     `json:"Enabled"`
	Mode       string   `json:"Mode"`
	Hosts      []string `json:"Hosts"`
	BlockEmpty bool     `json:"BlockEmpty"`
}

// IsWhitelist 是否为白名单模式
func (c *RefererBanConfig) IsWhitelist() bool {
	return c != nil && c.Mode == RefererModeWhitelist
}

const (
	RefererModeBlacklist = "blacklist"
	RefererModeWhitelist = "whitelist"
)

// RefererRedirectConfig 路径级引用来源 302 重定向配置
// 多组规则按 Rules 顺序匹配, 第一个命中的规则决定目标 (先命中先跳)。
// host 匹配与 RefererBanConfig.Hosts 一致, 走后缀语义 (含子域), 由 security.RefererMatcher 实现。
// 空 Referer 不参与分流 (不命中任何规则), 避免误伤直接访问 / curl。
type RefererRedirectConfig struct {
	Enabled bool                  `json:"Enabled"`
	Rules   []RefererRedirectRule `json:"Rules"`
}

// RefererRedirectRule 单条重定向规则: Referer host 命中 Hosts 时, 302 到 Target 拼出的目标
// Target 为目标前缀 (如 "https://cdn2.com"), 最终 URL = Target + 剥掉路径前缀后的子路径 + 原 query
type RefererRedirectRule struct {
	Hosts  []string `json:"Hosts"`
	Target string   `json:"Target"`
}

type IPBanConfig struct {
	Enabled                bool `json:"Enabled"`                // 是否启用IP封禁
	ErrorThreshold         int  `json:"ErrorThreshold"`         // 404错误阈值
	WindowMinutes          int  `json:"WindowMinutes"`          // 统计窗口时间（分钟）
	BanDurationMinutes     int  `json:"BanDurationMinutes"`     // 封禁时长（分钟）
	CleanupIntervalMinutes int  `json:"CleanupIntervalMinutes"` // 清理间隔（分钟）
}

type CacheConfig struct {
	MaxAge       int64 `json:"max_age"`        // 最大缓存时间（分钟）
	CleanupTick  int64 `json:"cleanup_tick"`   // 清理间隔（分钟）
	MaxCacheSize int64 `json:"max_cache_size"` // 最大缓存大小（GB）
}

// 缓存兜底默认值: 配置缺失 / 为 0 时使用。
// CacheManager 与客户端缓存头必须共用同一份默认值, 否则会出现
// "本地按 30 分钟缓存, 却不告诉浏览器缓存多久" 这类口径不一致。
const (
	DefaultCacheMaxAge      int64 = 30 // 分钟
	DefaultCacheCleanupTick int64 = 5  // 分钟
	DefaultCacheMaxSizeGB   int64 = 10 // GB
)

// IsNoCache 表示这份配置显式要求"不缓存"。
//
// 区分"没配"与"配了 0":
//   - c == nil    : 没配 -> 继承上一级 (路径级缺失就用全局)
//   - MaxAge == 0 : 显式关闭缓存 -> 不写本地缓存, 且下发 no-store
//   - MaxAge < 0  : 非法值, 按没配处理, 回退默认值
//
// 因此调用方必须先判 IsNoCache 再取 EffectiveMaxAge, 不能只看时长。
func (c *CacheConfig) IsNoCache() bool {
	return c != nil && c.MaxAge == 0
}

// EffectiveMaxAge 返回实际生效的缓存分钟数: 配置缺失或为非法负值时回退到默认值,
// 与 CacheManager 的兜底行为保持一致。
//
// 注意: MaxAge == 0 (显式不缓存) 也会返回默认值, 因为这个方法只回答"缓存多久",
// 不回答"要不要缓存" —— 后者由 IsNoCache 判定, 调用方需先判它。
func (c *CacheConfig) EffectiveMaxAge() int64 {
	if c == nil || c.MaxAge <= 0 {
		return DefaultCacheMaxAge
	}
	return c.MaxAge
}

// ClientCacheControl 按本路径的缓存时长, 生成下发给浏览器 / CDN 的 Cache-Control 值。
//
// 语义: 我们自己只保证缓存 MaxAge 分钟, 就让下游也缓存同样的时长 —— 超过这个时间我们
// 都要回源重取了, 没有理由让浏览器继续用旧副本。这样"缓存多久"全局只有一个可调项,
// 不需要再维护一份独立的客户端策略。
//
// 显式配 0 表示不缓存, 下发 no-store; 配置缺失 / 非法负值按 DefaultCacheMaxAge 兜底,
// 与 CacheManager 的行为一致, 保证 "本地缓存多久" 与 "告诉浏览器缓存多久" 永远是同一个数。
//
// 注意: "0 = 不缓存" 只适用于路径级配置。全局配置请用 CacheControlWithDefault,
// 它把 0 当作漏配而非全站禁用缓存 (与 CacheManager 的兜底一致)。
func (c *CacheConfig) ClientCacheControl() string {
	if c.IsNoCache() {
		return "no-store, no-cache, must-revalidate"
	}
	return c.CacheControlWithDefault()
}

// CacheControlWithDefault 只按时长生成 Cache-Control, 不解读 "0 = 不缓存"。
// 用于全局配置这一级: 漏配 max_age 不应静默关掉全站缓存。
func (c *CacheConfig) CacheControlWithDefault() string {
	return "public, max-age=" + strconv.FormatInt(c.EffectiveMaxAge()*60, 10)
}

// 扩展名映射配置结构
type ExtRuleConfig struct {
	Extensions    string `json:"Extensions"`    // 逗号分隔的扩展名
	Target        string `json:"Target"`        // 目标服务器
	SizeThreshold int64  `json:"SizeThreshold"` // 最小阈值
	MaxSize       int64  `json:"MaxSize"`       // 最大阈值
	RedirectMode  bool   `json:"RedirectMode"`  // 是否使用302跳转模式
	Domains       string `json:"Domains"`       // 逗号分隔的域名列表，为空表示匹配所有域名
}

// GetTargets 返回有序回源列表 (主源在前, 备源在后)。
// 优先用 DefaultTargets (剔除空白/空项); 为空时退化为单源 DefaultTarget。
// service 层统一通过本方法取源, 避免各处重复写 nil/空判断。
func (p *PathConfig) GetTargets() []string {
	if len(p.DefaultTargets) > 0 {
		targets := make([]string, 0, len(p.DefaultTargets))
		for _, t := range p.DefaultTargets {
			if t = strings.TrimSpace(t); t != "" {
				targets = append(targets, t)
			}
		}
		if len(targets) > 0 {
			return targets
		}
	}
	if p.DefaultTarget != "" {
		return []string{p.DefaultTarget}
	}
	return nil
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

		// 处理域名列表
		if rule.Domains != "" {
			for _, domain := range strings.Split(rule.Domains, ",") {
				domain = strings.TrimSpace(domain)
				if domain != "" {
					extRule.Domains = append(extRule.Domains, domain)
				}
			}
		}

		if len(extRule.Extensions) > 0 {
			p.ExtRules = append(p.ExtRules, extRule)
		}
	}
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
