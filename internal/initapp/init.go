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
	CDNHandler       *handler.CDNHandler
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

		// 接了 D1 却走到本地兜底, 说明这次启动没拿到线上配置。
		// 兜底文件很可能只是仓库自带的样例, 节点会用一份"看起来正常"的错配置对外服务 ——
		// 这种状态混在一堆 info 日志里极难发现, 必须显式喊出来
		if sync.IsEnabled() {
			paths := 0
			if cfg := components.ConfigManager.GetConfig(); cfg != nil {
				paths = len(cfg.MAP)
			}
			log.Printf("[Init] !!! 警告: 已启用 D1 但本次未能取到线上配置, 当前使用本地兜底配置 (%d 个路径映射)", paths)
			log.Printf("[Init] !!! 该配置可能不是你的线上配置。已同时禁止本进程向 D1 上传配置, " +
				"避免覆盖线上。确认 D1 可达后, 到后台点「从 D1 拉取」或重启本节点即可恢复")
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
	}
	// SecurityMiddleware 始终创建: 即使 IPBan 未启用, 也要承载 Referer 黑名单等其他全局规则
	components.SecurityMiddleware = middleware.NewSecurityMiddleware(components.BanManager)
	// 用启动时的配置初始化 Referer matcher; 后续热更新由 config 回调刷新
	applyReferer := func(cfg *config.Config) {
		rb := cfg.Security.RefererBan
		if rb.Enabled {
			components.SecurityMiddleware.SetRefererMatcher(security.Compile(rb.Hosts, rb.BlockEmpty))
		} else {
			components.SecurityMiddleware.SetRefererMatcher(security.Compile(nil, false))
		}
	}
	applyReferer(components.Config)
	config.RegisterUpdateCallback(applyReferer)

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

	// 创建安全管理处理器 (始终创建; banManager 为 nil 时各端点返回空态而非 404)
	components.SecurityHandler = handler.NewSecurityHandler(components.BanManager)

	// 创建认证处理器
	components.AuthHandler = handler.NewAuthHandler(components.AuthService)

	// 创建指标处理器
	components.MetricsHandler = handler.NewMetricsHandler(components.MetricsService)

	// 创建路径统计处理器
	components.PathStatsHandler = handler.NewPathStatsHandler(metrics.GetCollector())

	// 创建 CDN 缓存清理处理器
	components.CDNHandler = handler.NewCDNHandler(components.ConfigManager)

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
		components.CDNHandler,
	)
	components.MainRoutes = router.SetupMainRoutes(components.MirrorHandler, components.ProxyHandler, components.ConfigManager)

	log.Printf("[Init] 路由设置完成")
	return nil
}
