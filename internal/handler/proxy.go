package handler

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/security"
	"proxy-go/internal/service"
	"proxy-go/internal/utils"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/woodchen-ink/go-web-utils/iputil"
	"golang.org/x/net/http2"
)

const (
	// 超时时间常量
	clientConnTimeout   = 10 * time.Second
	proxyRespTimeout    = 60 * time.Second
	backendServTimeout  = 30 * time.Second
	idleConnTimeout     = 90 * time.Second
	tlsHandshakeTimeout = 5 * time.Second
)

// 优化后的连接池配置常量
const (
	// 连接池配置
	maxIdleConns        = 5000 // 全局最大空闲连接数（增加）
	maxIdleConnsPerHost = 500  // 每个主机最大空闲连接数（增加）
	maxConnsPerHost     = 1000 // 每个主机最大连接数（增加）

	// 缓冲区大小优化
	writeBufferSize = 256 * 1024 // 写缓冲区（增加）
	readBufferSize  = 256 * 1024 // 读缓冲区（增加）

	// HTTP/2 配置
	maxReadFrameSize = 64 * 1024 // HTTP/2 最大读帧大小（增加）
)

// ErrorHandler 定义错误处理函数类型
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

type ProxyHandler struct {
	// Service层依赖
	proxyService       *service.ProxyService
	pathMatcherService *service.PathMatcherService

	// 保留的字段（为了向后兼容）
	startTime    time.Time
	config       *config.Config
	errorHandler ErrorHandler
	Cache        *cache.CacheManager

	// pathRefererMatchers 按"路径前缀"持有路径级 Referer 黑名单 matcher;
	// 配置热更新时整体替换 (build 出新的 map 再 Store), 读侧无锁。
	pathRefererMatchers atomic.Pointer[map[string]*security.RefererMatcher]
}

// buildPathRefererMatchers 由 PathConfig 构建路径级 matcher; 只为配置了 RefererBan 的路径建立条目
func buildPathRefererMatchers(pathMap map[string]config.PathConfig) map[string]*security.RefererMatcher {
	out := make(map[string]*security.RefererMatcher)
	for prefix, cfg := range pathMap {
		rb := cfg.RefererBan
		if rb == nil || !rb.Enabled {
			continue
		}
		m := security.Compile(rb.Hosts, rb.BlockEmpty)
		if m.HasRules() {
			out[prefix] = m
		}
	}
	return out
}

// GetProxyService 获取ProxyService实例
func (h *ProxyHandler) GetProxyService() *service.ProxyService {
	return h.proxyService
}

// 前缀匹配器结构体
type prefixMatcher struct {
	prefixes []string
	configs  map[string]config.PathConfig
}

// 创建新的前缀匹配器
func newPrefixMatcher(pathMap map[string]config.PathConfig) *prefixMatcher {
	pm := &prefixMatcher{
		prefixes: make([]string, 0, len(pathMap)),
		configs:  make(map[string]config.PathConfig, len(pathMap)),
	}

	// 按长度降序排列前缀，确保最长匹配优先
	for prefix, cfg := range pathMap {
		pm.prefixes = append(pm.prefixes, prefix)
		pm.configs[prefix] = cfg
	}

	// 按长度降序排列
	sort.Slice(pm.prefixes, func(i, j int) bool {
		return len(pm.prefixes[i]) > len(pm.prefixes[j])
	})

	return pm
}

// 根据路径查找匹配的前缀和配置
func (pm *prefixMatcher) match(path string) (string, config.PathConfig, bool) {
	// 按预排序的前缀列表查找最长匹配
	for _, prefix := range pm.prefixes {
		if strings.HasPrefix(path, prefix) {
			// 确保匹配的是完整的路径段
			restPath := path[len(prefix):]
			if restPath == "" || restPath[0] == '/' {
				return prefix, pm.configs[prefix], true
			}
		}
	}
	return "", config.PathConfig{}, false
}

