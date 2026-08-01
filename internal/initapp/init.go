package initapp

import (
	"context"
	"fmt"
	"log"
	"os"
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
				// 把线上配置固化为本地快照: 之后 D1 不可达时的兜底
				// 是"最近一次成功同步的线上配置", 而不是仓库样例
				components.ConfigManager.SaveSnapshot()
			}
		}
	}

	// 3. 如果还没有配置管理器，使用本地配置文件
	if components.ConfigManager == nil {
		// 接了 D1 却没拿到线上配置, 且本地连快照都没有 —— 此时能加载到的只会是
		// 空白默认配置, 顶着它服务等于"看起来正常"地跑错配置。宁可响亮地失败:
		// 容器退出重启会自动重试 D1, 恢复后无需人工干预
		if sync.IsEnabled() {
			if _, statErr := os.Stat(opts.ConfigPath); os.IsNotExist(statErr) {
				return nil, fmt.Errorf(
					"已启用 D1 但无法拉取线上配置, 且本地无配置快照 (%s): 拒绝以空白配置启动, "+
						"请确认 D1_SYNC_URL 可达后重启", opts.ConfigPath)
			}
		}

		log.Printf("[Init] 正在从本地文件初始化配置管理器...")
		components.ConfigManager, err = config.NewConfigManager(opts.ConfigPath)
		if err != nil {
			log.Printf("[Init] 配置管理器初始化失败: %v", err)
			return nil, err
		}

		// 接了 D1 却走到本地兜底: 快照是"最近一次成功同步的线上配置", 可以先顶着服务,
		// 但它可能已过期。禁止上传防止覆盖线上, 心跳成功后自动热应用最新配置并解除只读
		if sync.IsEnabled() {
			paths := 0
			if cfg := components.ConfigManager.GetConfig(); cfg != nil {
				paths = len(cfg.MAP)
			}
			log.Printf("[Init] !!! 警告: 已启用 D1 但本次未能取到线上配置, 当前使用本地快照兜底 (%d 个路径映射)", paths)
			log.Printf("[Init] !!! 快照可能落后于线上。已禁止本进程向 D1 上传配置以免覆盖线上; " +
				"D1 恢复可达后, 下一次心跳 (~5 分钟) 会自动拉齐配置并解除只读, 无需人工干预")
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

			// 注册"回写 D1"回调: 走 RegisterSyncCallback 而不是 RegisterUpdateCallback,
			// 心跳热应用远端配置时不会触发它, 否则 A 改 → B 收 → B 又传, 形成回声循环
			config.RegisterSyncCallback(func(cfg *config.Config) {
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
