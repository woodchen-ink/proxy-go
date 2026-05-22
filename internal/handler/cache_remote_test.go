package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"proxy-go/internal/cache"
	"proxy-go/internal/config"
)

func TestNormalizeRemoteCacheURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "path drops query and fragment",
			input: " /b2/img/a.jpg?v=1 ",
			want:  "/b2/img/a.jpg",
		},
		{
			name:  "full url drops query and fragment",
			input: "https://example.com/b2/img/a.jpg?v=1#top",
			want:  "/b2/img/a.jpg",
		},
		{
			name:  "full url root path",
			input: "https://example.com",
			want:  "/",
		},
		{
			name:    "empty rejected",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "invalid url rejected",
			input:   "example.com/a.jpg",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRemoteCacheURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeRemoteCacheURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCacheRemoteHandlerClearCacheByURL(t *testing.T) {
	t.Setenv("CACHE_CLEAR_REMOTE_TOKEN", "secret-token")

	proxyCache := newRemoteTestCacheManager(t, "proxy")
	mirrorCache := newRemoteTestCacheManager(t, "mirror")
	addRemoteTestCacheEntry(t, proxyCache, "/b2/img/a.jpg?v=1")
	addRemoteTestCacheEntry(t, mirrorCache, "/b2/img/a.jpg?v=2")

	handler := NewCacheRemoteHandler(proxyCache, mirrorCache)

	req := httptest.NewRequest(http.MethodPost, "/api/cache/clear-url", strings.NewReader(`{"url":"https://example.com/b2/img/a.jpg?refresh=1"}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ClearCacheByURL(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response remoteCacheClearResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Data.NormalizedURL != "/b2/img/a.jpg" {
		t.Fatalf("normalized_url = %q, want %q", response.Data.NormalizedURL, "/b2/img/a.jpg")
	}
	if response.Data.Type != "all" {
		t.Fatalf("type = %q, want %q", response.Data.Type, "all")
	}
	if response.Data.ClearedItems != 2 {
		t.Fatalf("cleared_items = %d, want 2", response.Data.ClearedItems)
	}
}

func TestCacheRemoteHandlerAuthAndConfigErrors(t *testing.T) {
	t.Run("missing token config returns not found", func(t *testing.T) {
		t.Setenv("CACHE_CLEAR_REMOTE_TOKEN", "")
		handler := NewCacheRemoteHandler(newRemoteTestCacheManager(t, "proxy"), newRemoteTestCacheManager(t, "mirror"))

		req := httptest.NewRequest(http.MethodPost, "/api/cache/clear-url", strings.NewReader(`{"url":"/b2/img/a.jpg"}`))
		recorder := httptest.NewRecorder()

		handler.ClearCacheByURL(recorder, req)

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("invalid bearer token returns unauthorized", func(t *testing.T) {
		t.Setenv("CACHE_CLEAR_REMOTE_TOKEN", "secret-token")
		handler := NewCacheRemoteHandler(newRemoteTestCacheManager(t, "proxy"), newRemoteTestCacheManager(t, "mirror"))

		req := httptest.NewRequest(http.MethodPost, "/api/cache/clear-url", strings.NewReader(`{"url":"/b2/img/a.jpg"}`))
		req.Header.Set("Authorization", "Bearer wrong-token")
		recorder := httptest.NewRecorder()

		handler.ClearCacheByURL(recorder, req)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})

	t.Run("invalid cache type returns bad request", func(t *testing.T) {
		t.Setenv("CACHE_CLEAR_REMOTE_TOKEN", "secret-token")
		handler := NewCacheRemoteHandler(newRemoteTestCacheManager(t, "proxy"), newRemoteTestCacheManager(t, "mirror"))

		req := httptest.NewRequest(http.MethodPost, "/api/cache/clear-url", strings.NewReader(`{"url":"/b2/img/a.jpg","type":"invalid"}`))
		req.Header.Set("Authorization", "Bearer secret-token")
		recorder := httptest.NewRecorder()

		handler.ClearCacheByURL(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})
}

func newRemoteTestCacheManager(t *testing.T, name string) *cache.CacheManager {
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

func addRemoteTestCacheEntry(t *testing.T, cacheManager *cache.CacheManager, rawURL string) {
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
