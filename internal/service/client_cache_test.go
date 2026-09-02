package service

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"proxy-go/internal/cache"
	"proxy-go/internal/config"
)

func TestClientCacheControl(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.CacheConfig
		want string
	}{
		{"nil (没配) 按默认 30 分钟兜底", nil, "public, max-age=1800"},
		{"显式配 0 表示不缓存", &config.CacheConfig{MaxAge: 0}, "no-store, no-cache, must-revalidate"},
		{"负数 (非法) 按默认兜底", &config.CacheConfig{MaxAge: -5}, "public, max-age=1800"},
		{"30 分钟换算为 1800 秒", &config.CacheConfig{MaxAge: 30}, "public, max-age=1800"},
		{"1 分钟", &config.CacheConfig{MaxAge: 1}, "public, max-age=60"},
		{"1440 分钟 (1 天)", &config.CacheConfig{MaxAge: 1440}, "public, max-age=86400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ClientCacheControl(); got != tt.want {
				t.Errorf("ClientCacheControl() = %q, want %q", got, tt.want)
			}
		})
	}
}

// 上游可能是网站 / GitHub / S3, 缓存头不可控, 必须一律以本节点配置为准
func TestApplyClientCacheHeaders_AlwaysOverridesUpstream(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
	}{
		{"上游给了长缓存", "public, max-age=31536000, immutable"},
		{"上游给了 no-store", "no-store"},
		{"上游给了 private", "private, max-age=0, must-revalidate"},
		{"上游没给", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if tt.upstream != "" {
				w.Header().Set("Cache-Control", tt.upstream)
			}
			ApplyClientCacheHeaders(w, &config.CacheConfig{MaxAge: 30})
			if got := w.Header().Get("Cache-Control"); got != "public, max-age=1800" {
				t.Errorf("Cache-Control = %q, want 本节点配置值", got)
			}
		})
	}
}

// 上游残留的 Expires 会与新的 max-age 冲突, 必须清掉
func TestApplyClientCacheHeaders_ClearsUpstreamExpires(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("Expires", "Wed, 21 Oct 2099 07:28:00 GMT")
	w.Header().Set("Cache-Control", "max-age=31536000")

	ApplyClientCacheHeaders(w, &config.CacheConfig{MaxAge: 5})

	if got := w.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("Expires"); got != "" {
		t.Errorf("Expires 应被清除, 实际为 %q", got)
	}
}

// 路径级缺失时回退全局, 保证与 CacheManager 的取值口径一致
func TestResolveCacheConfig_FallsBackToGlobal(t *testing.T) {
	global := &config.CacheConfig{MaxAge: 30}

	t.Run("路径级有效时优先", func(t *testing.T) {
		got := resolveCacheConfig(&config.CacheConfig{MaxAge: 120}, global)
		if got.MaxAge != 120 {
			t.Errorf("MaxAge = %d, want 120", got.MaxAge)
		}
	})

	t.Run("路径级为 nil 时回退全局", func(t *testing.T) {
		if got := resolveCacheConfig(nil, global); got.MaxAge != 30 {
			t.Errorf("MaxAge = %d, want 30", got.MaxAge)
		}
	})

	t.Run("路径级显式 0 不回退全局 (表示不缓存)", func(t *testing.T) {
		got := resolveCacheConfig(&config.CacheConfig{MaxAge: 0}, global)
		if !got.IsNoCache() {
			t.Errorf("显式 0 被全局覆盖, MaxAge = %d", got.MaxAge)
		}
	})

	t.Run("路径级非法负值回退全局", func(t *testing.T) {
		got := resolveCacheConfig(&config.CacheConfig{MaxAge: -1}, global)
		if got.MaxAge != 30 {
			t.Errorf("MaxAge = %d, want 30", got.MaxAge)
		}
	})

	t.Run("全局也缺失时按默认值兜底", func(t *testing.T) {
		// 与 CacheManager 的兜底一致 (30 分钟), 不能什么都不下发
		if got := resolveCacheConfig(nil, nil).ClientCacheControl(); got != "public, max-age=1800" {
			t.Errorf("应兜底到默认 30 分钟, 实际 %q", got)
		}
	})
}

// 回归: 全局 Cache 漏配 (max_age=0) 时, CacheManager 会按默认 30 分钟缓存,
// 客户端头必须给出同一个数。
//
// 注意与路径级的区别: 路径级配 0 是"这个路径不缓存"的明确意图, 下发 no-store;
// 全局配 0 更可能是漏配, 若解读成全站禁用缓存, 一份不完整的配置就会静默关掉所有缓存。
func TestApplyClientCacheHeaders_GlobalZeroFallsBackNotDisables(t *testing.T) {
	cm, err := config.NewConfigManagerFromData("", map[string]any{
		"MAP":   map[string]any{},
		"Cache": map[string]any{"max_age": float64(0)},
	})
	if err != nil {
		t.Fatalf("构建配置失败: %v", err)
	}
	config.SetGlobalConfigManager(cm)
	t.Cleanup(func() { config.SetGlobalConfigManager(nil) })

	w := httptest.NewRecorder()
	ApplyClientCacheHeaders(w, nil)

	want := "public, max-age=" + strconv.FormatInt(config.DefaultCacheMaxAge*60, 10)
	if got := w.Header().Get("Cache-Control"); got != want {
		t.Errorf("Cache-Control = %q, want %q (须与 CacheManager 兜底一致)", got, want)
	}
}

