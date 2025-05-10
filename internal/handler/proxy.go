package handler

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
	"sort"
	"strings"
	"time"

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

// 添加 hop-by-hop 头部映射
var hopHeadersBase = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// ErrorHandler 定义错误处理函数类型
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

type ProxyHandler struct {
	pathMap      map[string]config.PathConfig
	prefixTree   *prefixMatcher // 添加前缀匹配树
	client       *http.Client
	startTime    time.Time
	config       *config.Config
	auth         *authManager
	errorHandler ErrorHandler
	Cache        *cache.CacheManager
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
		MaxIdleConns:           2000,
		MaxIdleConnsPerHost:    200,
		IdleConnTimeout:        idleConnTimeout,
		TLSHandshakeTimeout:    tlsHandshakeTimeout,
		ExpectContinueTimeout:  1 * time.Second,
		MaxConnsPerHost:        400,
		DisableKeepAlives:      false,
		DisableCompression:     false,
		ForceAttemptHTTP2:      true,
		WriteBufferSize:        128 * 1024,
		ReadBufferSize:         128 * 1024,
		ResponseHeaderTimeout:  backendServTimeout,
		MaxResponseHeaderBytes: 64 * 1024,
	}

	// 设置HTTP/2传输配置
	http2Transport, err := http2.ConfigureTransports(transport)
	if err == nil && http2Transport != nil {
		http2Transport.ReadIdleTimeout = 10 * time.Second
		http2Transport.PingTimeout = 5 * time.Second
		http2Transport.AllowHTTP = false
		http2Transport.MaxReadFrameSize = 32 * 1024
		http2Transport.StrictMaxConcurrentStreams = true
	}

	// 初始化缓存管理器
	cacheManager, err := cache.NewCacheManager("data/cache")
	if err != nil {
		log.Printf("[Cache] Failed to initialize cache manager: %v", err)
	}

	handler := &ProxyHandler{
		pathMap:    cfg.MAP,
		prefixTree: newPrefixMatcher(cfg.MAP), // 初始化前缀匹配树
		client: &http.Client{
			Transport: transport,
			Timeout:   proxyRespTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		startTime: time.Now(),
		config:    cfg,
		auth:      newAuthManager(),
		Cache:     cacheManager,
		errorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[Error] %s %s -> %v from %s", r.Method, r.URL.Path, err, utils.GetRequestSource(r))
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal Server Error"))
		},
	}

	// 注册配置更新回调
	config.RegisterUpdateCallback(func(newCfg *config.Config) {
		// 注意：config包已经在回调触发前处理了所有ExtRules，这里无需再次处理
		handler.pathMap = newCfg.MAP
		handler.prefixTree.update(newCfg.MAP) // 更新前缀匹配树
		handler.config = newCfg
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
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Welcome to CZL proxy.")
		log.Printf("[Proxy] %s %s -> %d (%s) from %s", r.Method, r.URL.Path, http.StatusOK, utils.GetClientIP(r), utils.GetRequestSource(r))
		return
	}

	// 使用前缀匹配树快速查找匹配的路径
	matchedPrefix, pathConfig, matched := h.prefixTree.match(r.URL.Path)

	// 如果没有找到匹配，返回404
	if !matched {
		http.NotFound(w, r)
		return
	}

	// 构建目标 URL
	targetPath := strings.TrimPrefix(r.URL.Path, matchedPrefix)

	// URL 解码，然后重新编码，确保特殊字符被正确处理
	decodedPath, err := url.QueryUnescape(targetPath)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error decoding path: %v", err))
		return
	}

	// 使用统一的路由选择逻辑
	targetBase, usedAltTarget := utils.GetTargetURL(h.client, r, pathConfig, decodedPath)

	// 重新编码路径，保留 '/'
	parts := strings.Split(decodedPath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(parts, "/")
	targetURL := targetBase + encodedPath

	// 添加原始请求的查询参数
	if r.URL.RawQuery != "" {
		targetURL = targetURL + "?" + r.URL.RawQuery
	}

	// 解析目标 URL 以获取 host
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error parsing URL: %v", err))
		return
	}

	// 创建新的请求时使用带超时的上下文
	proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, r.Body)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error creating request: %v", err))
		return
	}

	// 复制并处理请求头 - 使用更高效的方式
	copyHeader(proxyReq.Header, r.Header)

	// 添加常见浏览器User-Agent - 避免冗余字符串操作
	if r.Header.Get("User-Agent") == "" {
		proxyReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	}

	// 使用预先构建的URL字符串
	hostScheme := parsedURL.Scheme + "://" + parsedURL.Host

	// 添加Origin
	proxyReq.Header.Set("Origin", hostScheme)

	// 设置Referer为源站的完整域名（带上斜杠）
	proxyReq.Header.Set("Referer", hostScheme+"/")

	// 设置Host头和proxyReq.Host
	proxyReq.Header.Set("Host", parsedURL.Host)
	proxyReq.Host = parsedURL.Host

	// 确保设置适当的Accept头 - 避免冗余字符串操作
	if r.Header.Get("Accept") == "" {
		proxyReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	}

	// 确保设置Accept-Encoding - 避免冗余字符串操作
	if ae := r.Header.Get("Accept-Encoding"); ae != "" {
		proxyReq.Header.Set("Accept-Encoding", ae)
	} else {
		proxyReq.Header.Del("Accept-Encoding")
	}

	// 特别处理图片请求
	if utils.IsImageRequest(r.URL.Path) {
		accept := r.Header.Get("Accept")

		// 使用switch语句优化条件分支
		switch {
		case strings.Contains(accept, "image/avif"):
			proxyReq.Header.Set("Accept", "image/avif")
		case strings.Contains(accept, "image/webp"):
			proxyReq.Header.Set("Accept", "image/webp")
		}

		// 设置 Cloudflare 特定的头部
		proxyReq.Header.Set("CF-Image-Format", "auto")
	}

	// 设置最小必要的代理头部
	clientIP := utils.GetClientIP(r)
	proxyReq.Header.Set("X-Real-IP", clientIP)

	// 添加或更新 X-Forwarded-For - 减少重复获取客户端IP
	if clientIP != "" {
		if prior := proxyReq.Header.Get("X-Forwarded-For"); prior != "" {
			proxyReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			proxyReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}

	// 处理 Cookie 安全属性
	if r.TLS != nil && len(proxyReq.Cookies()) > 0 {
		cookies := proxyReq.Cookies()
		for _, cookie := range cookies {
			if !cookie.Secure {
				cookie.Secure = true
			}
			if !cookie.HttpOnly {
				cookie.HttpOnly = true
			}
		}
	}

	// 检查是否可以使用缓存
	if r.Method == http.MethodGet && h.Cache != nil {
		cacheKey := h.Cache.GenerateCacheKey(r)
		if item, hit, notModified := h.Cache.Get(cacheKey, r); hit {
			// 从缓存提供响应
			w.Header().Set("Content-Type", item.ContentType)
			if item.ContentEncoding != "" {
				w.Header().Set("Content-Encoding", item.ContentEncoding)
			}
			w.Header().Set("Proxy-Go-Cache-HIT", "1")

			// 如果使用了扩展名映射的备用目标，添加标记响应头
			if usedAltTarget {
				w.Header().Set("Proxy-Go-AltTarget", "1")
			}
			w.Header().Set("Proxy-Go-AltTarget", "0")

			if notModified {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			http.ServeFile(w, r, item.FilePath)
			collector.RecordRequest(r.URL.Path, http.StatusOK, time.Since(start), item.Size, utils.GetClientIP(r), r)
			return
		}
	}

	// 发送代理请求
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			h.errorHandler(w, r, fmt.Errorf("request timeout after %v", proxyRespTimeout))
			log.Printf("[Proxy] ERR %s %s -> 408 (%s) timeout from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		} else {
			h.errorHandler(w, r, fmt.Errorf("proxy error: %v", err))
			log.Printf("[Proxy] ERR %s %s -> 502 (%s) proxy error from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
		}
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Proxy-Go-Cache-HIT", "0")

	// 如果使用了扩展名映射的备用目标，添加标记响应头
	if usedAltTarget {
		w.Header().Set("Proxy-Go-AltTarget", "1")
	}
	w.Header().Set("Proxy-Go-AltTarget", "0")

	// 设置响应状态码
	w.WriteHeader(resp.StatusCode)

	var written int64
	// 如果是GET请求且响应成功，使用TeeReader同时写入缓存
	if r.Method == http.MethodGet && resp.StatusCode == http.StatusOK && h.Cache != nil {
		cacheKey := h.Cache.GenerateCacheKey(r)
		if cacheFile, err := h.Cache.CreateTemp(cacheKey, resp); err == nil {
			defer cacheFile.Close()

			// 使用缓冲IO提高性能
			bufSize := 32 * 1024 // 32KB 缓冲区
			buf := make([]byte, bufSize)

			teeReader := io.TeeReader(resp.Body, cacheFile)
			written, err = io.CopyBuffer(w, teeReader, buf)

			if err == nil {
				// 异步提交缓存，不阻塞当前请求处理
				fileName := cacheFile.Name()
				respClone := *resp // 创建响应的浅拷贝
				go func() {
					h.Cache.Commit(cacheKey, fileName, &respClone, written)
				}()
			}
		} else {
			// 使用缓冲的复制提高性能
			bufSize := 32 * 1024 // 32KB 缓冲区
			buf := make([]byte, bufSize)

			written, err = io.CopyBuffer(w, resp.Body, buf)
			if err != nil && !isConnectionClosed(err) {
				log.Printf("[Proxy] ERR %s %s -> write error (%s) from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
				return
			}
		}
	} else {
		// 使用缓冲的复制提高性能
		bufSize := 32 * 1024 // 32KB 缓冲区
		buf := make([]byte, bufSize)

		written, err = io.CopyBuffer(w, resp.Body, buf)
		if err != nil && !isConnectionClosed(err) {
			log.Printf("[Proxy] ERR %s %s -> write error (%s) from %s", r.Method, r.URL.Path, utils.GetClientIP(r), utils.GetRequestSource(r))
			return
		}
	}

	// 记录统计信息
	collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(start), written, utils.GetClientIP(r), r)
}

func copyHeader(dst, src http.Header) {
	// 创建一个新的局部 map，复制基础 hop headers
	hopHeaders := make(map[string]bool, len(hopHeadersBase))
	for k, v := range hopHeadersBase {
		hopHeaders[k] = v
	}

	// 处理 Connection 头部指定的其他 hop-by-hop 头部
	if connection := src.Get("Connection"); connection != "" {
		for _, h := range strings.Split(connection, ",") {
			hopHeaders[strings.TrimSpace(h)] = true
		}
	}

	// 使用局部 map 快速查找，跳过 hop-by-hop 头部
	for k, vv := range src {
		if !hopHeaders[k] {
			for _, v := range vv {
				dst.Add(k, v)
			}
		}
	}
}

// 添加辅助函数判断是否是连接关闭错误
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "broken pipe") ||
		strings.Contains(err.Error(), "connection reset by peer") ||
		strings.Contains(err.Error(), "protocol wrong type for socket")
}
