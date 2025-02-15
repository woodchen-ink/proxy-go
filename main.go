package main

import (
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
	"text/template"
)

func main() {
	// 加载配置
	cfg, err := config.Load("data/config.json")
	if err != nil {
		log.Fatal("Error loading config:", err)
	}

	// 加载模板
	tmpl, err := template.ParseFiles(
		"/app/web/templates/admin/layout.html",
		"/app/web/templates/admin/login.html",
		"/app/web/templates/admin/metrics.html",
		"/app/web/templates/admin/config.html",
	)
	if err != nil {
		log.Fatal("Error parsing templates:", err)
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
				log.Printf("[Debug] 处理管理路由: %s", r.URL.Path)

				// 处理静态文件
				if strings.HasPrefix(r.URL.Path, "/admin/static/") {
					log.Printf("[Debug] 处理静态文件: %s", r.URL.Path)
					http.StripPrefix("/admin/static/", http.FileServer(http.Dir("/app/web/static"))).ServeHTTP(w, r)
					return
				}

				switch r.URL.Path {
				case "/admin/login":
					log.Printf("[Debug] 提供登录页面，文件路径: /app/web/templates/admin/login.html")
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					if err := tmpl.ExecuteTemplate(w, "login.html", nil); err != nil {
						log.Printf("[Error] 渲染登录页面失败: %v", err)
						http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					}
				case "/admin/metrics":
					proxyHandler.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						if err := tmpl.ExecuteTemplate(w, "metrics.html", nil); err != nil {
							log.Printf("[Error] 渲染监控页面失败: %v", err)
							http.Error(w, "Internal Server Error", http.StatusInternalServerError)
						}
					}))(w, r)
				case "/admin/config":
					proxyHandler.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/html; charset=utf-8")
						if err := tmpl.ExecuteTemplate(w, "config.html", nil); err != nil {
							log.Printf("[Error] 渲染配置页面失败: %v", err)
							http.Error(w, "Internal Server Error", http.StatusInternalServerError)
						}
					}))(w, r)
				case "/admin/config/get":
					proxyHandler.AuthMiddleware(handler.NewConfigHandler(cfg).ServeHTTP)(w, r)
				case "/admin/config/save":
					proxyHandler.AuthMiddleware(handler.NewConfigHandler(cfg).ServeHTTP)(w, r)
				case "/admin/auth":
					proxyHandler.AuthHandler(w, r)
				default:
					log.Printf("[Debug] 未找到管理路由: %s", r.URL.Path)
					http.NotFound(w, r)
				}
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
		log.Printf("[Debug] 收到请求: %s %s", r.Method, r.URL.Path)

		// 处理静态文件
		if strings.HasPrefix(r.URL.Path, "/web/static/") {
			log.Printf("[Debug] 处理静态文件: %s", r.URL.Path)
			http.StripPrefix("/web/static/", http.FileServer(http.Dir("/app/web/static"))).ServeHTTP(w, r)
			return
		}

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
