package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/service"
	"time"
)

// CDNHandler 暴露 CDN provider 配置 CRUD 与一键清理缓存接口
type CDNHandler struct {
	cdnService *service.CDNService
}

// NewCDNHandler 构造 CDN handler
func NewCDNHandler(cm *config.ConfigManager) *CDNHandler {
	return &CDNHandler{cdnService: service.NewCDNService(cm)}
}

// ListProviders 返回所有 provider 配置 (含凭据), 仅认证管理员可访问
func (h *CDNHandler) ListProviders(w http.ResponseWriter, _ *http.Request) {
	providers := h.cdnService.ListProviders()
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
	})
}

// SaveProviders 整体覆盖 provider 列表, 实现单一启用约束
func (h *CDNHandler) SaveProviders(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Providers []config.CDNProvider `json:"providers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Providers == nil {
		body.Providers = []config.CDNProvider{}
	}
	if err := h.cdnService.SaveProviders(body.Providers); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message":   "CDN 配置已保存",
		"providers": h.cdnService.ListProviders(),
	})
}

// Purge 触发当前启用的 provider 执行 purge
func (h *CDNHandler) Purge(w http.ResponseWriter, r *http.Request) {
	var req service.CDNPurgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	result, err := h.cdnService.Purge(ctx, req)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, service.ErrCDNNoEnabledProvider):
			status = http.StatusFailedDependency
		case errors.Is(err, service.ErrCDNInvalidPurgeType),
			errors.Is(err, service.ErrCDNMissingTarget),
			errors.Is(err, service.ErrCDNUnsupportedType):
			status = http.StatusBadRequest
		}
		payload := map[string]any{"error": err.Error()}
		if result != nil {
			payload["result"] = result
		}
		writeJSON(w, status, payload)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}
