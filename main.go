package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"proxy-go/internal/compression"
	"proxy-go/internal/config"
	"proxy-go/internal/constants"
	"proxy-go/internal/handler"
	"proxy-go/internal/initapp"
	"proxy-go/internal/metrics"
	"proxy-go/internal/middleware"
	"strings"
	"syscall"
)

// Route 定义路由结构
type Route struct {
	Method      string
	Pattern     string
	Handler     http.HandlerFunc
	RequireAuth bool
}

func main() {

	// 初始化应用程序（包括配置迁移）
	configPath := "data/config.json"
	initapp.Init(configPath)

	// 初始化配置管理器
	configManager, err := config.Init(configPath)
	if err != nil {
		log.Fatal("Error initializing config manager:", err)
	}

	// 获取配置
	cfg := configManager.GetConfig()

	// 更新常量配置
	constants.UpdateFromConfig(cfg)

	// 初始化统计服务
	metrics.Init(cfg)

	// 创建压缩管理器
	compManager := compression.NewManager(compression.Config{
		Gzip:   compression.CompressorConfig(cfg.Compression.Gzip),
		Brotli: compression.CompressorConfig(cfg.Compression.Brotli),
	})

	// 创建代理处理器
	mirrorHandler := handler.NewMirrorProxyHandler()
	proxyHandler := handler.NewProxyHandler(cfg)

	// 定义API路由
	apiRoutes := []Route{
		{http.MethodGet, "/admin/api/auth", proxyHandler.LoginHandler, false},
		{http.MethodGet, "/admin/api/oauth/callback", proxyHandler.OAuthCallbackHandler, false},
		{http.MethodGet, "/admin/api/check-auth", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]bool{"authenticated": true})
		}, true},
		{http.MethodPost, "/admin/api/logout", proxyHandler.LogoutHandler, false},
		{http.MethodGet, "/admin/api/metrics", proxyHandler.MetricsHandler, true},
		{http.MethodGet, "/admin/api/config/get", handler.NewConfigHandler(cfg).ServeHTTP, true},
		{http.MethodPost, "/admin/api/config/save", handler.NewConfigHandler(cfg).ServeHTTP, true},
		{http.MethodGet, "/admin/api/cache/stats", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).GetCacheStats, true},
		{http.MethodPost, "/admin/api/cache/enable", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).SetCacheEnabled, true},
		{http.MethodPost, "/admin/api/cache/clear", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).ClearCache, true},
		{http.MethodGet, "/admin/api/cache/config", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).GetCacheConfig, true},
		{http.MethodPost, "/admin/api/cache/config", handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache).UpdateCacheConfig, true},
	}

	// 创建路由处理器
	handlers := []struct {
		matcher func(*http.Request) bool
		handler http.Handler
	}{
		// 管理路由处理器
		{
			matcher: func(r *http.Request) bool {
				return strings.HasPrefix(r.URL.Path, "/admin/")
			},
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// API请求处理
				if strings.HasPrefix(r.URL.Path, "/admin/api/") {
					for _, route := range apiRoutes {
						if r.URL.Path == route.Pattern && r.Method == route.Method {
							if route.RequireAuth {
								proxyHandler.AuthMiddleware(route.Handler)(w, r)
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
		},
		// Mirror代理处理器
		{
			matcher: func(r *http.Request) bool {
				return strings.HasPrefix(r.URL.Path, "/mirror/")
			},
			handler: mirrorHandler,
		},
		// 默认代理处理器
		{
			matcher: func(r *http.Request) bool {
				return true
			},
			handler: proxyHandler,
		},
	}

	// 创建主处理器
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 遍历所有处理器
		for _, h := range handlers {
			if h.matcher(r) {
				h.handler.ServeHTTP(w, r)
				return
			}
		}

		log.Printf("[Debug] 未找到处理器: %s", r.URL.Path)
		http.NotFound(w, r)
	})

	// 添加压缩中间件
	var handler http.Handler = mainHandler
	if cfg.Compression.Gzip.Enabled || cfg.Compression.Brotli.Enabled {
		handler = middleware.CompressionMiddleware(compManager)(handler)
	}

	// 创建服务器
	server := &http.Server{
		Addr:    ":3336",
		Handler: handler,
	}

	// 优雅关闭
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down server...")

		// 停止指标存储服务
		metrics.StopMetricsStorage()

		if err := server.Close(); err != nil {
			log.Printf("Error during server shutdown: %v\n", err)
		}
	}()

	// 启动服务器
	log.Println("Starting proxy server on :3336")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal("Error starting server:", err)
	}
}
