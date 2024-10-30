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
	proxyHandler := handler.NewProxyHandler(cfg.MAP)

	// 创建 CDNJS 中间件配置
	cdnjsConfigs := []middleware.CDNJSConfig{
		{
			Path:       "/cdnjs",
			TargetHost: "cdnjs.cloudflare.com",
			TargetURL:  "https://cdnjs.cloudflare.com",
		},
		{
			Path:       "/jsdelivr",
			TargetHost: "cdn.jsdelivr.net",
			TargetURL:  "https://cdn.jsdelivr.net",
		},
	}

	// 创建主处理器
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/cdnjs") {
			// CDNJS 请求使用 CDNJS 中间件处理
			handler := middleware.CDNJSMiddleware(cdnjsConfigs)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			handler.ServeHTTP(w, r)
		} else {
			// 非 CDNJS 请求使用普通代理处理器
			proxyHandler.ServeHTTP(w, r)
		}
	})

	// 对非 CDNJS 请求添加压缩中间件
	var handler http.Handler = mainHandler
	if cfg.Compression.Gzip.Enabled || cfg.Compression.Brotli.Enabled {
		handler = middleware.CompressionMiddleware(compManager)(handler)
	}

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
