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
	"syscall"
)

func main() {
	// 加载配置
	cfg, err := config.Load("data/config.json")
	if err != nil {
		log.Fatal("Error loading config:", err)
	}

	// 创建压缩管理器，直接使用配置文件中的压缩配置
	compManager := compression.NewManager(compression.Config{
		Gzip:   compression.CompressorConfig(cfg.Compression.Gzip),
		Brotli: compression.CompressorConfig(cfg.Compression.Brotli),
	})

	// 创建代理处理器
	proxyHandler := handler.NewProxyHandler(cfg.MAP)

	// 添加中间件
	var handler http.Handler = proxyHandler
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
