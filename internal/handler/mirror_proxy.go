package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"proxy-go/internal/cache"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
	"strings"
	"time"
)

type MirrorProxyHandler struct {
	client *http.Client
	Cache  *cache.CacheManager
}

func NewMirrorProxyHandler() *MirrorProxyHandler {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	// 初始化缓存管理器
	cacheManager, err := cache.NewCacheManager("data/mirror_cache")
	if err != nil {
		log.Printf("[Cache] Failed to initialize mirror cache manager: %v", err)
	}

	return &MirrorProxyHandler{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
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
			utils.GetClientIP(r), "-", r.URL.Path)
		return
	}

	// 从路径中提取实际URL
	actualURL := strings.TrimPrefix(r.URL.Path, "/mirror/")
	if actualURL == "" || actualURL == r.URL.Path {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Invalid URL",
			r.Method, http.StatusBadRequest, time.Since(startTime),
			utils.GetClientIP(r), "-", r.URL.Path)
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
			utils.GetClientIP(r), "-", actualURL, err)
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
			utils.GetClientIP(r), "-", actualURL, err)
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
			w.Header().Set("Proxy-Go-Cache", "HIT")
			if notModified {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			http.ServeFile(w, r, item.FilePath)
			collector.RecordRequest(r.URL.Path, http.StatusOK, time.Since(startTime), item.Size, utils.GetClientIP(r), r)
			return
		}
	}

	// 发送请求
	resp, err := h.client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Error forwarding request: %v",
			r.Method, http.StatusBadGateway, time.Since(startTime),
			utils.GetClientIP(r), "-", actualURL, err)
		return
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading response", http.StatusInternalServerError)
		log.Printf("Error reading response: %v", err)
		return
	}

	// 如果是GET请求且响应成功，尝试缓存
	if r.Method == http.MethodGet && resp.StatusCode == http.StatusOK && h.Cache != nil {
		cacheKey := h.Cache.GenerateCacheKey(r)
		if _, err := h.Cache.Put(cacheKey, resp, body); err != nil {
			log.Printf("[Cache] Failed to cache %s: %v", actualURL, err)
		}
	}

	// 复制响应头
	copyHeader(w.Header(), resp.Header)
	w.Header().Set("Proxy-Go-Cache", "MISS")

	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	// 写入响应体
	written, err := w.Write(body)
	if err != nil {
		log.Printf("Error writing response: %v", err)
		return
	}

	// 记录访问日志
	log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | %s",
		r.Method, resp.StatusCode, time.Since(startTime),
		utils.GetClientIP(r), utils.FormatBytes(int64(written)),
		utils.GetRequestSource(r), actualURL)

	// 记录统计信息
	collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(startTime), int64(written), utils.GetClientIP(r), r)
}
