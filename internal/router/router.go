package router

import (
	"net/http"
	"os"
	"proxy-go/internal/handler"
	"strings"
)

// RouteHandler 定义路由处理器结构
type RouteHandler struct {
	Matcher func(*http.Request) bool
	Handler http.Handler
}

// SetupMainRoutes 设置主要路由
func SetupMainRoutes(mirrorHandler *handler.MirrorProxyHandler, proxyHandler *handler.ProxyHandler) []RouteHandler {
	return []RouteHandler{
		// favicon.ico 处理器
		{
			Matcher: func(r *http.Request) bool {
				return r.URL.Path == "/favicon.ico"
			},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// 检查是否有自定义favicon文件
				faviconPath := "favicon/favicon.ico"
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
