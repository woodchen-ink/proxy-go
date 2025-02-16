package cache

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func TestCacheExpiry(t *testing.T) {
	// 创建临时目录用于测试
	tempDir, err := os.MkdirTemp("", "cache-test-*")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tempDir)

	// 创建缓存管理器，设置较短的过期时间（5秒）用于测试
	cm, err := NewCacheManager(tempDir)
	if err != nil {
		t.Fatal("Failed to create cache manager:", err)
	}
	cm.maxAge = 5 * time.Second

	// 创建测试请求和响应
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}
	testData := []byte("test data")

	// 生成缓存键
	key := cm.GenerateCacheKey(req)

	// 1. 首先放入缓存
	_, err = cm.Put(key, resp, testData)
	if err != nil {
		t.Fatal("Failed to put item in cache:", err)
	}

	// 2. 立即获取，应该能命中
	if _, hit, _ := cm.Get(key, req); !hit {
		t.Error("Cache should hit immediately after putting")
	}

	// 3. 等待3秒（未过期），再次访问
	time.Sleep(3 * time.Second)
	if _, hit, _ := cm.Get(key, req); !hit {
		t.Error("Cache should hit after 3 seconds")
	}

	// 4. 再等待3秒（总共6秒，但因为上次访问重置了时间，所以应该还在有效期内）
	time.Sleep(3 * time.Second)
	if _, hit, _ := cm.Get(key, req); !hit {
		t.Error("Cache should hit after 6 seconds because last access reset the timer")
	}

	// 5. 等待6秒（超过过期时间且无访问），这次应该过期
	time.Sleep(6 * time.Second)
	if _, hit, _ := cm.Get(key, req); hit {
		t.Error("Cache should expire after 6 seconds of no access")
	}
}