// 全局回退的端到端验证: 路径级未配时, 头部应来自全局 Cache 配置。
// 上面的单测走的是 GetConfig() 返回 nil 的分支, 这里补上真正加载了配置的情况。
func TestApplyClientCacheHeaders_UsesGlobalConfigWhenPathUnset(t *testing.T) {
	cm, err := config.NewConfigManagerFromData("", map[string]any{
		"MAP": map[string]any{},
		"Cache": map[string]any{
			"max_age":        float64(45),
			"cleanup_tick":   float64(5),
			"max_cache_size": float64(10),
		},
	})
	if err != nil {
		t.Fatalf("构建配置失败: %v", err)
	}
	config.SetGlobalConfigManager(cm)
	t.Cleanup(func() { config.SetGlobalConfigManager(nil) })

	w := httptest.NewRecorder()
	w.Header().Set("Cache-Control", "no-store") // 模拟上游 (如 S3) 给的头
	ApplyClientCacheHeaders(w, nil)             // 路径级未配置

	if got := w.Header().Get("Cache-Control"); got != "public, max-age=2700" {
		t.Errorf("Cache-Control = %q, want 全局 45 分钟换算值 public, max-age=2700", got)
	}
}

// 显式配 0 = 不缓存, 与 "没配" (nil) 必须区分开
func TestIsNoCache(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.CacheConfig
		want bool
	}{
		{"nil 是没配, 不是不缓存", nil, false},
		{"显式 0 是不缓存", &config.CacheConfig{MaxAge: 0}, true},
		{"正数不是不缓存", &config.CacheConfig{MaxAge: 30}, false},
		{"负数是非法值, 按没配处理", &config.CacheConfig{MaxAge: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsNoCache(); got != tt.want {
				t.Errorf("IsNoCache() = %v, want %v", got, tt.want)
			}
		})
	}
}

// 路径级显式配 0 不能被全局配置覆盖掉 —— 否则"这个路径不缓存"的意图会丢失
func TestResolveCacheConfig_ExplicitZeroBeatsGlobal(t *testing.T) {
	global := &config.CacheConfig{MaxAge: 30}
	got := resolveCacheConfig(&config.CacheConfig{MaxAge: 0}, global)
	if !got.IsNoCache() {
		t.Fatalf("路径级 0 被全局覆盖了, MaxAge = %d", got.MaxAge)
	}
	if cc := got.ClientCacheControl(); cc != "no-store, no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// 不缓存的路径下发 no-store, 且清掉上游残留的 Expires
func TestApplyClientCacheHeaders_NoCachePath(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("Expires", "Wed, 21 Oct 2099 07:28:00 GMT")

	ApplyClientCacheHeaders(w, &config.CacheConfig{MaxAge: 0})

	if got := w.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("Expires"); got != "" {
		t.Errorf("Expires 应被清除, 实际 %q", got)
	}
}

// no-cache 路径必须真的不读不写磁盘缓存, 而不是只下发 no-store 头。
// 只堵头不堵盘的话, 配置等于没生效: 内容照样落盘, 且改配置前的旧副本会继续被服务。
func TestNoCachePath_SkipsCacheReadAndWrite(t *testing.T) {
	cacheManager, err := cache.NewCacheManager(t.TempDir(), &config.CacheConfig{
		MaxAge: 30, CleanupTick: 5, MaxCacheSize: 1,
	})
	if err != nil {
		t.Fatalf("NewCacheManager() error = %v", err)
	}
	svc := NewProxyService(&http.Client{}, cacheManager, nil)

	newReq := func(cc *config.CacheConfig) *ProxyRequest {
		return &ProxyRequest{
			OriginalRequest: httptest.NewRequest(http.MethodGet, "/b2/img/a.jpg", nil),
			PathConfig:      config.PathConfig{CacheConfig: cc},
		}
	}
	okResp := &http.Response{StatusCode: http.StatusOK}

	t.Run("配 0 的路径不写缓存", func(t *testing.T) {
		if svc.shouldCache(newReq(&config.CacheConfig{MaxAge: 0}), okResp) {
			t.Error("shouldCache = true, want false")
		}
	})

	t.Run("配 0 的路径不读缓存", func(t *testing.T) {
		if _, hit, _ := svc.CheckCache(newReq(&config.CacheConfig{MaxAge: 0})); hit {
			t.Error("CheckCache 命中了, 应直接跳过")
		}
	})

	t.Run("正常路径仍写缓存", func(t *testing.T) {
		if !svc.shouldCache(newReq(&config.CacheConfig{MaxAge: 30}), okResp) {
			t.Error("shouldCache = false, want true")
		}
	})

	t.Run("未配置路径 (nil) 仍写缓存", func(t *testing.T) {
		if !svc.shouldCache(newReq(nil), okResp) {
			t.Error("nil 配置应继承全局继续缓存")
		}
	})
}