// 更新前缀匹配器
func (pm *prefixMatcher) update(pathMap map[string]config.PathConfig) {
	pm.prefixes = make([]string, 0, len(pathMap))
	pm.configs = make(map[string]config.PathConfig, len(pathMap))

	for prefix, cfg := range pathMap {
		pm.prefixes = append(pm.prefixes, prefix)
		pm.configs[prefix] = cfg
	}

	// 按长度降序排列
	sort.Slice(pm.prefixes, func(i, j int) bool {
		return len(pm.prefixes[i]) > len(pm.prefixes[j])
	})
}

// NewProxyHandler 创建新的代理处理器
func NewProxyHandler(cfg *config.Config) *ProxyHandler {
	dialer := &net.Dialer{
		Timeout:   clientConnTimeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext:            dialer.DialContext,
		MaxIdleConns:           maxIdleConns,
		MaxIdleConnsPerHost:    maxIdleConnsPerHost,
		IdleConnTimeout:        idleConnTimeout,
		TLSHandshakeTimeout:    tlsHandshakeTimeout,
		ExpectContinueTimeout:  1 * time.Second,
		MaxConnsPerHost:        maxConnsPerHost,
		DisableKeepAlives:      false,
		DisableCompression:     false,
		ForceAttemptHTTP2:      true,
		WriteBufferSize:        writeBufferSize,
		ReadBufferSize:         readBufferSize,
		ResponseHeaderTimeout:  backendServTimeout,
		MaxResponseHeaderBytes: 128 * 1024, // 增加响应头缓冲区
	}

	// 设置HTTP/2传输配置
	http2Transport, err := http2.ConfigureTransports(transport)
	if err == nil && http2Transport != nil {
		http2Transport.ReadIdleTimeout = 30 * time.Second // 增加读空闲超时
		http2Transport.PingTimeout = 10 * time.Second     // 增加ping超时
		http2Transport.AllowHTTP = false
		http2Transport.MaxReadFrameSize = maxReadFrameSize // 使用常量
		http2Transport.StrictMaxConcurrentStreams = true

	}

	// 初始化缓存管理器 - 从主配置获取缓存配置
	mainConfig := config.GetConfig()
	var cacheConfig *config.CacheConfig
	if mainConfig != nil {
		cacheConfig = &mainConfig.Cache
	}
	cacheManager, err := cache.NewCacheManager("data/cache", cacheConfig)
	if err != nil {
		log.Printf("[Cache] Failed to initialize cache manager: %v", err)
	}

	// 初始化规则服务
	ruleService := service.NewRuleService(cacheManager)

	// 记录开始时间
	startTime := time.Now()

	// 创建HTTP客户端
	client := &http.Client{
		Transport: transport,
		Timeout:   proxyRespTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}

	// 初始化重定向处理器（暂时保留）
	_ = NewRedirectHandler(ruleService)

	// 初始化Service层
	pathMatcherService := service.NewPathMatcherService(cfg.MAP)
	proxyService := service.NewProxyService(client, cacheManager, ruleService)

	handler := &ProxyHandler{
		// Service层依赖
		proxyService:       proxyService,
		pathMatcherService: pathMatcherService,

		// 保留字段
		startTime: startTime,
		config:    cfg,
		Cache:     cacheManager,
		errorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[Error] %s %s -> %v from %s", r.Method, r.URL.Path, err, utils.GetRequestSource(r))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		},
	}

	// 初始化路径级 Referer 黑名单
	initMatchers := buildPathRefererMatchers(cfg.MAP)
	handler.pathRefererMatchers.Store(&initMatchers)

	// 注册配置更新回调
	config.RegisterUpdateCallback(func(newCfg *config.Config) {
		// 注意：config包已经在回调触发前处理了所有ExtRules，这里无需再次处理
		handler.pathMatcherService.UpdatePaths(newCfg.MAP)
		handler.config = newCfg

		// 重建路径级 Referer 黑名单 matcher 整张表
		newMatchers := buildPathRefererMatchers(newCfg.MAP)
		handler.pathRefererMatchers.Store(&newMatchers)

		// 清理ExtensionMatcher缓存，确保使用新配置
		if handler.Cache != nil {
			handler.Cache.InvalidateAllExtensionMatchers()
			log.Printf("[Config] ExtensionMatcher缓存已清理")
		}

		// 清理URL可访问性缓存和文件大小缓存
		utils.ClearAccessibilityCache()
		utils.ClearFileSizeCache()

		log.Printf("[Config] 代理处理器配置已更新: %d 个路径映射", len(newCfg.MAP))
	})

	return handler
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 添加 panic 恢复
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[Panic] %s %s -> %v from %s", r.Method, r.URL.Path, err, utils.GetRequestSource(r))
			h.errorHandler(w, r, fmt.Errorf("panic: %v", err))
		}
	}()

	collector := metrics.GetCollector()
	collector.BeginRequest()
	defer collector.EndRequest()

	start := time.Now()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(r.Context(), proxyRespTimeout)
	defer cancel()
	r = r.WithContext(ctx)

	// 处理根路径请求
	if r.URL.Path == "/" {
		h.handleWelcome(w, r, start)
		return
	}

	// 使用路径匹配服务查找匹配的路径
	matchResult := h.pathMatcherService.MatchPath(r.URL.Path)
	if !matchResult.Matched {
		http.NotFound(w, r)
		return
	}

	// 路径级 Referer 黑名单 (与全局规则叠加, 任一命中即拒); 在缓存检查之前, 防止已缓存内容被盗链方拿到
	if matchers := h.pathRefererMatchers.Load(); matchers != nil {
		if m, ok := (*matchers)[matchResult.MatchedPrefix]; ok && m.IsBlocked(r.Header.Get("Referer")) {
			http.Error(w, "Forbidden: referer not allowed", http.StatusForbidden)
			collector.RecordRequest(r.URL.Path, matchResult.MatchedPrefix, http.StatusForbidden, time.Since(start), 0, iputil.GetClientIP(r), r)
			return
		}
	}

	// 创建代理请求结构
	proxyReq := &service.ProxyRequest{
		OriginalRequest: r,
		MatchedPrefix:   matchResult.MatchedPrefix,
		PathConfig:      matchResult.PathConfig,
		TargetPath:      matchResult.TargetPath,
		StartTime:       start,
	}

	// 检查缓存
	if item, hit, notModified := h.proxyService.CheckCache(proxyReq); hit {
		h.handleCacheHit(w, r, item, notModified, start, collector, matchResult.MatchedPrefix, matchResult.PathConfig)
		return
	}

	// 检查重定向
	if h.proxyService.CheckRedirect(proxyReq, w) {
		collector.RecordRequest(r.URL.Path, matchResult.MatchedPrefix, http.StatusFound, time.Since(start), 0, iputil.GetClientIP(r), r)
		return
	}

	// 选择目标服务器
	targetURL, altTarget := h.proxyService.SelectTarget(proxyReq)

	// 创建代理请求
	httpReq, err := h.proxyService.CreateProxyRequest(proxyReq, targetURL)
	if err != nil {
		h.errorHandler(w, r, err)
		return
	}

	// 执行请求
	resp, err := h.proxyService.ExecuteRequest(httpReq)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error executing request: %v", err))
		return
	}

	// 处理响应
	defer resp.Body.Close()
	written, err := h.proxyService.ProcessResponse(proxyReq, resp, w, altTarget)
	if err != nil {
		h.errorHandler(w, r, err)
		return
	}

	// 记录统计信息（缓存未命中）
	collector.RecordRequestWithCache(r.URL.Path, matchResult.MatchedPrefix, resp.StatusCode, time.Since(start), written, iputil.GetClientIP(r), r, false, 0)
}

