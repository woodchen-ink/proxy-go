package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/cache"
	"proxy-go/internal/service"
)

type CacheAdminHandler struct {
	cacheService *service.CacheService
}

func NewCacheAdminHandler(proxyCache, mirrorCache *cache.CacheManager) *CacheAdminHandler {
	return &CacheAdminHandler{
		cacheService: service.NewCacheService(proxyCache, mirrorCache),
	}
}


// GetCacheStats 获取缓存统计信息
func (h *CacheAdminHandler) GetCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.cacheService.GetCacheStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetCacheConfig 获取缓存配置
func (h *CacheAdminHandler) GetCacheConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	configs := h.cacheService.GetCacheConfig()

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
		Type   string                 `json:"type"`   // "proxy", "mirror"
		Config service.CacheConfig    `json:"config"` // 新的配置
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.cacheService.UpdateCacheConfig(req.Type, req.Config); err != nil {
		if err.Error() == "invalid cache type" {
			http.Error(w, "Invalid cache type", http.StatusBadRequest)
		} else {
			http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		}
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

	if err := h.cacheService.SetCacheEnabled(req.Type, req.Enabled); err != nil {
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

	if err := h.cacheService.ClearCache(req.Type); err != nil {
		if err.Error() == "invalid cache type" {
			http.Error(w, "Invalid cache type", http.StatusBadRequest)
		} else {
			http.Error(w, "Failed to clear cache: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}
