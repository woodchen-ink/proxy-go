package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/cache"
)

type CacheAdminHandler struct {
	proxyCache     *cache.CacheManager
	mirrorCache    *cache.CacheManager
	fixedPathCache *cache.CacheManager
}

func NewCacheAdminHandler(proxyCache, mirrorCache, fixedPathCache *cache.CacheManager) *CacheAdminHandler {
	return &CacheAdminHandler{
		proxyCache:     proxyCache,
		mirrorCache:    mirrorCache,
		fixedPathCache: fixedPathCache,
	}
}

// GetCacheStats 获取缓存统计信息
func (h *CacheAdminHandler) GetCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := map[string]cache.CacheStats{
		"proxy":     h.proxyCache.GetStats(),
		"mirror":    h.mirrorCache.GetStats(),
		"fixedPath": h.fixedPathCache.GetStats(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// SetCacheEnabled 设置缓存开关状态
func (h *CacheAdminHandler) SetCacheEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type    string `json:"type"`    // "proxy", "mirror" 或 "fixedPath"
		Enabled bool   `json:"enabled"` // true 或 false
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	switch req.Type {
	case "proxy":
		h.proxyCache.SetEnabled(req.Enabled)
	case "mirror":
		h.mirrorCache.SetEnabled(req.Enabled)
	case "fixedPath":
		h.fixedPathCache.SetEnabled(req.Enabled)
	default:
		http.Error(w, "Invalid cache type", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ClearCache 清空缓存
func (h *CacheAdminHandler) ClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type string `json:"type"` // "proxy", "mirror", "fixedPath" 或 "all"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var err error
	switch req.Type {
	case "proxy":
		err = h.proxyCache.ClearCache()
	case "mirror":
		err = h.mirrorCache.ClearCache()
	case "fixedPath":
		err = h.fixedPathCache.ClearCache()
	case "all":
		err = h.proxyCache.ClearCache()
		if err == nil {
			err = h.mirrorCache.ClearCache()
		}
		if err == nil {
			err = h.fixedPathCache.ClearCache()
		}
	default:
		http.Error(w, "Invalid cache type", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, "Failed to clear cache: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
