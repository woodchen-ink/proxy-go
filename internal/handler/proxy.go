package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

type ProxyHandler struct {
	pathMap map[string]string
}

func NewProxyHandler(pathMap map[string]string) *ProxyHandler {
	return &ProxyHandler{
		pathMap: pathMap,
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 处理根路径请求
	if r.URL.Path == "/" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Welcome to CZL proxy.")
		return
	}

	// 查找匹配的代理路径
	var matchedPrefix string
	var targetBase string
	for prefix, target := range h.pathMap {
		if strings.HasPrefix(r.URL.Path, prefix) {
			matchedPrefix = prefix
			targetBase = target
			break
		}
	}

	// 如果没有匹配的路径，返回 404
	if matchedPrefix == "" {
		http.NotFound(w, r)
		return
	}

	// 构建目标 URL
	targetPath := strings.TrimPrefix(r.URL.Path, matchedPrefix)
	targetURL := targetBase + targetPath

	// 创建新的请求
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}

	// 复制原始请求的 header
	copyHeader(proxyReq.Header, r.Header)

	// 设置一些必要的头部
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Real-IP", getClientIP(r))

	// 发送代理请求
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 复制响应 header
	copyHeader(w.Header(), resp.Header)

	// 设置响应状态码
	w.WriteHeader(resp.StatusCode)

	// 复制响应体
	if _, err := io.Copy(w, resp.Body); err != nil {
		// 这里只记录错误，不返回给客户端，因为响应头已经发送
		fmt.Printf("Error copying response: %v\n", err)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func getClientIP(r *http.Request) string {
	// 检查各种可能的请求头
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	// 从RemoteAddr获取
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
