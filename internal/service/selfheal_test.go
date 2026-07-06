package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"proxy-go/internal/cache"
	"proxy-go/internal/config"
)

// newCacheBackedProxyService 构造一个带真实文件缓存的 ProxyService, 供 404 自愈测试使用。
func newCacheBackedProxyService(t *testing.T) *ProxyService {
	t.Helper()

	cm, err := cache.NewCacheManager(t.TempDir(), &config.CacheConfig{
		MaxAge:       60,
		CleanupTick:  60,
		MaxCacheSize: 1,
	})
	if err != nil {
		t.Fatalf("new cache manager: %v", err)
	}

	return &ProxyService{
		client: &http.Client{Timeout: 5 * time.Second},
		cache:  cm,
	}
}

// processGet 用给定状态码与响应体驱动一次 ProcessResponse, 复用真实缓存写入 / 自愈路径。
func processGet(t *testing.T, s *ProxyService, urlPath string, status int, body string) {
	t.Helper()

	req := newGetProxyRequest(t, urlPath)
	upstream := &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       http.NoBody,
		Request:    req.OriginalRequest,
	}
	if body != "" {
		upstream.Body = io.NopCloser(strings.NewReader(body))
	}

	rec := httptest.NewRecorder()
	if _, err := s.ProcessResponse(req, upstream, rec, false); err != nil {
		t.Fatalf("ProcessResponse(%d) err: %v", status, err)
	}
}

// TestSelfHealOn404ClearsCachedURL 源站先 200 被缓存, 后 404 应触发本地缓存自愈清除。
func TestSelfHealOn404ClearsCachedURL(t *testing.T) {
	s := newCacheBackedProxyService(t)
	const urlPath = "/b2/img/self-heal.png"

	// 第一次 200: 写入本地缓存 (Commit 为异步, 稍等落盘)
	processGet(t, s, urlPath, http.StatusOK, "image-bytes")
	time.Sleep(150 * time.Millisecond)

	// httptest.NewRequest 相对路径请求, URL.String() 即路径本身, 与缓存写入键一致
	if !s.cache.HasURLCached(urlPath) {
		t.Fatalf("expected URL cached after 200 response")
	}

	// 第二次 404: selfHealOn404 应清掉本地缓存
	processGet(t, s, urlPath, http.StatusNotFound, "")

	if s.cache.HasURLCached(urlPath) {
		t.Fatalf("expected local cache cleared after 404 self-heal")
	}
}

// TestSelfHealOn404NoopWhenNotCached 未缓存过的 URL 命中 404 时不应报错, 仅廉价 no-op。
func TestSelfHealOn404NoopWhenNotCached(t *testing.T) {
	s := newCacheBackedProxyService(t)
	// 直接 404, 缓存里从无此条目; 断言不 panic / 不报错即可 (探测 O(1) miss 后直接返回)
	processGet(t, s, "/b2/img/never-cached.png", http.StatusNotFound, "")
}
