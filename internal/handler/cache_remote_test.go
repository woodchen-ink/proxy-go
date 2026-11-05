package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCacheRemoteForwardsToHub(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cache/clear-url" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":200,"msg":"cache cleared"}`))
	}))
	defer hub.Close()

	h := NewCacheRemoteHandler(func() string { return hub.URL })
	req := httptest.NewRequest(http.MethodPost, "http://i.example.com/api/cache/clear-url", strings.NewReader(`{"url":"/b2/a.jpg"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ClearCacheByURL(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("authorization not forwarded: %q", gotAuth)
	}
	if gotBody["host"] != "i.example.com" || gotBody["url"] != "/b2/a.jpg" {
		t.Fatalf("unexpected forwarded body: %v", gotBody)
	}
}

func TestCacheRemoteDisabledWithoutHub(t *testing.T) {
	h := NewCacheRemoteHandler(func() string { return "" })
	rec := httptest.NewRecorder()
	h.ClearCacheByURL(rec, httptest.NewRequest(http.MethodPost, "/api/cache/clear-url", strings.NewReader(`{}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCacheRemotePassesHubStatus(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"msg":"unauthorized"}`))
	}))
	defer hub.Close()
	h := NewCacheRemoteHandler(func() string { return hub.URL })
	rec := httptest.NewRecorder()
	h.ClearCacheByURL(rec, httptest.NewRequest(http.MethodPost, "/api/cache/clear-url", strings.NewReader(`{"url":"/x"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
