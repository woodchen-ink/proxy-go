package handler

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"proxy-go/internal/cache"
	"proxy-go/internal/service"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

type CacheRemoteHandler struct {
	cacheService *service.CacheService
	token        string
}

type remoteCacheClearRequest struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type remoteCacheClearResponse struct {
	Code int                          `json:"code"`
	Data remoteCacheClearResponseData `json:"data,omitempty"`
	Msg  string                       `json:"msg"`
}

type remoteCacheClearResponseData struct {
	InputURL      string `json:"input_url"`
	NormalizedURL string `json:"normalized_url"`
	Type          string `json:"type"`
	ClearedItems  int    `json:"cleared_items"`
}

// NewCacheRemoteHandler 创建远程单 URL 清理缓存处理器。
func NewCacheRemoteHandler(proxyCache, mirrorCache *cache.CacheManager) *CacheRemoteHandler {
	return &CacheRemoteHandler{
		cacheService: service.NewCacheService(proxyCache, mirrorCache),
		token:        strings.TrimSpace(os.Getenv("CACHE_CLEAR_REMOTE_TOKEN")),
	}
}

// ClearCacheByURL 提供给外部三方调用的单 URL 缓存清理接口。
func (h *CacheRemoteHandler) ClearCacheByURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if h.token == "" {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}

	if !h.isAuthorized(r) {
		log.Printf("[RemoteCacheClear] AUTH_FAIL %s %s ip=%s", r.Method, r.URL.Path, iputil.GetClientIP(r))
		h.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req remoteCacheClearRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cacheType := strings.TrimSpace(req.Type)
	if cacheType == "" {
		cacheType = "all"
	}

	normalizedURL, err := normalizeRemoteCacheURL(req.URL)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	clearedItems, err := h.cacheService.ClearCacheByURL(cacheType, normalizedURL)
	if err != nil {
		if err.Error() == "invalid cache type" {
			h.writeError(w, http.StatusBadRequest, "invalid cache type")
			return
		}

		log.Printf("[RemoteCacheClear] ERR ip=%s type=%s input=%q normalized=%q err=%v", iputil.GetClientIP(r), cacheType, req.URL, normalizedURL, err)
		h.writeError(w, http.StatusInternalServerError, "failed to clear cache")
		return
	}

	log.Printf("[RemoteCacheClear] OK ip=%s type=%s input=%q normalized=%q cleared_items=%d", iputil.GetClientIP(r), cacheType, req.URL, normalizedURL, clearedItems)
	h.writeJSON(w, http.StatusOK, remoteCacheClearResponse{
		Code: http.StatusOK,
		Data: remoteCacheClearResponseData{
			InputURL:      req.URL,
			NormalizedURL: normalizedURL,
			Type:          cacheType,
			ClearedItems:  clearedItems,
		},
		Msg: "cache cleared",
	})
}

// isAuthorized 校验远程缓存清理接口的 Bearer Token。
func (h *CacheRemoteHandler) isAuthorized(r *http.Request) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if token == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(h.token)) == 1
}

// writeError 统一输出远程缓存清理接口错误响应。
func (h *CacheRemoteHandler) writeError(w http.ResponseWriter, statusCode int, message string) {
	h.writeJSON(w, statusCode, remoteCacheClearResponse{
		Code: statusCode,
		Msg:  message,
	})
}

// writeJSON 写出 JSON 响应。
func (h *CacheRemoteHandler) writeJSON(w http.ResponseWriter, statusCode int, payload remoteCacheClearResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[RemoteCacheClear] Failed to encode response: %v", err)
	}
}

// normalizeRemoteCacheURL 将完整 URL 或站内路径归一化为缓存清理使用的路径。
func normalizeRemoteCacheURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", &remoteCacheInputError{message: "url is required"}
	}

	if strings.HasPrefix(trimmed, "/") {
		return trimCachePathSuffix(stripCacheURLQuery(trimmed)), nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", &remoteCacheInputError{message: "invalid url"}
	}

	path := parsed.Path
	if path == "" {
		path = "/"
	}

	return trimCachePathSuffix(path), nil
}

// trimCachePathSuffix 统一尾斜杠归一化，但保留根路径。
func trimCachePathSuffix(path string) string {
	if path == "/" {
		return path
	}

	return strings.TrimSuffix(path, "/")
}

// stripCacheURLQuery 去除 query 和 fragment，保留原始路径文本。
func stripCacheURLQuery(path string) string {
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		return path[:index]
	}

	return path
}

type remoteCacheInputError struct {
	message string
}

func (e *remoteCacheInputError) Error() string {
	return e.message
}
