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
	"proxy-go/internal/metrics"
	"proxy-go/internal/middleware"
	"strings"
	"syscall"
)

func main() {
	// 加载配置
	cfg, err := config.Load("data/config.json")
	if err != nil {
		log.Fatal("Error loading config:", err)
	}

	// 更新常量配置
	constants.UpdateFromConfig(cfg)

	// 初始化指标收集器
	if err := metrics.InitCollector(cfg); err != nil {
		log.Fatal("Error initializing metrics collector:", err)
	}

	// 创建压缩管理器
	compManager := compression.NewManager(compression.Config{
		Gzip:   compression.CompressorConfig(cfg.Compression.Gzip),
		Brotli: compression.CompressorConfig(cfg.Compression.Brotli),
	})

	// 创建代理处理器
	mirrorHandler := handler.NewMirrorProxyHandler()
	proxyHandler := handler.NewProxyHandler(cfg)
	fixedPathCache := middleware.GetFixedPathCache()

	// 创建处理器链
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
					switch r.URL.Path {
					case "/admin/api/auth":
						if r.Method == http.MethodPost {
							proxyHandler.AuthHandler(w, r)
						} else {
							http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
						}
					case "/admin/api/check-auth":
						proxyHandler.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusOK)
							json.NewEncoder(w).Encode(map[string]bool{"authenticated": true})
						}))(w, r)
					case "/admin/api/logout":
						if r.Method == http.MethodPost {
							proxyHandler.LogoutHandler(w, r)
						} else {
							http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
						}
					case "/admin/api/metrics":
						proxyHandler.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							proxyHandler.MetricsHandler(w, r)
						}))(w, r)
					case "/admin/api/config/get":
						proxyHandler.AuthMiddleware(handler.NewConfigHandler(cfg).ServeHTTP)(w, r)
					case "/admin/api/config/save":
						proxyHandler.AuthMiddleware(handler.NewConfigHandler(cfg).ServeHTTP)(w, r)
					case "/admin/api/cache/stats":
						proxyHandler.AuthMiddleware(handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache, fixedPathCache).GetCacheStats)(w, r)
					case "/admin/api/cache/enable":
						proxyHandler.AuthMiddleware(handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache, fixedPathCache).SetCacheEnabled)(w, r)
					case "/admin/api/cache/clear":
						proxyHandler.AuthMiddleware(handler.NewCacheAdminHandler(proxyHandler.Cache, mirrorHandler.Cache, fixedPathCache).ClearCache)(w, r)
					default:
						http.NotFound(w, r)
					}
					return
				}

				// 静态文件处理
				path := r.URL.Path
				if path == "/admin" || path == "/admin/" {
					path = "/admin/index.html"
				}

				// 从web/out目录提供静态文件
				filePath := "web/out" + strings.TrimPrefix(path, "/admin")
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					// 如果文件不存在，返回index.html（用于客户端路由）
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
		// 固定路径处理器
		{
			matcher: func(r *http.Request) bool {
				for _, fp := range cfg.FixedPaths {
					if strings.HasPrefix(r.URL.Path, fp.Path) {
						return true
					}
				}
				return false
			},
			handler: middleware.FixedPathProxyMiddleware(cfg.FixedPaths)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})),
		},
		// 默认代理处理器
		{
			matcher: func(r *http.Request) bool {
				return true // 总是匹配，作为默认处理器
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