// handleWelcome 处理根路径欢迎消息
func (h *ProxyHandler) handleWelcome(w http.ResponseWriter, r *http.Request, start time.Time) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Welcome to CZL proxy.")
	log.Printf("[Proxy] %s %s -> %d (%s) from %s", r.Method, r.URL.Path, http.StatusOK, iputil.GetClientIP(r), utils.GetRequestSource(r))
}

// handleCacheHit 处理缓存命中
func (h *ProxyHandler) handleCacheHit(w http.ResponseWriter, r *http.Request, item *cache.CacheItem, notModified bool, start time.Time, collector *metrics.Collector, matchedPrefix string, pathCfg config.PathConfig) {
	// 🔧 修复缓存文件被删除后404的问题：在提供文件前再次验证文件是否存在
	if _, err := os.Stat(item.FilePath); err != nil {
		// 缓存文件不存在，清理缓存记录并重新处理请求
		if h.Cache != nil {
			// 清理内存中的缓存记录, 使用与命中时一致的 cfImageOpt 策略
			cacheKey := h.Cache.GenerateCacheKey(r, pathCfg.CFImageOpt)
			h.Cache.InvalidateCacheItem(cacheKey)
			log.Printf("[Cache] File missing, invalidated cache for %s", r.URL.Path)
		}
		// 重新执行正常的代理流程
		h.handleMissedCache(w, r, start, collector)
		return
	}

	w.Header().Set("Content-Type", item.ContentType)
	if item.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", item.ContentEncoding)
	}
	w.Header().Set("CZL-Proxy-Cache-HIT", "1")
	w.Header().Set("CZL-Proxy-AltTarget", "0") // 缓存命中时设为0

	if notModified {
		w.WriteHeader(http.StatusNotModified)
		// 记录缓存命中（304响应也算命中，节省了带宽）
		collector.RecordRequestWithCache(r.URL.Path, matchedPrefix, http.StatusNotModified, time.Since(start), 0, iputil.GetClientIP(r), r, true, item.Size)
		return
	}
	http.ServeFile(w, r, item.FilePath)
	// 记录缓存命中，节省的字节数等于文件大小
	collector.RecordRequestWithCache(r.URL.Path, matchedPrefix, http.StatusOK, time.Since(start), item.Size, iputil.GetClientIP(r), r, true, item.Size)
}

