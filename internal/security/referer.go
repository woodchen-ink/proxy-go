package security

import (
	"net/url"
	"strings"
)

// RefererMatcher 判定一个 Referer 是否命中 host 名单
// 匹配规则: 名单 host 走后缀语义, "x.com" 命中自身及所有子域 (foo.x.com / a.b.x.com),
// 但不会误伤 "evilx.com" — 通过 hostname == h || endsWith("."+h) 双判定实现。
//
// 支持两种模式:
//   - 黑名单 (默认): 命中名单即拒绝, 未命中放行
//   - 白名单: 命中名单才放行, 未命中一律拒绝 (用于"仅供自己站点引用"的强隔离场景)
//
// 两种模式下 blockEmpty 语义一致: 控制空 Referer (curl / 直接访问 / 无 Referer 的 App 内嵌) 是否拒绝。
//
// 该类型本身无锁, 上游需要在配置热更新时整体替换实例 (Compile 返回新对象), 而不是原地修改。
type RefererMatcher struct {
	hosts      []string
	blockEmpty bool
	whitelist  bool
}

// Compile 用一份配置生成黑名单 matcher; 空白条目自动剔除, host 统一小写并去掉端口
func Compile(hosts []string, blockEmpty bool) *RefererMatcher {
	return compile(hosts, blockEmpty, false)
}

// CompileWhitelist 用一份配置生成白名单 matcher: 只有命中 hosts 才放行, 其余 (含未知 host) 一律拒绝
func CompileWhitelist(hosts []string, blockEmpty bool) *RefererMatcher {
	return compile(hosts, blockEmpty, true)
}

func compile(hosts []string, blockEmpty bool, whitelist bool) *RefererMatcher {
	cleaned := make([]string, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(strings.ToLower(h))
		if h == "" {
			continue
		}
		// 用户可能粘成 "*.bad.com" / "https://bad.com" / "bad.com:8080" / "bad.com/" 之类形式,
		// 统一归一为纯 host
		h = stripScheme(h)
		h = stripPort(h)
		h = strings.TrimPrefix(h, "*.")
		h = strings.TrimSuffix(h, "/")
		h = strings.TrimSuffix(h, ".")
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		cleaned = append(cleaned, h)
	}
	return &RefererMatcher{hosts: cleaned, blockEmpty: blockEmpty, whitelist: whitelist}
}

// IsBlocked 给定原始 Referer 头返回是否应该拒绝
// 空 Referer 仅在 blockEmpty 时拒绝; 非空但解析失败 (畸形 URL) 一律视为空处理, 走 blockEmpty 分支
// 白名单模式下语义取反: 命中名单放行, 未命中 (含无法解析出 host) 一律拒绝
func (m *RefererMatcher) IsBlocked(referer string) bool {
	if m == nil {
		return false
	}
	if referer == "" {
		return m.blockEmpty
	}
	host := extractHost(referer)
	if host == "" {
		return m.blockEmpty
	}
	matched := false
	for _, h := range m.hosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			matched = true
			break
		}
	}
	if m.whitelist {
		return !matched
	}
	return matched
}

// HasRules 是否配置了任何拦截条件, 用于上游决定要不要走这个 matcher
// 白名单模式下即使 hosts 为空也需要生效 (空白名单 = 拒绝所有带 Referer 的请求), 因此总是有效
func (m *RefererMatcher) HasRules() bool {
	if m == nil {
		return false
	}
	return m.whitelist || len(m.hosts) > 0 || m.blockEmpty
}

func extractHost(referer string) string {
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func stripScheme(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[i+3:]
	}
	return s
}

func stripPort(s string) string {
	// 取第一个 path / port 分隔符之前的部分
	if i := strings.IndexAny(s, ":/?#"); i >= 0 {
		return s[:i]
	}
	return s
}
