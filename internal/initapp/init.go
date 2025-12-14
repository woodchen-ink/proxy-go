package initapp

import (
	"context"
	"log"
	"proxy-go/internal/config"
	"proxy-go/internal/handler"
	"proxy-go/internal/metrics"
	"proxy-go/internal/middleware"
	"proxy-go/internal/router"
	"proxy-go/internal/security"
	"proxy-go/internal/service"
	"proxy-go/pkg/sync"
	"time"
)

// InitOptions 初始化选项
type InitOptions struct {
	ConfigPath  string
	SyncTimeout time.Duration
}

// AppComponents 应用组件集合
type AppComponents struct {
	ConfigManager      *config.ConfigManager
	Config             *config.Config
	BanManager         *security.IPBanManager
	SecurityMiddleware *middleware.SecurityMiddleware
	MetricsService     *service.MetricsService
	AuthService        *service.AuthService
	// Handlers
	ProxyHandler     *handler.ProxyHandler
	MirrorHandler    *handler.MirrorProxyHandler
	ConfigHandler    *handler.ConfigHandler
	SecurityHandler  *handler.SecurityHandler
	AuthHandler      *handler.AuthHandler
	MetricsHandler   *handler.MetricsHandler
	PathStatsHandler *handler.PathStatsHandler
	// Routes
	AdminHandler router.RouteHandler
	MainRoutes   []router.RouteHandler
}

// Init 初始化应用程序（简化版本，兼容现有代码）
func Init(configPath string) (*config.ConfigManager, error) {
	components, err := InitApp(InitOptions{
		ConfigPath:  configPath,
		SyncTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return components.ConfigManager, nil
}

// InitApp 完整初始化应用程序
func InitApp(opts InitOptions) (*AppComponents, error) {
	return InitWithOptions(opts)
}

// InitWithOptions 使用选项初始化应用程序
func InitWithOptions(opts InitOptions) (*AppComponents, error) {
	log.Printf("[Init] 开始初始化应用程序...")

	components := &AppComponents{}
	var err error

	// 1. 初始化同步服务
	log.Printf("[Init] 正在初始化同步服务...")
	if err := sync.InitSyncService(); err != nil {
		log.Printf("[Init] 同步服务初始化失败: %v", err)
		log.Printf("[Init] 将使用本地配置")
	} else if sync.IsEnabled() {
		log.Printf("[Init] 同步服务初始化成功")

		// 2. 从 D1 下载最新配置
		log.Printf("[Init] 正在从 D1 下载最新配置...")
		ctx, cancel := context.WithTimeout(context.Background(), opts.SyncTimeout)
		configData, usedLocal, downloadErr := sync.DownloadConfigOnly(ctx)
		cancel()

		if downloadErr != nil {
			log.Printf("[Init] 下载 D1 配置失败: %v", downloadErr)
			log.Printf("[Init] 将使用本地配置")
		} else if configData != nil {
			// 使用 D1 配置数据初始化
			components.ConfigManager, err = config.NewConfigManagerFromData(opts.ConfigPath, configData)
			if err != nil {
				log.Printf("[Init] 从 D1 数据创建配置管理器失败: %v", err)
			} else {
				if usedLocal {
					log.Printf("[Init] D1 配置为空，已上传本地配置")
				} else {
					log.Printf("[Init] D1 配置下载完成")
				}
			}
		}
	}

	// 3. 如果还没有配置管理器，使用本地配置文件
	if components.ConfigManager == nil {
		log.Printf("[Init] 正在从本地文件初始化配置管理器...")
		components.ConfigManager, err = config.NewConfigManager(opts.ConfigPath)
		if err != nil {
			log.Printf("[Init] 配置管理器初始化失败: %v", err)
			return nil, err
		}
	}

	// 设置为全局配置管理器
	config.SetGlobalConfigManager(components.ConfigManager)
	components.Config = components.ConfigManager.GetConfig()
	log.Printf("[Init] 配置管理器初始化成功")

	// 4. 启动同步服务的定时任务
	if sync.IsEnabled() {
		ctx := context.Background()
		if err := sync.StartSyncService(ctx); err != nil {
			log.Printf("[Init] 同步服务启动失败: %v", err)
		} else {
			log.Printf("[Init] 同步服务已启动")

			// 注册配置更新回调
			config.RegisterUpdateCallback(func(cfg *config.Config) {
				sync.ConfigSyncCallback()
			})
		}
	}

	// 5. 初始化其他服务组件
	if err := initServices(components); err != nil {
		return nil, err
	}

	// 6. 创建处理器
	if err := createHandlers(components); err != nil {
		return nil, err
	}

	// 7. 设置路由
	if err := setupRoutes(components); err != nil {
		return nil, err
	}

	log.Printf("[Init] 应用程序初始化完成")
	return components, nil
}

// initServices 初始化各种服务
func initServices(components *AppComponents) error {
	log.Printf("[Init] 正在初始化应用服务...")

	// 初始化统计服务
	metrics.Init(components.Config)

	// 创建安全管理器
	if components.Config.Security.IPBan.Enabled {
		banConfig := &security.IPBanConfig{
			ErrorThreshold:         components.Config.Security.IPBan.ErrorThreshold,
			WindowMinutes:          components.Config.Security.IPBan.WindowMinutes,
			BanDurationMinutes:     components.Config.Security.IPBan.BanDurationMinutes,
			CleanupIntervalMinutes: components.Config.Security.IPBan.CleanupIntervalMinutes,
		}
		components.BanManager = security.NewIPBanManager(banConfig)
		components.SecurityMiddleware = middleware.NewSecurityMiddleware(components.BanManager)
	}

	// 创建服务层
	startTime := time.Now()
	components.MetricsService = service.NewMetricsService(startTime)
	components.AuthService = service.NewAuthServiceFromEnv()

	log.Printf("[Init] 应用服务初始化完成")
	return nil
}

// createHandlers 创建各种处理器
func createHandlers(components *AppComponents) error {
	log.Printf("[Init] 正在创建处理器...")

	// 创建代理处理器
	components.MirrorHandler = handler.NewMirrorProxyHandler()
	components.ProxyHandler = handler.NewProxyHandler(components.Config)

	// 创建配置处理器
	components.ConfigHandler = handler.NewConfigHandler(components.ConfigManager)

	// 创建安全管理处理器
	if components.BanManager != nil {
		components.SecurityHandler = handler.NewSecurityHandler(components.BanManager)
	}

	// 创建认证处理器
	components.AuthHandler = handler.NewAuthHandler(components.AuthService)

	// 创建指标处理器
	components.MetricsHandler = handler.NewMetricsHandler(components.MetricsService)

	// 创建路径统计处理器
	components.PathStatsHandler = handler.NewPathStatsHandler(metrics.GetCollector())

	log.Printf("[Init] 处理器创建完成")
	return nil
}

// setupRoutes 设置路由
func setupRoutes(components *AppComponents) error {
	log.Printf("[Init] 正在设置路由...")

	// 设置路由
	_, components.AdminHandler = router.SetupAdminRoutes(
		components.ProxyHandler,
		components.AuthHandler,
		components.MetricsHandler,
		components.MirrorHandler,
		components.ConfigHandler,
		components.SecurityHandler,
		components.PathStatsHandler,
	)
	components.MainRoutes = router.SetupMainRoutes(components.MirrorHandler, components.ProxyHandler, components.ConfigManager)

	log.Printf("[Init] 路由设置完成")
	return nil
}
