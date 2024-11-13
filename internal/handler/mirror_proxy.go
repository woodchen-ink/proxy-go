// handler/mirror_proxy.go
package handler

import (
	"io"
	"log"
	"net/http"
	"proxy-go/internal/utils"
	"strings"
	"time"
)

type MirrorProxyHandler struct{}

func NewMirrorProxyHandler() *MirrorProxyHandler {
	return &MirrorProxyHandler{}
}

func (h *MirrorProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// 从路径中提取实际URL
	// 例如：/mirror/https://example.com/path 变成 https://example.com/path
	actualURL := strings.TrimPrefix(r.URL.Path, "/mirror/")
	if actualURL == "" || actualURL == r.URL.Path {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	// 添加原始请求中的查询参数和片段
	if r.URL.RawQuery != "" {
		actualURL += "?" + r.URL.RawQuery
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

	// 发送请求
	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | Error forwarding request: %v",
			r.Method, http.StatusBadGateway, time.Since(startTime),
			utils.GetClientIP(r), "-", actualURL, err)
		return
	}
	defer resp.Body.Close()

	// 设置CORS头
	w.Header().Set("Access-Control-Allow-Origin", "*")

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
}
