package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"proxy-go/internal/config"
)

// newFailoverTestService 构造一个仅用于 failover 测试的 ProxyService。
// cache / ruleService 在 ExecuteRequestWithFailover 路径上不被触达, 传 nil 即可。
func newFailoverTestService() *ProxyService {
	return &ProxyService{
		client:      &http.Client{Timeout: 5 * time.Second},
		retryConfig: RetryConfig{MaxRetries: 0, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, Multiplier: 1},
	}
}

func newGetProxyRequest(t *testing.T, targetPath string) *ProxyRequest {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, targetPath, nil)
	return &ProxyRequest{
		OriginalRequest: r,
		TargetPath:      targetPath,
		PathConfig:      config.PathConfig{},
		StartTime:       time.Now(),
	}
}

// TestFailoverSkipsTo404NextSource 主源 404 时应回落到返回 200 的备源
func TestFailoverSkipsTo404NextSource(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer primary.Close()

	var backupHit int32
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupHit, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backup.Close()

	s := newFailoverTestService()
	req := newGetProxyRequest(t, "/a.jpg")
	resp, target, didFailover, err := s.ExecuteRequestWithFailover(req, []string{primary.URL, backup.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from backup, got %d", resp.StatusCode)
	}
	if !didFailover {
		t.Fatalf("expected didFailover=true")
	}
	if target != backup.URL {
		t.Fatalf("expected target=%s, got %s", backup.URL, target)
	}
	if atomic.LoadInt32(&backupHit) != 1 {
		t.Fatalf("backup should be hit exactly once, got %d", backupHit)
	}
}

// TestFailoverConnectionErrorFallsThrough 主源连接失败 (端口已关) 时回落到备源
func TestFailoverConnectionErrorFallsThrough(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // 立即关闭, 制造连接拒绝

	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()

	s := newFailoverTestService()
	req := newGetProxyRequest(t, "/x")
	resp, target, didFailover, err := s.ExecuteRequestWithFailover(req, []string{deadURL, backup.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK || !didFailover || target != backup.URL {
		t.Fatalf("expected failover to backup 200, got status=%d didFailover=%v target=%s", resp.StatusCode, didFailover, target)
	}
}

// TestFailoverLastSourcePassthrough 全部源都 404 时, 透传最后一个源的真实 404 (不吞掉)
func TestFailoverLastSourcePassthrough(t *testing.T) {
	mk404 := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
	}
	s1, s2 := mk404(), mk404()
	defer s1.Close()
	defer s2.Close()

	s := newFailoverTestService()
	req := newGetProxyRequest(t, "/missing")
	resp, target, _, err := s.ExecuteRequestWithFailover(req, []string{s1.URL, s2.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected passthrough 404, got %d", resp.StatusCode)
	}
	if target != s2.URL {
		t.Fatalf("expected last source target=%s, got %s", s2.URL, target)
	}
}

// TestFailoverSingleSourceUnchanged 单源行为与旧逻辑一致 (didFailover=false)
func TestFailoverSingleSourceUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := newFailoverTestService()
	req := newGetProxyRequest(t, "/single")
	resp, _, didFailover, err := s.ExecuteRequestWithFailover(req, []string{srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if didFailover {
		t.Fatalf("single source should never report failover")
	}
}

// TestPostWithBodyDoesNotFailover 带 body 的 POST 只打首源, 不换源 (避免重复副作用)
func TestPostWithBodyDoesNotFailover(t *testing.T) {
	var primaryHit, backupHit int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryHit, 1)
		w.WriteHeader(http.StatusNotFound) // 即使 404, POST 也不该换源
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupHit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backup.Close()

	s := newFailoverTestService()
	r := httptest.NewRequest(http.MethodPost, "/submit", http.NoBody)
	r.ContentLength = 5 // 模拟有 body
	req := &ProxyRequest{OriginalRequest: r, TargetPath: "/submit", StartTime: time.Now()}

	resp, _, didFailover, err := s.ExecuteRequestWithFailover(req, []string{primary.URL, backup.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if didFailover {
		t.Fatalf("POST with body must not failover")
	}
	if atomic.LoadInt32(&backupHit) != 0 {
		t.Fatalf("backup must not be hit for non-idempotent request, got %d", backupHit)
	}
	if atomic.LoadInt32(&primaryHit) != 1 {
		t.Fatalf("primary should be hit once, got %d", primaryHit)
	}
}
