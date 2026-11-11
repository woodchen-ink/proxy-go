package config

import "sync/atomic"

// nodeIDResolver 返回当前节点 ID, 由启动流程注入 (pkg/sync.NodeID); config 包不直接依赖同步层
var nodeIDResolver atomic.Pointer[func() string]

// SetNodeIDResolver 注入节点 ID 解析函数, Referer 例外规则 (RefererBanConfig.RulesForNode) 据此筛选
func SetNodeIDResolver(f func() string) {
	nodeIDResolver.Store(&f)
}

// CurrentNodeID 当前节点 ID; 未注入时为空串 (此时任何按节点覆盖都不命中, 走默认规则)
func CurrentNodeID() string {
	if f := nodeIDResolver.Load(); f != nil {
		return (*f)()
	}
	return ""
}
