package initapp

import (
	"context"
	"log"
	"proxy-go/internal/config"
	"proxy-go/internal/constants"
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
	ConfigPath      string
	SyncTimeout     time.Duration
	EnableSync      bool
	FallbackOnError bool // 同步失败时是否回退到本地配置
}

// AppComponents 应用组件集合
type AppComponents struct {
	ConfigManager     *config.ConfigManager
	Config            *config.Config
	BanManager        *security.IPBanManager
	SecurityMiddleware *middleware.SecurityMiddleware
	MetricsService    *service.MetricsService
	AuthService       *service.AuthService
	// Handlers
	ProxyHandler    *handler.ProxyHandler
	MirrorHandler   *handler.MirrorProxyHandler
	ConfigHandler   *handler.ConfigHandler
	SecurityHandler *handler.SecurityHandler
	AuthHandler     *handler.AuthHandler
	MetricsHandler  *handler.MetricsHandler
	HealthHandler   *handler.HealthHandler
	// Routes
	AdminHandler router.RouteHandler
	MainRoutes   []router.RouteHandler
}

// Init 初始化应用程序（简化版本，兼容现有代码）
func Init(configPath string) (*config.ConfigManager, error) {
	components, err := InitApp(InitOptions{
		ConfigPath:      configPath,
		SyncTimeout:     30 * time.Second,
		EnableSync:      true,
		FallbackOnError: true,
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

	// 1. 尝试初始化同步服务
	if opts.EnableSync {
		log.Printf("[Init] 正在初始化同步服务...")
		if err := sync.InitSyncService(); err != nil {
			log.Printf("[Init] 同步服务初始化失败: %v", err)
			if !opts.FallbackOnError {
				return nil, err
			}
			log.Printf("[Init] 将使用本地配置")
		} else {
			log.Printf("[Init] 同步服务初始化成功")

			// 2. 从远程下载最新配置
			log.Printf("[Init] 正在从远程下载最新配置...")
			ctx, cancel := context.WithTimeout(context.Background(), opts.SyncTimeout)
			if err := sync.DownloadConfigOnly(ctx); err != nil {
				log.Printf("[Init] 下载远程配置失败: %v", err)
				if !opts.FallbackOnError {
					cancel()
					return nil, err
				}
				log.Printf("[Init] 将使用本地配置")
			} else {
				log.Printf("[Init] 远程配置下载完成")
			}
			cancel()
		}
	}

	// 3. 初始化配置管理器（使用已下载的配置或本地配置）
	log.Printf("[Init] 正在初始化配置管理器...")
	components.ConfigManager, err = config.NewConfigManager(opts.ConfigPath)
	if err != nil {
		log.Printf("[Init] 配置管理器初始化失败: %v", err)
		return nil, err
	}
	
	// 设置为全局配置管理器
	config.SetGlobalConfigManager(components.ConfigManager)
	components.Config = components.ConfigManager.GetConfig()
	log.Printf("[Init] 配置管理器初始化成功")

	// 5. 启动同步服务的定时任务
	if opts.EnableSync && sync.IsEnabled() {
		ctx := context.Background()
		if err := sync.StartSyncService(ctx); err != nil {
			log.Printf("[Init] 同步服务启动失败: %v", err)
			if !opts.FallbackOnError {
				return components, err
			}
		} else {
			log.Printf("[Init] 同步服务已启动")
			
			// 注册配置更新回调
			config.RegisterUpdateCallback(func(cfg *config.Config) {
				sync.ConfigSyncCallback()
			})
		}
	}

	// 4. 初始化其他服务组件
	if err := initServices(components); err != nil {
		return nil, err
	}

	// 5. 创建处理器
	if err := createHandlers(components); err != nil {
		return nil, err
	}

	// 6. 设置路由
	if err := setupRoutes(components); err != nil {
		return nil, err
	}

	log.Printf("[Init] 应用程序初始化完成")
	return components, nil
}

// initServices 初始化各种服务
func initServices(components *AppComponents) error {
	log.Printf("[Init] 正在初始化应用服务...")

	// 更新常量配置
	constants.UpdateFromConfig(components.Config)

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

	// 创建健康检查处理器
	components.HealthHandler = handler.NewHealthHandler(components.ProxyHandler.GetProxyService())

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
		components.HealthHandler,
	)
	components.MainRoutes = router.SetupMainRoutes(components.MirrorHandler, components.ProxyHandler)

	log.Printf("[Init] 路由设置完成")
	return nil
}
