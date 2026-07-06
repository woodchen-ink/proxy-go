package cache

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"proxy-go/internal/config"
)

// newAccountingCache 构造一个用于计数校验的缓存管理器 (临时目录, 默认配置)。
func newAccountingCache(t *testing.T) *CacheManager {
	t.Helper()
	cm, err := NewCacheManager(filepath.Join(t.TempDir(), "acct"), &config.CacheConfig{
		MaxAge:       30,
		CleanupTick:  5,
		MaxCacheSize: 1,
	})
	if err != nil {
		t.Fatalf("NewCacheManager() error = %v", err)
	}
	t.Cleanup(cm.Stop)
	return cm
}

// putEntry 以给定 URL 与 body 写入一条缓存, 返回缓存键。
func putEntry(t *testing.T, cm *CacheManager, rawURL, body string) CacheKey {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	key := cm.GenerateCacheKey(req, false)
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req}
	if _, err := cm.Put(key, resp, []byte(body)); err != nil {
		t.Fatalf("Put(%q) error = %v", rawURL, err)
	}
	return key
}

// assertCounts 校验四个规模计数与预期一致。
func assertCounts(t *testing.T, cm *CacheManager, items, size, files, disk int64) {
	t.Helper()
	s := cm.GetStats()
	if int64(s.TotalItems) != items || s.TotalSize != size ||
		int64(s.DiskFileCount) != files || s.DiskSize != disk {
		t.Fatalf("counts = {items:%d size:%d files:%d disk:%d}, want {items:%d size:%d files:%d disk:%d}",
			s.TotalItems, s.TotalSize, s.DiskFileCount, s.DiskSize, items, size, files, disk)
	}
}

// TestAccountingPutDistinctAndDedup 校验: 不同内容各占一份物理文件;
// 同内容不同 key 复用同一物理文件 (逻辑 +1, 磁盘不变)。
func TestAccountingPutDistinctAndDedup(t *testing.T) {
	cm := newAccountingCache(t)

	putEntry(t, cm, "/a.jpg", "AAAA") // 4B, 新文件
	assertCounts(t, cm, 1, 4, 1, 4)

	putEntry(t, cm, "/b.jpg", "BBBBBB") // 6B, 新文件
	assertCounts(t, cm, 2, 10, 2, 10)

	// 同内容不同 URL: 逻辑口径 +1, 去重口径不变
	putEntry(t, cm, "/c.jpg", "AAAA")
	assertCounts(t, cm, 3, 14, 2, 10)
}

// TestAccountingClearByURL 校验清理单 URL 后计数正确回落。
func TestAccountingClearByURL(t *testing.T) {
	cm := newAccountingCache(t)
	putEntry(t, cm, "/x.jpg?v=1", "AAAA")
	putEntry(t, cm, "/y.jpg", "BBBBBB")
	assertCounts(t, cm, 2, 10, 2, 10)

	if _, err := cm.ClearCacheByURL("/x.jpg"); err != nil {
		t.Fatalf("ClearCacheByURL error = %v", err)
	}
	assertCounts(t, cm, 1, 6, 1, 6)
}

// TestAccountingClearAll 校验清空后四计数全部归零。
func TestAccountingClearAll(t *testing.T) {
	cm := newAccountingCache(t)
	putEntry(t, cm, "/a.jpg", "AAAA")
	putEntry(t, cm, "/b.jpg", "BBBBBB")

	if err := cm.ClearCache(); err != nil {
		t.Fatalf("ClearCache error = %v", err)
	}
	assertCounts(t, cm, 0, 0, 0, 0)
}

// TestAccountingInvalidateItem 校验单项失效后计数扣减。
func TestAccountingInvalidateItem(t *testing.T) {
	cm := newAccountingCache(t)
	key := putEntry(t, cm, "/a.jpg", "AAAA")
	putEntry(t, cm, "/b.jpg", "BBBBBB")
	assertCounts(t, cm, 2, 10, 2, 10)

	cm.InvalidateCacheItem(key)
	assertCounts(t, cm, 1, 6, 1, 6)
}
