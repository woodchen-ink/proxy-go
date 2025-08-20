package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"proxy-go/internal/config"
	"proxy-go/internal/constants"
	"proxy-go/internal/handler"
	"proxy-go/internal/initapp"
	"proxy-go/internal/metrics"
	"proxy-go/internal/middleware"
	"proxy-go/internal/router"
	"proxy-go/internal/security"
	"proxy-go/internal/service"
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

	// 创建安全管理器
	var banManager *security.IPBanManager
	var securityMiddleware *middleware.SecurityMiddleware
	if cfg.Security.IPBan.Enabled {
		banConfig := &security.IPBanConfig{
			ErrorThreshold:         cfg.Security.IPBan.ErrorThreshold,
			WindowMinutes:          cfg.Security.IPBan.WindowMinutes,
			BanDurationMinutes:     cfg.Security.IPBan.BanDurationMinutes,
			CleanupIntervalMinutes: cfg.Security.IPBan.CleanupIntervalMinutes,
		}
		banManager = security.NewIPBanManager(banConfig)
		securityMiddleware = middleware.NewSecurityMiddleware(banManager)
	}

	// 创建服务层
	startTime := time.Now()
	metricsService := service.NewMetricsService(startTime)
	authService := service.NewAuthServiceFromEnv()

	// 创建代理处理器
	mirrorHandler := handler.NewMirrorProxyHandler()
	proxyHandler := handler.NewProxyHandler(cfg)

	// 创建配置处理器
	configHandler := handler.NewConfigHandler(configManager)

	// 创建安全管理处理器
	var securityHandler *handler.SecurityHandler
	if banManager != nil {
		securityHandler = handler.NewSecurityHandler(banManager)
	}

	// 创建认证处理器
	authHandler := handler.NewAuthHandler(authService)
	
	// 创建指标处理器
	metricsHandler := handler.NewMetricsHandler(metricsService)

	// 设置路由
	_, adminHandler := router.SetupAdminRoutes(proxyHandler, authHandler, metricsHandler, mirrorHandler, configHandler, securityHandler)
	mainRoutes := router.SetupMainRoutes(mirrorHandler, proxyHandler)

	// 创建路由处理器
	handlers := []router.RouteHandler{adminHandler}
	handlers = append(handlers, mainRoutes...)

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
	if securityMiddleware != nil {
		handler = securityMiddleware.IPBanMiddleware(handler)
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
		if banManager != nil {
			banManager.Stop()
		}

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
