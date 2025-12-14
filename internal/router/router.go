package router

import (
	"io"
	"log"
	"net/http"
	"os"
	"proxy-go/internal/config"
	"proxy-go/internal/handler"
	"strings"
)

// RouteHandler 定义路由处理器结构
type RouteHandler struct {
	Matcher func(*http.Request) bool
	Handler http.Handler
}

// SetupMainRoutes 设置主要路由
func SetupMainRoutes(mirrorHandler *handler.MirrorProxyHandler, proxyHandler *handler.ProxyHandler, configManager *config.ConfigManager) []RouteHandler {
	return []RouteHandler{
		// favicon.ico 处理器
		{
			Matcher: func(r *http.Request) bool {
				return r.URL.Path == "/favicon.ico"
			},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cfg := configManager.GetConfig()

				// 优先使用配置中的 FaviconURL (支持环境变量 FAVICON_URL 覆盖)
				if cfg.FaviconURL != "" {
					// 从 URL 代理 favicon
					resp, err := http.Get(cfg.FaviconURL)
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

					// 复制内容
					if _, err := io.Copy(w, resp.Body); err != nil {
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
