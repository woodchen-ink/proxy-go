package router

import (
	"encoding/json"
	"net/http"
	"os"
	"proxy-go/internal/handler"
	"strings"
)

// Route 定义路由结构
type Route struct {
	Method      string
	Pattern     string
	Handler     http.HandlerFunc
	RequireAuth bool
}

// SetupAdminRoutes 设置管理员路由
func SetupAdminRoutes(proxyHandler *handler.ProxyHandler, authHandler *handler.AuthHandler, metricsHandler *handler.MetricsHandler, mirrorHandler *handler.MirrorProxyHandler, configHandler *handler.ConfigHandler, securityHandler *handler.SecurityHandler, healthHandler *handler.HealthHandler, pathStatsHandler *handler.PathStatsHandler) ([]Route, RouteHandler) {
	// 定义API路由
	apiRoutes := []Route{
		{http.MethodGet, "/admin/api/auth", authHandler.LoginHandler, false},
		{http.MethodGet, "/admin/api/oauth/callback", authHandler.OAuthCallbackHandler, false},
		{http.MethodGet, "/admin/api/check-auth", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]bool{"authenticated": true})
		}, true},
		{http.MethodPost, "/admin/api/logout", authHandler.LogoutHandler, false},
		{http.MethodGet, "/admin/api/metrics", metricsHandler.GetMetrics, true},
		{http.MethodGet, "/admin/api/config/get", configHandler.ServeHTTP, true},
		{http.MethodPost, "/admin/api/config/save", configHandler.ServeHTTP, true},
		{http.MethodGet, "/admin/api/cache/stats", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).GetCacheStats, true},
		{http.MethodPost, "/admin/api/cache/enable", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).SetCacheEnabled, true},
		{http.MethodPost, "/admin/api/cache/clear", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).ClearCache, true},
		{http.MethodGet, "/admin/api/cache/config", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).GetCacheConfig, true},
		{http.MethodPost, "/admin/api/cache/config", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).UpdateCacheConfig, true},
		{http.MethodGet, "/admin/api/health/status", healthHandler.GetHealthStatus, true},
		{http.MethodPost, "/admin/api/health/reset", healthHandler.ResetTargetHealth, true},
		{http.MethodGet, "/admin/api/path-stats", pathStatsHandler.GetAllPathStats, true},
	}

	// 添加安全API路由（如果启用了安全功能）
	if securityHandler != nil {
		securityRoutes := []Route{
			{http.MethodGet, "/admin/api/security/banned-ips", securityHandler.GetBannedIPs, true},
			{http.MethodPost, "/admin/api/security/unban", securityHandler.UnbanIP, true},
			{http.MethodGet, "/admin/api/security/stats", securityHandler.GetSecurityStats, true},
			{http.MethodGet, "/admin/api/security/check-ip", securityHandler.CheckIPStatus, true},
			{http.MethodGet, "/admin/api/security/ban-history", securityHandler.GetBanHistory, true},
		}
		apiRoutes = append(apiRoutes, securityRoutes...)
	}

	// 管理路由处理器
	adminHandler := RouteHandler{
		Matcher: func(r *http.Request) bool {
			return strings.HasPrefix(r.URL.Path, "/admin/")
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// API请求处理
			if strings.HasPrefix(r.URL.Path, "/admin/api/") {
				for _, route := range apiRoutes {
					if r.URL.Path == route.Pattern && r.Method == route.Method {
						if route.RequireAuth {
							authHandler.AuthMiddleware(route.Handler)(w, r)
						} else {
							route.Handler(w, r)
						}
						return
					}
				}

				if r.URL.Path != "/admin/api/404" {
					http.Error(w, "Not found", http.StatusNotFound)
				}
				return
			}

			// 静态文件处理
			path := r.URL.Path
			if path == "/admin" || path == "/admin/" {
				path = "/admin/index.html"
			}

			filePath := "web/out" + strings.TrimPrefix(path, "/admin")
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				filePath = "web/out/index.html"
			}
			http.ServeFile(w, r, filePath)
		}),
	}

	return apiRoutes, adminHandler
}
