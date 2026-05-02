package router

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"proxy-go/internal/config"
	"proxy-go/internal/handler"
	"strings"
	"time"
)

// faviconHTTPClient 限制超时，防止远程 favicon 拖垮请求
var faviconHTTPClient = &http.Client{Timeout: 10 * time.Second}

const maxFaviconBytes = 5 * 1024 * 1024 // 5MB

// validateFaviconURL 校验 favicon URL，防止 SSRF
func validateFaviconURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q (only http/https allowed)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	// 拒绝纯 IP 形式的私有/回环/链路本地/多播地址
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("private/loopback ip not allowed: %s", host)
		}
	}
	// 拒绝常见内网主机名
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") ||
		strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("private hostname not allowed: %s", host)
	}
	return nil
}

// RouteHandler 定义路由处理器结构
type RouteHandler struct {
	Matcher func(*http.Request) bool
	Handler http.Handler
}

// SetupMainRoutes 设置主要路由
func SetupMainRoutes(mirrorHandler *handler.MirrorProxyHandler, proxyHandler *handler.ProxyHandler, configManager *config.ConfigManager) []RouteHandler {
	remoteCacheHandler := handler.NewCacheRemoteHandler(proxyHandler.Cache, mirrorHandler.Cache)

	return []RouteHandler{
		// 远程缓存清理接口
		{
			Matcher: func(r *http.Request) bool {
				return r.URL.Path == "/api/cache/clear-url"
			},
			Handler: http.HandlerFunc(remoteCacheHandler.ClearCacheByURL),
		},
		// favicon.ico 处理器
		{
			Matcher: func(r *http.Request) bool {
				return r.URL.Path == "/favicon.ico"
			},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cfg := configManager.GetConfig()

				// 优先使用配置中的 FaviconURL (支持环境变量 FAVICON_URL 覆盖)
				if cfg.FaviconURL != "" {
					// 校验 URL 防止 SSRF
					if err := validateFaviconURL(cfg.FaviconURL); err != nil {
						log.Printf("[Favicon] 拒绝不安全的 favicon URL: %v", err)
						http.NotFound(w, r)
						return
					}
					// 从 URL 代理 favicon
					resp, err := faviconHTTPClient.Get(cfg.FaviconURL)
					if err != nil {
						log.Printf("[Favicon] 获取远程 favicon 失败: %v", err)
						http.NotFound(w, r)
						return
					}
					defer resp.Body.Close()

					if resp.StatusCode != http.StatusOK {
						log.Printf("[Favicon] 远程 favicon 返回非 200 状态码: %d", resp.StatusCode)
						http.NotFound(w, r)
						return
					}

					// 设置响应头
					w.Header().Set("Content-Type", "image/x-icon")
					w.Header().Set("Cache-Control", "public, max-age=31536000") // 1年缓存

					// 复制内容（限制最大体积，防止下游恶意大文件）
					if _, err := io.Copy(w, io.LimitReader(resp.Body, maxFaviconBytes)); err != nil {
						log.Printf("[Favicon] 复制 favicon 内容失败: %v", err)
					}
					return
				}

				// 回退到本地文件 web/public/favicon.ico
				faviconPath := "web/public/favicon.ico"
				if _, err := os.Stat(faviconPath); err == nil {
					// 设置正确的Content-Type和缓存头
					w.Header().Set("Content-Type", "image/x-icon")
					w.Header().Set("Cache-Control", "public, max-age=31536000") // 1年缓存
					http.ServeFile(w, r, faviconPath)
				} else {
					// 如果没有自定义favicon，返回404
					http.NotFound(w, r)
				}
			}),
		},
		// Mirror代理处理器
		{
			Matcher: func(r *http.Request) bool {
				return strings.HasPrefix(r.URL.Path, "/mirror/")
			},
			Handler: mirrorHandler,
		},
		// 默认代理处理器
		{
			Matcher: func(r *http.Request) bool {
				return true
			},
			Handler: proxyHandler,
		},
	}
}
