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

	// 创建处理器链
	handlers := []struct {
		pathPrefix string
		handler    http.Handler
	}{
		// 固定路径处理器
		{
			pathPrefix: "", // 空字符串表示检查所有 FixedPaths 配置
			handler:    middleware.FixedPathProxyMiddleware(cfg.FixedPaths)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})),
		},
		// 可以在这里添加其他固定路径处理器
		// {
		//     pathPrefix: "/something",
		//     handler:    someOtherMiddleware(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})),
		// },
		// 默认代理处理器放在最后
		{
			pathPrefix: "",
			handler:    proxyHandler,
		},
	}

	// 创建主处理器
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 遍历所有处理器
		for _, h := range handlers {
			if h.pathPrefix == "" || strings.HasPrefix(r.URL.Path, h.pathPrefix) {
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
