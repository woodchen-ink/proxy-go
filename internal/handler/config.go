package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/service"
)

// ConfigHandler 配置管理处理器
type ConfigHandler struct {
	configService *service.ConfigService
}

// NewConfigHandler 创建新的配置管理处理器
func NewConfigHandler(configManager *config.ConfigManager) *ConfigHandler {
	return &ConfigHandler{
		configService: service.NewConfigService(configManager),
	}
}

// ServeHTTP 实现http.Handler接口
func (h *ConfigHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/admin/api/config/get":
		h.handleGetConfig(w, r)
	case "/admin/api/config/save":
		h.handleSaveConfig(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleGetConfig 处理获取配置请求
func (h *ConfigHandler) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	configData, err := h.configService.GetConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(configData)
}

// handleSaveConfig 处理保存配置请求
func (h *ConfigHandler) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 解析新配置
	var newConfig config.Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "解析配置失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 保存配置（包含验证和更新逻辑）
	if err := h.configService.SaveConfig(&newConfig); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message": "配置已更新并生效"}`))
}

