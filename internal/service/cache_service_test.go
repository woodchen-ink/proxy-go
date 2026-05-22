package service

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"proxy-go/internal/cache"
	"proxy-go/internal/config"
)

func TestClearCacheByURLHonorsTypeAndIgnoresQuery(t *testing.T) {
	proxyCache := newTestCacheManager(t, "proxy")
	mirrorCache := newTestCacheManager(t, "mirror")

	addTestCacheEntry(t, proxyCache, "/b2/img/a.jpg?v=1")
	addTestCacheEntry(t, mirrorCache, "/b2/img/a.jpg?v=2")

	cacheService := NewCacheService(proxyCache, mirrorCache)

	count, err := cacheService.ClearCacheByURL("proxy", "/b2/img/a.jpg")
	if err != nil {
		t.Fatalf("ClearCacheByURL(proxy) returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("ClearCacheByURL(proxy) cleared %d items, want 1", count)
	}
	if got := proxyCache.GetStats().TotalItems; got != 0 {
		t.Fatalf("proxy cache items = %d, want 0", got)
	}
	if got := mirrorCache.GetStats().TotalItems; got != 1 {
		t.Fatalf("mirror cache items after proxy clear = %d, want 1", got)
	}

	count, err = cacheService.ClearCacheByURL("mirror", "https://example.com/b2/img/a.jpg?refresh=1")
	if err != nil {
		t.Fatalf("ClearCacheByURL(mirror) returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("ClearCacheByURL(mirror) cleared %d items, want 1", count)
	}
	if got := mirrorCache.GetStats().TotalItems; got != 0 {
		t.Fatalf("mirror cache items = %d, want 0", got)
	}

	addTestCacheEntry(t, proxyCache, "/b2/img/a.jpg?v=11")
	addTestCacheEntry(t, mirrorCache, "/b2/img/a.jpg?v=22")

	count, err = cacheService.ClearCacheByURL("all", "/b2/img/a.jpg")
	if err != nil {
		t.Fatalf("ClearCacheByURL(all) returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("ClearCacheByURL(all) cleared %d items, want 2", count)
	}
}

func TestClearCacheByURLRejectsInvalidType(t *testing.T) {
	proxyCache := newTestCacheManager(t, "proxy")
	mirrorCache := newTestCacheManager(t, "mirror")

	cacheService := NewCacheService(proxyCache, mirrorCache)
	if _, err := cacheService.ClearCacheByURL("invalid", "/b2/img/a.jpg"); err == nil {
		t.Fatal("expected invalid cache type error, got nil")
	}
}

func newTestCacheManager(t *testing.T, name string) *cache.CacheManager {
	t.Helper()

	cacheManager, err := cache.NewCacheManager(filepath.Join(t.TempDir(), name), &config.CacheConfig{
		MaxAge:       30,
		CleanupTick:  5,
		MaxCacheSize: 1,
	})
	if err != nil {
		t.Fatalf("NewCacheManager() error = %v", err)
	}

	t.Cleanup(func() {
		cacheManager.Stop()
	})

	return cacheManager
}

func addTestCacheEntry(t *testing.T, cacheManager *cache.CacheManager, rawURL string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	key := cacheManager.GenerateCacheKey(req, false)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Request:    req,
	}

	if _, err := cacheManager.Put(key, resp, []byte("cached body")); err != nil {
		t.Fatalf("Put(%q) error = %v", rawURL, err)
	}
}
