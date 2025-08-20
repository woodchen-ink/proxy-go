package handler

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"proxy-go/internal/cache"
	"proxy-go/internal/metrics"
	"proxy-go/internal/service"
	"time"

	"github.com/woodchen-ink/go-web-utils/iputil"
	"golang.org/x/net/http2"
)

// 镜像代理专用配置常量
const (
	mirrorMaxIdleConns        = 2000             // 镜像代理全局最大空闲连接
	mirrorMaxIdleConnsPerHost = 200              // 镜像代理每个主机最大空闲连接
	mirrorMaxConnsPerHost     = 500              // 镜像代理每个主机最大连接数
	mirrorTimeout             = 60 * time.Second // 镜像代理超时时间
)

type MirrorProxyHandler struct {
	mirrorService *service.MirrorProxyService
	Cache        *cache.CacheManager // 保留Cache字段以兼容现有代码
}

func NewMirrorProxyHandler() *MirrorProxyHandler {
	// 创建优化的拨号器
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// 创建优化的传输层
	transport := &http.Transport{
		DialContext:            dialer.DialContext,
		MaxIdleConns:           mirrorMaxIdleConns,
		MaxIdleConnsPerHost:    mirrorMaxIdleConnsPerHost,
		MaxConnsPerHost:        mirrorMaxConnsPerHost,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    5 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		DisableKeepAlives:      false,
		DisableCompression:     false,
		ForceAttemptHTTP2:      true,
		WriteBufferSize:        128 * 1024,
		ReadBufferSize:         128 * 1024,
		ResponseHeaderTimeout:  30 * time.Second,
		MaxResponseHeaderBytes: 64 * 1024,
	}

	// 配置 HTTP/2
	http2Transport, err := http2.ConfigureTransports(transport)
	if err == nil && http2Transport != nil {
		http2Transport.ReadIdleTimeout = 30 * time.Second
		http2Transport.PingTimeout = 10 * time.Second
		http2Transport.AllowHTTP = false
		http2Transport.MaxReadFrameSize = 32 * 1024
		http2Transport.StrictMaxConcurrentStreams = true
	}

	// 初始化缓存管理器
	cacheManager, err := cache.NewCacheManager("data/mirror_cache")
	if err != nil {
		log.Printf("[Cache] Failed to initialize mirror cache manager: %v", err)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   mirrorTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}

	return &MirrorProxyHandler{
		mirrorService: service.NewMirrorProxyService(client, cacheManager),
		Cache:         cacheManager, // 保留字段以兼容现有代码
	}
}

func (h *MirrorProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	collector := metrics.GetCollector()
	collector.BeginRequest()
	defer collector.EndRequest()

	// 设置 CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	// 处理 OPTIONS 请求
	if r.Method == "OPTIONS" {
		h.handleCORSPreflight(w, r, startTime)
		return
	}

	// 提取目标URL
	mirrorReq, err := h.mirrorService.ExtractTargetURL(r)
	if err != nil {
		h.handleError(w, r, "Invalid URL", http.StatusBadRequest, startTime, err)
		return
	}

	// 检查缓存
	if item, hit, notModified := h.mirrorService.CheckCache(mirrorReq); hit {
		h.handleCacheHit(w, r, item, notModified, startTime, collector)
		return
	}

	// 创建代理请求
	proxyReq, err := h.mirrorService.CreateProxyRequest(mirrorReq)
	if err != nil {
		h.handleError(w, r, "Error creating request", http.StatusInternalServerError, startTime, err)
		return
	}

	// 执行请求
	resp, err := h.mirrorService.ExecuteRequest(proxyReq)
	if err != nil {
		h.handleError(w, r, "Error forwarding request", http.StatusBadGateway, startTime, err)
		return
	}
	defer resp.Body.Close()

	// 处理响应
	written, err := h.mirrorService.ProcessResponse(mirrorReq, resp, w)
	if err != nil {
		log.Printf("Error processing response: %v", err)
		return
	}

	// 记录访问日志
	log.Printf(h.mirrorService.CreateLogEntry(mirrorReq, resp.StatusCode, time.Since(startTime), written))

	// 记录统计信息
	collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(startTime), written, iputil.GetClientIP(r), r)
}

// handleCORSPreflight 处理CORS预检请求
func (h *MirrorProxyHandler) handleCORSPreflight(w http.ResponseWriter, r *http.Request, startTime time.Time) {
	w.WriteHeader(http.StatusOK)
	log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | CORS Preflight",
		r.Method, http.StatusOK, time.Since(startTime),
		iputil.GetClientIP(r), "-", r.URL.Path)
}

// handleError 处理错误
func (h *MirrorProxyHandler) handleError(w http.ResponseWriter, r *http.Request, message string, statusCode int, startTime time.Time, err error) {
	http.Error(w, message, statusCode)
	log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Error: %v",
		r.Method, statusCode, time.Since(startTime),
		iputil.GetClientIP(r), "-", r.URL.Path, err)
}

// handleCacheHit 处理缓存命中
func (h *MirrorProxyHandler) handleCacheHit(w http.ResponseWriter, r *http.Request, item *cache.CacheItem, notModified bool, startTime time.Time, collector *metrics.Collector) {
	w.Header().Set("Content-Type", item.ContentType)
	if item.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", item.ContentEncoding)
	}
	w.Header().Set("Proxy-Go-Cache-HIT", "1")
	
	if notModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	
	http.ServeFile(w, r, item.FilePath)
	collector.RecordRequest(r.URL.Path, http.StatusOK, time.Since(startTime), item.Size, iputil.GetClientIP(r), r)
}
