package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
	"strings"
	"time"
)

type MirrorProxyHandler struct {
	client *http.Client
}

func NewMirrorProxyHandler() *MirrorProxyHandler {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	return &MirrorProxyHandler{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
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

	// 复制响应头
	copyHeader(w.Header(), resp.Header)

	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	// 复制响应体
	bytesCopied, err := io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Error copying response: %v", err)
		return
	}

	// 记录访问日志
	log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | %s",
		r.Method, resp.StatusCode, time.Since(startTime),
		utils.GetClientIP(r), utils.FormatBytes(bytesCopied),
		utils.GetRequestSource(r), actualURL)

	// 记录统计信息
	collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(startTime), bytesCopied, utils.GetClientIP(r))
}
