package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/time/rate"
)

const (
	// 缓冲区大小
	defaultBufferSize = 32 * 1024 // 32KB

	// 超时时间常量
	clientConnTimeout   = 10 * time.Second
	proxyRespTimeout    = 60 * time.Second
	backendServTimeout  = 40 * time.Second
	idleConnTimeout     = 120 * time.Second
	tlsHandshakeTimeout = 10 * time.Second

	// 限流相关常量
	globalRateLimit   = 2000
	globalBurstLimit  = 500
	perHostRateLimit  = 200
	perHostBurstLimit = 100
	perIPRateLimit    = 50
	perIPBurstLimit   = 20
	cleanupInterval   = 10 * time.Minute
)

// 统一的缓冲池
var bufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, defaultBufferSize))
	},
}

// getBuffer 获取缓冲区
func getBuffer() (*bytes.Buffer, func()) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf, func() {
		if buf != nil {
			bufferPool.Put(buf)
		}
	}
}

// 添加 hop-by-hop 头部映射
var hopHeadersMap = make(map[string]bool)

func init() {
	headers := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	}
	for _, h := range headers {
		hopHeadersMap[h] = true
	}
}

// ErrorHandler 定义错误处理函数类型
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// RateLimiter 定义限流器接口
type RateLimiter interface {
	Allow() bool
	Clean(now time.Time)
}

// 限流管理器
type rateLimitManager struct {
	globalLimiter *rate.Limiter
	hostLimiters  *sync.Map // host -> *rate.Limiter
	ipLimiters    *sync.Map // IP -> *rate.Limiter
	lastCleanup   time.Time
}

// 创建新的限流管理器
func newRateLimitManager() *rateLimitManager {
	manager := &rateLimitManager{
		globalLimiter: rate.NewLimiter(rate.Limit(globalRateLimit), globalBurstLimit),
		hostLimiters:  &sync.Map{},
		ipLimiters:    &sync.Map{},
		lastCleanup:   time.Now(),
	}

	// 启动清理协程
	go manager.cleanupLoop()
	return manager
}

func (m *rateLimitManager) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	for range ticker.C {
		now := time.Now()
		m.cleanup(now)
	}
}

func (m *rateLimitManager) cleanup(now time.Time) {
	m.hostLimiters.Range(func(key, value interface{}) bool {
		if now.Sub(m.lastCleanup) > cleanupInterval {
			m.hostLimiters.Delete(key)
		}
		return true
	})

	m.ipLimiters.Range(func(key, value interface{}) bool {
		if now.Sub(m.lastCleanup) > cleanupInterval {
			m.ipLimiters.Delete(key)
		}
		return true
	})

	m.lastCleanup = now
}

func (m *rateLimitManager) getHostLimiter(host string) *rate.Limiter {
	if limiter, exists := m.hostLimiters.Load(host); exists {
		return limiter.(*rate.Limiter)
	}

	limiter := rate.NewLimiter(rate.Limit(perHostRateLimit), perHostBurstLimit)
	m.hostLimiters.Store(host, limiter)
	return limiter
}

func (m *rateLimitManager) getIPLimiter(ip string) *rate.Limiter {
	if limiter, exists := m.ipLimiters.Load(ip); exists {
		return limiter.(*rate.Limiter)
	}

	limiter := rate.NewLimiter(rate.Limit(perIPRateLimit), perIPBurstLimit)
	m.ipLimiters.Store(ip, limiter)
	return limiter
}

// 检查是否允许请求
func (m *rateLimitManager) allowRequest(r *http.Request) error {
	// 全局限流检查
	if !m.globalLimiter.Allow() {
		return fmt.Errorf("global rate limit exceeded")
	}

	// Host限流检查
	host := r.Host
	if host != "" {
		if !m.getHostLimiter(host).Allow() {
			return fmt.Errorf("host rate limit exceeded for %s", host)
		}
	}

	// IP限流检查
	ip := utils.GetClientIP(r)
	if ip != "" {
		if !m.getIPLimiter(ip).Allow() {
			return fmt.Errorf("ip rate limit exceeded for %s", ip)
		}
	}

	return nil
}

type ProxyHandler struct {
	pathMap      map[string]config.PathConfig
	client       *http.Client
	limiter      *rate.Limiter
	startTime    time.Time
	config       *config.Config
	auth         *authManager
	errorHandler ErrorHandler // 添加错误处理器
	rateLimiter  *rateLimitManager
}

