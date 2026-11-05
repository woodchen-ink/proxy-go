package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"proxy-go/internal/control"
	"proxy-go/internal/initapp"
	"proxy-go/internal/metrics"
	"proxy-go/internal/upgrade"
	"proxy-go/pkg/sync"
	gosync "sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// 自更新试运行用: 只打印版本号后退出
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(control.Version)
		return
	}
	// 同一镜像下有自更新程序时转交过去 (不返回)
	upgrade.Launch(control.BuildTimeUnix())

	// 加载.env文件（如果存在）
	if err := godotenv.Load(".env"); err != nil {
		log.Printf("[Init] .env文件不存在或加载失败: %v", err)
	} else {
		log.Printf("[Init] .env文件加载成功")
	}

	// 初始化应用程序（完整初始化：同步、配置、服务、处理器、路由）
	// 注意: configPath 仅作为 fallback，优先使用 D1 配置
	components, err := initapp.InitApp(initapp.InitOptions{
		ConfigPath:  "data/config.json", // fallback 本地配置路径
		SyncTimeout: 30 * time.Second,
	})
	if err != nil {
		log.Fatal("Error initializing application:", err)
	}

	// 创建路由处理器
	handlers := components.MainRoutes

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

	// 停服流程: 信号退出与自更新 exec 前共用
	var stopOnce gosync.Once
	shutdown := func(drain time.Duration) {
		stopOnce.Do(func() {
			log.Println("Shutting down server...")
			if components.BanManager != nil {
				components.BanManager.Stop()
			}
			metrics.StopMetricsStorage()
			if collector := metrics.GetCollector(); collector != nil {
				collector.Stop(5 * time.Second)
			}
			if err := sync.StopSyncService(); err != nil {
				log.Printf("Error stopping sync service: %v", err)
			}
			// 自更新时给在途请求一个短暂的收尾窗口, 超时强关
			if drain > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), drain)
				_ = server.Shutdown(ctx)
				cancel()
			}
			if err := server.Close(); err != nil {
				log.Printf("Error during server shutdown: %v", err)
			}
		})
	}
	upgrade.SetShutdown(func() { shutdown(5 * time.Second) })

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		shutdown(0)
	}()

	// 启动服务器
	log.Println("Starting proxy server on :3336")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal("Error starting server:", err)
	}
	// 自更新中: 等 exec 替换进程, main 返回会直接结束进程
	if upgrade.Restarting() {
		select {}
	}
}
