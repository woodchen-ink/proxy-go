package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/cache"
)

type CacheAdminHandler struct {
	proxyCache  *cache.CacheManager
	mirrorCache *cache.CacheManager
}

func NewCacheAdminHandler(proxyCache, mirrorCache *cache.CacheManager) *CacheAdminHandler {
	return &CacheAdminHandler{
		proxyCache:  proxyCache,
		mirrorCache: mirrorCache,
	}
}

// CacheConfig 缓存配置结构
type CacheConfig struct {
	MaxAge       int64 `json:"max_age"`        // 最大缓存时间（分钟）
	CleanupTick  int64 `json:"cleanup_tick"`   // 清理间隔（分钟）
	MaxCacheSize int64 `json:"max_cache_size"` // 最大缓存大小（GB）
}

// GetCacheStats 获取缓存统计信息
func (h *CacheAdminHandler) GetCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := map[string]cache.CacheStats{
		"proxy":  h.proxyCache.GetStats(),
		"mirror": h.mirrorCache.GetStats(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetCacheConfig 获取缓存配置
func (h *CacheAdminHandler) GetCacheConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := map[string]cache.CacheConfig{
		"proxy":  h.proxyCache.GetConfig(),
		"mirror": h.mirrorCache.GetConfig(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configs)
}

// UpdateCacheConfig 更新缓存配置
func (h *CacheAdminHandler) UpdateCacheConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type   string      `json:"type"`   // "proxy", "mirror"
		Config CacheConfig `json:"config"` // 新的配置
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var targetCache *cache.CacheManager
	switch req.Type {
	case "proxy":
		targetCache = h.proxyCache
	case "mirror":
		targetCache = h.mirrorCache
	default:
		http.Error(w, "Invalid cache type", http.StatusBadRequest)
		return
	}

	if err := targetCache.UpdateConfig(req.Config.MaxAge, req.Config.CleanupTick, req.Config.MaxCacheSize); err != nil {
		http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// SetCacheEnabled 设置缓存开关状态
func (h *CacheAdminHandler) SetCacheEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type    string `json:"type"`    // "proxy", "mirror"
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
		Type string `json:"type"` // "proxy", "mirror" 或 "all"
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
	case "all":
		err = h.proxyCache.ClearCache()
		if err == nil {
			err = h.mirrorCache.ClearCache()
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
