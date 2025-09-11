package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"proxy-go/internal/initapp"
	"proxy-go/internal/metrics"
	"proxy-go/internal/router"
	"proxy-go/pkg/sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// 加载.env文件（如果存在）
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("[Init] .env文件不存在或加载失败: %v", err)
	} else {
		log.Printf("[Init] .env文件加载成功")
	}

	// 初始化应用程序（完整初始化：同步、配置、服务、处理器、路由）
	configPath := "data/config.json"
	components, err := initapp.InitApp(initapp.InitOptions{
		ConfigPath:      configPath,
		SyncTimeout:     30 * time.Second,
		EnableSync:      true,
		FallbackOnError: true,
	})
	if err != nil {
		log.Fatal("Error initializing application:", err)
	}

	// 创建路由处理器
	handlers := []router.RouteHandler{components.AdminHandler}
	handlers = append(handlers, components.MainRoutes...)

	// 创建主处理器
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 遍历所有处理器
		for _, h := range handlers {
			if h.Matcher(r) {
				h.Handler.ServeHTTP(w, r)
				return
			}
		}

		log.Printf("[Debug] 未找到处理器: %s", r.URL.Path)
		http.NotFound(w, r)
	})

	// 构建中间件链
	var handler http.Handler = mainHandler

	// 添加安全中间件（最外层，优先级最高）
	if components.SecurityMiddleware != nil {
		handler = components.SecurityMiddleware.IPBanMiddleware(handler)
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

		// 停止安全管理器
		if components.BanManager != nil {
			components.BanManager.Stop()
		}

		// 停止指标存储服务
		metrics.StopMetricsStorage()

		// 停止同步服务
		if err := sync.StopSyncService(); err != nil {
			log.Printf("Error stopping sync service: %v", err)
		}

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
