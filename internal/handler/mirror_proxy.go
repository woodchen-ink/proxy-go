package handler

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"proxy-go/internal/cache"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
	"strings"
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
	client *http.Client
	Cache  *cache.CacheManager
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

	return &MirrorProxyHandler{
		client: &http.Client{
			Transport: transport,
			Timeout:   mirrorTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		Cache: cacheManager,
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
		w.WriteHeader(http.StatusOK)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | CORS Preflight",
			r.Method, http.StatusOK, time.Since(startTime),
			iputil.GetClientIP(r), "-", r.URL.Path)
		return
	}

	// 从路径中提取实际URL
	actualURL := strings.TrimPrefix(r.URL.Path, "/mirror/")
	if actualURL == "" || actualURL == r.URL.Path {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Invalid URL",
			r.Method, http.StatusBadRequest, time.Since(startTime),
			iputil.GetClientIP(r), "-", r.URL.Path)
		return
	}

	if r.URL.RawQuery != "" {
		actualURL += "?" + r.URL.RawQuery
	}

	// 解析目标 URL 以获取 host
	parsedURL, err := url.Parse(actualURL)
	if err != nil {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Parse URL error: %v",
			r.Method, http.StatusBadRequest, time.Since(startTime),
			iputil.GetClientIP(r), "-", actualURL, err)
		return
	}

	// 确保有 scheme
	scheme := parsedURL.Scheme
	if scheme == "" {
		scheme = "https"
		actualURL = "https://" + actualURL
		parsedURL, _ = url.Parse(actualURL)
	}

	// 创建新的请求
	proxyReq, err := http.NewRequest(r.Method, actualURL, r.Body)
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Error creating request: %v",
			r.Method, http.StatusInternalServerError, time.Since(startTime),
			iputil.GetClientIP(r), "-", actualURL, err)
		return
	}

	// 复制原始请求的header
	copyHeader(proxyReq.Header, r.Header)

	// 设置必要的请求头
	proxyReq.Header.Set("Origin", fmt.Sprintf("%s://%s", scheme, parsedURL.Host))
	proxyReq.Header.Set("Referer", fmt.Sprintf("%s://%s/", scheme, parsedURL.Host))
	if ua := r.Header.Get("User-Agent"); ua != "" {
		proxyReq.Header.Set("User-Agent", ua)
	} else {
		proxyReq.Header.Set("User-Agent", "Mozilla/5.0")
	}
	proxyReq.Header.Set("Host", parsedURL.Host)
	proxyReq.Host = parsedURL.Host

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
			if notModified {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			http.ServeFile(w, r, item.FilePath)
			collector.RecordRequest(r.URL.Path, http.StatusOK, time.Since(startTime), item.Size, iputil.GetClientIP(r), r)
			return
		}
	}

	// 发送请求
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Error forwarding request: %v",
			r.Method, http.StatusBadGateway, time.Since(startTime),
			iputil.GetClientIP(r), "-", actualURL, err)
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Proxy-Go-Cache-HIT", "0")

	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	var written int64
	// 如果是GET请求且响应成功，使用TeeReader同时写入缓存
	if r.Method == http.MethodGet && resp.StatusCode == http.StatusOK && h.Cache != nil {
		cacheKey := h.Cache.GenerateCacheKey(r)
		if cacheFile, err := h.Cache.CreateTemp(cacheKey, resp); err == nil {
			defer cacheFile.Close()
			teeReader := io.TeeReader(resp.Body, cacheFile)
			written, err = io.Copy(w, teeReader)
			if err == nil {
				h.Cache.Commit(cacheKey, cacheFile.Name(), resp, written)
			}
		} else {
			written, err = io.Copy(w, resp.Body)
			if err != nil && !isConnectionClosed(err) {
				log.Printf("Error writing response: %v", err)
				return
			}
		}
	} else {
		written, err = io.Copy(w, resp.Body)
		if err != nil && !isConnectionClosed(err) {
			log.Printf("Error writing response: %v", err)
			return
		}
	}

	// 记录访问日志
	log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | %s",
		r.Method, resp.StatusCode, time.Since(startTime),
		iputil.GetClientIP(r), utils.FormatBytes(written),
		utils.GetRequestSource(r), actualURL)

	// 记录统计信息
	collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(startTime), written, iputil.GetClientIP(r), r)
}
