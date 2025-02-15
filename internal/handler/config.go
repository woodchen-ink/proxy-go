package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"proxy-go/internal/config"
)

// ConfigHandler 配置管理处理器
type ConfigHandler struct {
	config *config.Config
}

// NewConfigHandler 创建新的配置管理处理器
func NewConfigHandler(cfg *config.Config) *ConfigHandler {
	return &ConfigHandler{
		config: cfg,
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

// handleConfigPage 处理配置页面请求
func (h *ConfigHandler) handleConfigPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/templates/config.html")
}

// handleGetConfig 处理获取配置请求
func (h *ConfigHandler) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 读取当前配置文件
	configData, err := os.ReadFile("data/config.json")
	if err != nil {
		http.Error(w, fmt.Sprintf("读取配置文件失败: %v", err), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("解析配置失败: %v", err), http.StatusBadRequest)
		return
	}

	// 验证新配置
	if err := h.validateConfig(&newConfig); err != nil {
		http.Error(w, fmt.Sprintf("配置验证失败: %v", err), http.StatusBadRequest)
		return
	}

	// 将新配置格式化为JSON
	configData, err := json.MarshalIndent(newConfig, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("格式化配置失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 保存到临时文件
	tempFile := "data/config.json.tmp"
	if err := os.WriteFile(tempFile, configData, 0644); err != nil {
		http.Error(w, fmt.Sprintf("保存配置失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 重命名临时文件为正式文件
	if err := os.Rename(tempFile, "data/config.json"); err != nil {
		http.Error(w, fmt.Sprintf("更新配置文件失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 更新运行时配置
	*h.config = newConfig
	config.TriggerCallbacks(h.config)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message": "配置已更新并生效"}`))
}

// validateConfig 验证配置
func (h *ConfigHandler) validateConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("配置不能为空")
	}

	// 验证MAP配置
	if cfg.MAP == nil {
		return fmt.Errorf("MAP配置不能为空")
	}

	for path, pathConfig := range cfg.MAP {
		if path == "" {
			return fmt.Errorf("路径不能为空")
		}
		if pathConfig.DefaultTarget == "" {
			return fmt.Errorf("路径 %s 的默认目标不能为空", path)
		}
		if _, err := url.Parse(pathConfig.DefaultTarget); err != nil {
			return fmt.Errorf("路径 %s 的默认目标URL无效: %v", path, err)
		}
	}

	// 验证FixedPaths配置
	for _, fp := range cfg.FixedPaths {
		if fp.Path == "" {
			return fmt.Errorf("固定路径不能为空")
		}
		if fp.TargetURL == "" {
			return fmt.Errorf("固定路径 %s 的目标URL不能为空", fp.Path)
		}
		if _, err := url.Parse(fp.TargetURL); err != nil {
			return fmt.Errorf("固定路径 %s 的目标URL无效: %v", fp.Path, err)
		}
	}

	return nil
}
