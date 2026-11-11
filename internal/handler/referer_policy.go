package handler

import (
	"proxy-go/internal/config"
	"proxy-go/internal/security"
)

// BuildRefererPolicy 由 RefererBanConfig 构建本节点上的 Referer 策略 (默认规则 + 本节点生效的例外规则)
// allowWhitelist=false 时 (全局规则) 一律按黑名单编译; 返回 nil 表示无需检查
func BuildRefererPolicy(rb *config.RefererBanConfig, nodeID string, allowWhitelist bool) *security.RefererPolicy {
	if rb == nil {
		return nil
	}
	compile := func(enabled bool, mode string, hosts []string, blockEmpty bool) *security.RefererMatcher {
		if !enabled {
			return nil
		}
		if allowWhitelist && mode == config.RefererModeWhitelist {
			return security.CompileWhitelist(hosts, blockEmpty)
		}
		return security.Compile(hosts, blockEmpty)
	}
	p := security.NewRefererPolicy(compile(rb.Enabled, rb.Mode, rb.Hosts, rb.BlockEmpty))
	for _, o := range rb.RulesForNode(nodeID) {
		p.AddScoped(o.Paths, compile(o.Enabled, o.Mode, o.Hosts, o.BlockEmpty))
	}
	if !p.HasRules() {
		return nil
	}
	return p
}