// NewProxyHandler 创建新的代理处理器
func NewProxyHandler(cfg *config.Config) *ProxyHandler {
	dialer := &net.Dialer{
		Timeout:   clientConnTimeout,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		DialContext:            dialer.DialContext,
		MaxIdleConns:           1000, // 增加最大空闲连接数
		MaxIdleConnsPerHost:    100,  // 增加每个主机的最大空闲连接数
		IdleConnTimeout:        idleConnTimeout,
		TLSHandshakeTimeout:    tlsHandshakeTimeout,
		ExpectContinueTimeout:  1 * time.Second,
		MaxConnsPerHost:        200, // 增加每个主机的最大连接数
		DisableKeepAlives:      false,
		DisableCompression:     false,
		ForceAttemptHTTP2:      true,
		WriteBufferSize:        64 * 1024,
		ReadBufferSize:         64 * 1024,
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

	handler := &ProxyHandler{
		pathMap: cfg.MAP,
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
		limiter:     rate.NewLimiter(rate.Limit(5000), 10000),
		startTime:   time.Now(),
		config:      cfg,
		auth:        newAuthManager(),
		rateLimiter: newRateLimitManager(),
		errorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[Error] %s %s -> %v", r.Method, r.URL.Path, err)
			if strings.Contains(err.Error(), "rate limit exceeded") {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte("Too Many Requests"))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal Server Error"))
			}
		},
	}

	// 注册配置更新回调
	config.RegisterUpdateCallback(func(newCfg *config.Config) {
		handler.pathMap = newCfg.MAP
		handler.config = newCfg
		log.Printf("[Config] 配置已更新并生效")
	})

	return handler
}

// SetErrorHandler 允许自定义错误处理函数
func (h *ProxyHandler) SetErrorHandler(handler ErrorHandler) {
	if handler != nil {
		h.errorHandler = handler
	}
}

