package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
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
		Type   string              `json:"type"`   // "proxy", "mirror"
		Config config.CacheConfig  `json:"config"` // 新的配置
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 更新缓存管理器配置
	if err := h.cacheService.UpdateCacheConfig(req.Type, req.Config); err != nil {
		if err.Error() == "invalid cache type" {
			http.Error(w, "Invalid cache type", http.StatusBadRequest)
		} else {
			http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// 同时更新主配置文件
	mainConfig := config.GetConfig()
	if mainConfig != nil {
		// 创建配置副本以避免并发问题
		newConfig := *mainConfig
		
		// 更新对应的缓存配置
		switch req.Type {
		case "proxy":
			newConfig.Cache = req.Config
		case "mirror":
			newConfig.MirrorCache = req.Config
		}
		
		// 保存到主配置文件
		if err := config.UpdateConfig(&newConfig); err != nil {
			http.Error(w, "Failed to save config to file: "+err.Error(), http.StatusInternalServerError)
			return
		}
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

// ClearCacheByPath 清空指定路径的缓存
func (h *CacheAdminHandler) ClearCacheByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type       string `json:"type"`        // "proxy", "mirror" 或 "all"
		PathPrefix string `json:"path_prefix"` // 路径前缀，例如 "/path1"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.PathPrefix == "" {
		http.Error(w, "path_prefix is required", http.StatusBadRequest)
		return
	}

	count, err := h.cacheService.ClearCacheByPath(req.Type, req.PathPrefix)
	if err != nil {
		if err.Error() == "invalid cache type" {
			http.Error(w, "Invalid cache type", http.StatusBadRequest)
		} else {
			http.Error(w, "Failed to clear cache: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"cleared_items": count,
		"message":       fmt.Sprintf("Cleared %d cache items for path prefix: %s", count, req.PathPrefix),
	})
}

// ClearCacheByURLs 清空指定 URL 列表的缓存
func (h *CacheAdminHandler) ClearCacheByURLs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Type string   `json:"type"` // "proxy", "mirror" 或 "all"
		URLs []string `json:"urls"` // URL 列表，例如 ["/b2/img/photo.jpg", "/oracle/file.pdf"]
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.URLs) == 0 {
		http.Error(w, "urls is required and cannot be empty", http.StatusBadRequest)
		return
	}

	count, err := h.cacheService.ClearCacheByURLs(req.Type, req.URLs)
	if err != nil {
		if err.Error() == "invalid cache type" {
			http.Error(w, "Invalid cache type", http.StatusBadRequest)
		} else {
			http.Error(w, "Failed to clear cache: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"cleared_items": count,
		"message":       fmt.Sprintf("Cleared %d cache items for %d URLs", count, len(req.URLs)),
	})
}
