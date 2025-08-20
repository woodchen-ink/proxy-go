package handler

import (
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/service"
)

// RedirectHandler 处理302跳转逻辑
type RedirectHandler struct {
	redirectService *service.RedirectService
}

// NewRedirectHandler 创建新的跳转处理器
func NewRedirectHandler(ruleService *service.RuleService) *RedirectHandler {
	return &RedirectHandler{
		redirectService: service.NewRedirectService(ruleService),
	}
}

// HandleRedirect 处理302跳转请求
func (rh *RedirectHandler) HandleRedirect(w http.ResponseWriter, r *http.Request, pathConfig config.PathConfig, targetPath string, client *http.Client) bool {
	result := rh.redirectService.HandleRedirect(r, pathConfig, targetPath, client)

	if !result.ShouldRedirect {
		return false
	}

	// 执行302跳转
	rh.redirectService.PerformRedirect(w, r, result.TargetURL)
	return true
}


// IsRedirectEnabled 检查路径配置是否启用了任何形式的302跳转
func (rh *RedirectHandler) IsRedirectEnabled(pathConfig config.PathConfig) bool {
	return rh.redirectService.IsRedirectEnabled(pathConfig)
}