// copyResponse 使用缓冲方式传输数据
func copyResponse(dst io.Writer, src io.Reader, flusher http.Flusher) (int64, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	defer bufferPool.Put(buf)
	buf.Reset()

	var written int64
	for {
		// 清空缓冲区
		buf.Reset()

		// 读取数据到缓冲区
		_, er := io.CopyN(buf, src, defaultBufferSize)
		if er != nil && er != io.EOF {
			return written, er
		}

		// 如果有数据，写入目标
		if buf.Len() > 0 {
			nw, ew := dst.Write(buf.Bytes())
			if ew != nil {
				return written, ew
			}
			written += int64(nw)

			// 定期刷新缓冲区
			if flusher != nil && written%(1024*1024) == 0 { // 每1MB刷新一次
				flusher.Flush()
			}
		}

		if er == io.EOF {
			break
		}
	}

	// 最后一次刷新
	if flusher != nil {
		flusher.Flush()
	}
	return written, nil
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 添加 panic 恢复
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[Panic] %s %s -> %v", r.Method, r.URL.Path, err)
			h.errorHandler(w, r, fmt.Errorf("panic: %v", err))
		}
	}()

	collector := metrics.GetCollector()
	collector.BeginRequest()
	defer collector.EndRequest()

	// 限流检查
	if err := h.rateLimiter.allowRequest(r); err != nil {
		h.errorHandler(w, r, err)
		return
	}

	start := time.Now()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(r.Context(), proxyRespTimeout)
	defer cancel()
	r = r.WithContext(ctx)

	// 处理根路径请求
	if r.URL.Path == "/" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Welcome to CZL proxy.")
		log.Printf("[%s] %s %s -> %d (root path) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, http.StatusOK, time.Since(start))
		return
	}

	// 查找匹配的代理路径
	var matchedPrefix string
	var pathConfig config.PathConfig
	for prefix, cfg := range h.pathMap {
		if strings.HasPrefix(r.URL.Path, prefix) {
			matchedPrefix = prefix
			pathConfig = cfg
			break
		}
	}

	// 如果没有匹配的路径，返回 404
	if matchedPrefix == "" {
		http.NotFound(w, r)
		log.Printf("[%s] %s %s -> 404 (not found) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, time.Since(start))
		return
	}

	// 构建目标 URL
	targetPath := strings.TrimPrefix(r.URL.Path, matchedPrefix)

	// URL 解码，然后重新编码，确保特殊字符被正确处理
	decodedPath, err := url.QueryUnescape(targetPath)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error decoding path: %v", err))
		log.Printf("[%s] %s %s -> 500 (error decoding path: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(start))
		return
	}

	// 使用统一的路由选择逻辑
	targetBase := utils.GetTargetURL(h.client, r, pathConfig, decodedPath)

	// 重新编码路径，保留 '/'
	parts := strings.Split(decodedPath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(parts, "/")
	targetURL := targetBase + encodedPath

	// 解析目标 URL 以获取 host
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error parsing URL: %v", err))
		log.Printf("[%s] %s %s -> 500 (error parsing URL: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(start))
		return
	}

	// 创建新的请求时使用带超时的上下文
	proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, r.Body)
	if err != nil {
		h.errorHandler(w, r, fmt.Errorf("error creating request: %v", err))
		return
	}

	// 复制并处理请求头
	copyHeader(proxyReq.Header, r.Header)

	// 特别处理图片请求
	if utils.IsImageRequest(r.URL.Path) {
		// 获取 Accept 头
		accept := r.Header.Get("Accept")

		// 根据 Accept 头设置合适的图片格式
		if strings.Contains(accept, "image/avif") {
			proxyReq.Header.Set("Accept", "image/avif")
		} else if strings.Contains(accept, "image/webp") {
			proxyReq.Header.Set("Accept", "image/webp")
		}

		// 设置 Cloudflare 特定的头部
		proxyReq.Header.Set("CF-Image-Format", "auto") // 让 Cloudflare 根据 Accept 头自动选择格式
	}

	// 设置其他必要的头部
	proxyReq.Host = parsedURL.Host
	proxyReq.Header.Set("Host", parsedURL.Host)
	proxyReq.Header.Set("X-Real-IP", utils.GetClientIP(r))
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", r.URL.Scheme)

	// 添加或更新 X-Forwarded-For
	if clientIP := utils.GetClientIP(r); clientIP != "" {
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

	// 发送代理请求
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			h.errorHandler(w, r, fmt.Errorf("request timeout after %v", proxyRespTimeout))
			log.Printf("[Timeout] %s %s -> timeout after %v",
				r.Method, r.URL.Path, proxyRespTimeout)
		} else {
			h.errorHandler(w, r, fmt.Errorf("proxy error: %v", err))
			log.Printf("[%s] %s %s -> 502 (proxy error: %v) [%v]",
				utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(start))
		}
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)

	// 删除严格的 CSP
	w.Header().Del("Content-Security-Policy")

	// 根据响应大小选择不同的处理策略
	contentLength := resp.ContentLength
	if contentLength > 0 && contentLength < 1<<20 { // 1MB 以下的小响应
		// 获取合适大小的缓冲区
		buf, putBuffer := getBuffer()
		defer putBuffer()

		// 使用缓冲区读取响应
		_, err := io.Copy(buf, resp.Body)
		if err != nil {
			if !isConnectionClosed(err) {
				h.errorHandler(w, r, fmt.Errorf("error reading response: %v", err))
			}
			return
		}

		// 设置响应状态码并一次性写入响应
		w.WriteHeader(resp.StatusCode)
		written, err := w.Write(buf.Bytes())
		if err != nil {
			if !isConnectionClosed(err) {
				log.Printf("Error writing response: %v", err)
			}
		}
		collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(start), int64(written), utils.GetClientIP(r), r)
	} else {
		// 大响应使用零拷贝传输
		w.WriteHeader(resp.StatusCode)
		var bytesCopied int64
		var err error

		if f, ok := w.(http.Flusher); ok {
			bytesCopied, err = copyResponse(w, resp.Body, f)
		} else {
			bytesCopied, err = copyResponse(w, resp.Body, nil)
		}

		if err != nil && !isConnectionClosed(err) {
			log.Printf("Error copying response: %v", err)
		}

		// 记录访问日志
		collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(start), bytesCopied, utils.GetClientIP(r), r)
	}
}

func copyHeader(dst, src http.Header) {
	// 处理 Connection 头部指定的其他 hop-by-hop 头部
	if connection := src.Get("Connection"); connection != "" {
		for _, h := range strings.Split(connection, ",") {
			hopHeadersMap[strings.TrimSpace(h)] = true
		}
	}

	// 使用 map 快速查找，跳过 hop-by-hop 头部
	for k, vv := range src {
		if !hopHeadersMap[k] {
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
