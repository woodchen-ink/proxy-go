package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/service"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

type CacheRemoteHandler struct {
	cacheService *service.CacheService
	cdnPurger    remoteCacheCDNPurger
	token        string
}

type remoteCacheCDNPurger interface {
	ListProviders() []config.CDNProvider
	Purge(context.Context, service.CDNPurgeRequest) (*service.CDNPurgeResult, error)
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
func NewCacheRemoteHandler(proxyCache, mirrorCache *cache.CacheManager, configManagers ...*config.ConfigManager) *CacheRemoteHandler {
	var cdnPurger remoteCacheCDNPurger
	if len(configManagers) > 0 && configManagers[0] != nil {
		cdnPurger = service.NewCDNService(configManagers[0])
	}

	return &CacheRemoteHandler{
		cacheService: service.NewCacheService(proxyCache, mirrorCache),
		cdnPurger:    cdnPurger,
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
	h.purgeCDNByURLAsync(r, req.URL, normalizedURL)
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

// purgeCDNByURLAsync 在本地缓存清理成功后后台触发当前启用 CDN provider 的 URL purge。
func (h *CacheRemoteHandler) purgeCDNByURLAsync(r *http.Request, inputURL, normalizedURL string) {
	if h.cdnPurger == nil || !hasEnabledCDNProvider(h.cdnPurger.ListProviders()) {
		return
	}

	target, err := buildRemoteCacheCDNTarget(r, inputURL, normalizedURL)
	if err != nil {
		log.Printf("[RemoteCacheClear] CDN_SKIP input=%q normalized=%q err=%v", inputURL, normalizedURL, err)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()

		result, err := h.cdnPurger.Purge(ctx, service.CDNPurgeRequest{
			Type:    service.CDNPurgeTypeURLs,
			Targets: []string{target},
		})
		if err != nil {
			log.Printf("[RemoteCacheClear] CDN_ERR target=%q err=%v", target, err)
			return
		}

		jobID := ""
		if result != nil {
			jobID = result.JobID
		}
		log.Printf("[RemoteCacheClear] CDN_OK target=%q job_id=%q", target, jobID)
	}()
}

// hasEnabledCDNProvider 判断是否存在启用中的 CDN provider, 未配置时远程清理保持本地 no-op。
func hasEnabledCDNProvider(providers []config.CDNProvider) bool {
	for _, provider := range providers {
		if provider.Enabled {
			return true
		}
	}
	return false
}

// buildRemoteCacheCDNTarget 把远程清理输入转为 CDN purge 需要的完整 URL。
func buildRemoteCacheCDNTarget(r *http.Request, inputURL, normalizedURL string) (string, error) {
	trimmed := strings.TrimSpace(inputURL)
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("unsupported url scheme %q", parsed.Scheme)
		}
		return parsed.Scheme + "://" + parsed.Host + normalizedURL, nil
	}

	if r == nil || strings.TrimSpace(r.Host) == "" {
		return "", fmt.Errorf("request host is required for relative cache url")
	}

	scheme := requestScheme(r)
	return scheme + "://" + r.Host + normalizedURL, nil
}

// requestScheme 从代理头和 TLS 状态推断外部请求协议。
func requestScheme(r *http.Request) string {
	if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		if index := strings.Index(forwardedProto, ","); index >= 0 {
			forwardedProto = forwardedProto[:index]
		}
		forwardedProto = strings.ToLower(strings.TrimSpace(forwardedProto))
		if forwardedProto == "http" || forwardedProto == "https" {
			return forwardedProto
		}
	}

	if forwarded := strings.TrimSpace(r.Header.Get("Forwarded")); forwarded != "" {
		if proto := forwardedProtoValue(forwarded); proto == "http" || proto == "https" {
			return proto
		}
	}

	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// forwardedProtoValue 解析 RFC 7239 Forwarded 头中的 proto 值。
func forwardedProtoValue(header string) string {
	if index := strings.Index(header, ","); index >= 0 {
		header = header[:index]
	}

	for _, part := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || !strings.EqualFold(key, "proto") {
			continue
		}
		unquoted, err := strconv.Unquote(strings.TrimSpace(value))
		if err == nil {
			value = unquoted
		}
		return strings.ToLower(strings.TrimSpace(value))
	}
	return ""
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
