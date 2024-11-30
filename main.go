package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"proxy-go/internal/compression"
	"proxy-go/internal/config"
	"proxy-go/internal/handler"
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
	})

	// 添加压缩中间件
	var handler http.Handler = mainHandler
	if cfg.Compression.Gzip.Enabled || cfg.Compression.Brotli.Enabled {
		handler = middleware.CompressionMiddleware(compManager)(handler)
	}

	// 添加监控路由
	http.HandleFunc("/metrics", proxyHandler.AuthMiddleware(proxyHandler.MetricsHandler))
	http.HandleFunc("/metrics/ui", proxyHandler.MetricsPageHandler)
	http.HandleFunc("/metrics/auth", proxyHandler.MetricsAuthHandler)
	http.HandleFunc("/metrics/dashboard", proxyHandler.MetricsDashboardHandler)

	// 创建服务器
	server := &http.Server{
		Addr:    ":80",
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
	log.Println("Starting proxy server on :80")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal("Error starting server:", err)
	}
}
