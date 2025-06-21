package handler

import (
	"log"
	"net/http"
	"net/url"
	"proxy-go/internal/config"
	"proxy-go/internal/service"
	"proxy-go/internal/utils"
	"strings"

	"github.com/woodchen-ink/go-web-utils/iputil"
)

// RedirectHandler 处理302跳转逻辑
type RedirectHandler struct {
	ruleService *service.RuleService
}

// NewRedirectHandler 创建新的跳转处理器
func NewRedirectHandler(ruleService *service.RuleService) *RedirectHandler {
	return &RedirectHandler{
		ruleService: ruleService,
	}
}

// HandleRedirect 处理302跳转请求
func (rh *RedirectHandler) HandleRedirect(w http.ResponseWriter, r *http.Request, pathConfig config.PathConfig, targetPath string, client *http.Client) bool {
	// 检查是否需要进行302跳转
	shouldRedirect, targetURL := rh.shouldRedirect(r, pathConfig, targetPath, client)

	if !shouldRedirect {
		return false
	}

	// 执行302跳转
	rh.performRedirect(w, r, targetURL)
	return true
}

// shouldRedirect 判断是否应该进行302跳转，并返回目标URL（优化版本）
func (rh *RedirectHandler) shouldRedirect(r *http.Request, pathConfig config.PathConfig, targetPath string, client *http.Client) (bool, string) {
	// 使用service包的规则选择函数，传递请求的域名
	result := rh.ruleService.SelectRuleForRedirect(client, pathConfig, targetPath, r.Host)

	if result.ShouldRedirect {
		// 构建完整的目标URL
		targetURL := rh.buildTargetURL(result.TargetURL, targetPath, r.URL.RawQuery)

		if result.Rule != nil {
			log.Printf("[Redirect] %s -> 使用选中规则进行302跳转 (域名: %s): %s", targetPath, r.Host, targetURL)
		} else {
			log.Printf("[Redirect] %s -> 使用默认目标进行302跳转 (域名: %s): %s", targetPath, r.Host, targetURL)
		}

		return true, targetURL
	}

	return false, ""
}

// buildTargetURL 构建完整的目标URL
func (rh *RedirectHandler) buildTargetURL(baseURL, targetPath, rawQuery string) string {
	// URL 解码，然后重新编码，确保特殊字符被正确处理
	decodedPath, err := url.QueryUnescape(targetPath)
	if err != nil {
		// 如果解码失败，使用原始路径
		decodedPath = targetPath
	}

	// 重新编码路径，保留 '/'
	parts := strings.Split(decodedPath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(parts, "/")

	// 构建完整URL
	targetURL := baseURL + encodedPath

	// 添加查询参数
	if rawQuery != "" {
		targetURL = targetURL + "?" + rawQuery
	}

	return targetURL
}

// performRedirect 执行302跳转
func (rh *RedirectHandler) performRedirect(w http.ResponseWriter, r *http.Request, targetURL string) {
	// 设置302跳转响应头
	w.Header().Set("Location", targetURL)
	w.Header().Set("Proxy-Go-Redirect", "1")

	// 添加缓存控制头，避免浏览器缓存跳转响应
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// 设置状态码为302
	w.WriteHeader(http.StatusFound)

	// 记录跳转日志
	clientIP := iputil.GetClientIP(r)
	log.Printf("[Redirect] %s %s -> 302 %s (%s) from %s",
		r.Method, r.URL.Path, targetURL, clientIP, utils.GetRequestSource(r))
}

// IsRedirectEnabled 检查路径配置是否启用了任何形式的302跳转
func (rh *RedirectHandler) IsRedirectEnabled(pathConfig config.PathConfig) bool {
	// 检查默认目标是否启用跳转
	if pathConfig.RedirectMode {
		return true
	}

	// 检查扩展名规则是否有启用跳转的
	for _, rule := range pathConfig.ExtRules {
		if rule.RedirectMode {
			return true
		}
	}

	return false
}