// handleMissedCache 处理缓存未命中或缓存失效的情况，重新执行代理请求
func (h *ProxyHandler) handleMissedCache(w http.ResponseWriter, r *http.Request, start time.Time, collector *metrics.Collector) {
	// 使用路径匹配服务查找匹配的路径
	matchResult := h.pathMatcherService.MatchPath(r.URL.Path)
	if !matchResult.Matched {
		http.NotFound(w, r)
		return
	}

	// 创建代理请求结构
	proxyReq := &service.ProxyRequest{
		OriginalRequest: r,
		MatchedPrefix:   matchResult.MatchedPrefix,
		PathConfig:      matchResult.PathConfig,
		TargetPath:      matchResult.TargetPath,
		StartTime:       start,
	}

	// 检查重定向
	if h.proxyService.CheckRedirect(proxyReq, w) {
		collector.RecordRequest(r.URL.Path, matchResult.MatchedPrefix, http.StatusFound, time.Since(start), 0, iputil.GetClientIP(r), r)
		return
	}

	// 选择目标服务器
	targetURL, altTarget := h.proxyService.SelectTarget(proxyReq)

	// 创建代理请求
	httpReq, err := h.proxyService.CreateProxyRequest(proxyReq, targetURL)
	if err != nil {
		h.errorHandler(w, r, err)
		return
	}

	// 执行请求
	resp, err := h.proxyService.ExecuteRequest(httpReq)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error executing request: %v", err))
		return
	}

	// 处理响应
	defer resp.Body.Close()
	written, err := h.proxyService.ProcessResponse(proxyReq, resp, w, altTarget)
	if err != nil {
		h.errorHandler(w, r, err)
		return
	}

	// 记录统计信息（缓存未命中）
	collector.RecordRequestWithCache(r.URL.Path, matchResult.MatchedPrefix, resp.StatusCode, time.Since(start), written, iputil.GetClientIP(r), r, false, 0)
}
