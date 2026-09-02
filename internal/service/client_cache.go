package service

import (
	"net/http"

	"proxy-go/internal/config"
)

// resolveCacheConfig 返回该路径实际生效的缓存配置: 路径级优先, 缺失时回退全局。
//
// 三级取值与 CacheManager 完全同源 —— 路径级 -> 全局 -> config 包的默认值,
// 否则会出现 "本地已过期回源, 但浏览器还被告知可以继续用" 这类偏差。
// 全局也读不到时返回 nil, 由 EffectiveMaxAge 兜底到默认值。
//
// 路径级显式配 0 (不缓存) 必须原样返回, 不能当成"没配"落到全局 —— 否则
// "这个路径不要缓存" 的意图会被全局配置悄悄覆盖掉。
func resolveCacheConfig(pathCache *config.CacheConfig, global *config.CacheConfig) *config.CacheConfig {
	if pathCache != nil && (pathCache.MaxAge > 0 || pathCache.IsNoCache()) {
		return pathCache
	}
	return global
}

// globalCacheConfig 取全局代理缓存配置, 读不到返回 nil (由调用方兜底)
func globalCacheConfig() *config.CacheConfig {
	cfg := config.GetConfig()
	if cfg == nil {
		return nil
	}
	return &cfg.Cache
}

// ApplyClientCacheHeaders 按本节点的缓存时长设置下发给客户端 (浏览器 / CDN) 的 Cache-Control。
//
// 无条件覆盖上游的 Cache-Control: 上游可能是任意网站 / GitHub / S3, 缓存头不可控,
// 且不影响我们实际留多久, 也就不该影响我们告诉下游留多久。
//
// 必须在 WriteHeader / ServeFile 之前调用, 否则头部不会生效。
func ApplyClientCacheHeaders(w http.ResponseWriter, pathCache *config.CacheConfig) {
	w.Header().Set("Cache-Control", clientCacheControl(pathCache))
	// 上游残留的 Expires 会与新的 max-age 冲突 (Expires 在部分实现里优先), 一并清掉
	w.Header().Del("Expires")
}

// clientCacheControl 计算该路径最终下发的 Cache-Control。
//
// "配 0 = 不缓存" 只在**路径级**成立: 那是针对单个路径的明确选择。
// 全局 Cache.max_age 为 0 时仍按默认值兜底 (与 CacheManager 一致), 不解读为
// "全站禁用缓存" —— 否则一份漏配 max_age 的全局配置会静默关掉所有缓存。
func clientCacheControl(pathCache *config.CacheConfig) string {
	if pathCache.IsNoCache() {
		return "no-store, no-cache, must-revalidate"
	}
	// 走到这里说明路径级没配 (或非法值), 取全局; 全局的 0 按默认值兜底而非不缓存
	return resolveCacheConfig(pathCache, globalCacheConfig()).CacheControlWithDefault()
}

// MirrorPathCacheConfig 取 /mirror 系统路径上配置的缓存时长。
//
// mirror 转发走独立的 "/mirror/" 前缀路由, 不经过 MAP 匹配, 但后台仍把 /mirror 作为
// 一个 MAP 条目展示 (见 config.MirrorTargetMarker), 缓存配置就挂在那个条目上。
func MirrorPathCacheConfig() *config.CacheConfig {
	cfg := config.GetConfig()
	if cfg == nil {
		return nil
	}
	if pathCfg, ok := cfg.MAP[config.MirrorPathKey]; ok && pathCfg.CacheConfig != nil && pathCfg.CacheConfig.MaxAge > 0 {
		return pathCfg.CacheConfig
	}
	// 路径级缺失时回退到 mirror 自己的全局缓存配置 (而非代理的 Cache);
	// 它若也为零值, 由 EffectiveMaxAge 兜底到默认值
	return &cfg.MirrorCache
}
