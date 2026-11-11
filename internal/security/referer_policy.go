package security

import "strings"

// RefererPolicy 一个作用域 (全局或某个路径映射) 的 Referer 规则: 默认规则 + 按子路径的例外规则
//
// 例外规则按顺序匹配请求路径, 先命中先用, 命中后整体替换默认规则 (可以收紧, 也可以对某子路径放开);
// 都不命中走默认规则。只对本节点生效的例外在构建时已经筛好, 这里不再看节点。
type RefererPolicy struct {
	def    *RefererMatcher
	scoped []scopedMatcher
}

type scopedMatcher struct {
	patterns []string // 空 = 匹配全部路径 (只按节点限定的例外)
	m        *RefererMatcher
}

// NewRefererPolicy 以默认规则构建; def 为 nil 表示默认不检查
func NewRefererPolicy(def *RefererMatcher) *RefererPolicy {
	return &RefererPolicy{def: def}
}

// AddScoped 追加一条例外; m 没有规则 (例外里关闭了检查) 时表示该范围内不做 Referer 检查
func (p *RefererPolicy) AddScoped(patterns []string, m *RefererMatcher) {
	clean := make([]string, 0, len(patterns))
	for _, s := range patterns {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	p.scoped = append(p.scoped, scopedMatcher{patterns: clean, m: m})
}

// HasRules 默认规则或任一例外有实际规则时才需要在请求路径上挂这条策略
func (p *RefererPolicy) HasRules() bool {
	if p == nil {
		return false
	}
	if p.def.HasRules() {
		return true
	}
	for _, s := range p.scoped {
		if s.m.HasRules() {
			return true
		}
	}
	return false
}

// IsBlocked 按请求路径选规则后判断 Referer
func (p *RefererPolicy) IsBlocked(path, referer string) bool {
	if p == nil {
		return false
	}
	m := p.def
	for _, s := range p.scoped {
		if MatchPathPatterns(s.patterns, path) {
			m = s.m
			break
		}
	}
	return m.HasRules() && m.IsBlocked(referer)
}

// MatchPathPatterns 路径是否命中任一模式; 模式列表为空视为全部命中
//   - 以 * 结尾: 前缀匹配, "/b2/random-video*" 命中 /b2/random-video.mp4、/b2/random-video/1.mp4
//   - 其余: 精确匹配或其子路径, "/b2/videos" 命中 /b2/videos 与 /b2/videos/a.mp4, 不命中 /b2/videos2
func MatchPathPatterns(patterns []string, path string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(path, prefix) {
				return true
			}
			continue
		}
		p = strings.TrimSuffix(p, "/")
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
