package handler

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBufferSize = 32 * 1024 // 32KB
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
	startTime := time.Now()

	// 处理根路径请求
	if r.URL.Path == "/" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Welcome to CZL proxy.")
		log.Printf("[%s] %s %s -> %d (root path) [%v]",
			getClientIP(r), r.Method, r.URL.Path, http.StatusOK, time.Since(startTime))
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
		log.Printf("[%s] %s %s -> 404 (not found) [%v]",
			getClientIP(r), r.Method, r.URL.Path, time.Since(startTime))
		return
	}

	// 构建目标 URL
	targetPath := strings.TrimPrefix(r.URL.Path, matchedPrefix)
	targetURL := targetBase + targetPath

	// 解析目标 URL 以获取 host
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Error parsing target URL", http.StatusInternalServerError)
		log.Printf("[%s] %s %s -> 500 (error parsing URL: %v) [%v]",
			getClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}

	// 创建新的请求
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		log.Printf("[%s] %s %s -> 500 (error: %v) [%v]",
			getClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}

	// 复制原始请求的 header
	copyHeader(proxyReq.Header, r.Header)

	// 设置必要的头部，使用目标站点的 Host
	proxyReq.Host = parsedURL.Host
	proxyReq.Header.Set("Host", parsedURL.Host)
	proxyReq.Header.Set("X-Real-IP", getClientIP(r))
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", r.URL.Scheme)

	// 添加或更新 X-Forwarded-For
	if clientIP := getClientIP(r); clientIP != "" {
		if prior := proxyReq.Header.Get("X-Forwarded-For"); prior != "" {
			proxyReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			proxyReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}

	// 设置接受压缩内容的header
	proxyReq.Header.Set("Accept-Encoding", r.Header.Get("Accept-Encoding"))

	// 发送代理请求
	client := &http.Client{
		Transport: &http.Transport{
			// 禁用自动处理压缩，让我们能完整传递压缩内容
			DisableCompression: true,
		},
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		log.Printf("[%s] %s %s -> 502 (proxy error: %v) [%v]",
			getClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}
	defer resp.Body.Close()

	// 复制所有响应头
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	// 确保Content-Type正确设置
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// 保持原始的Content-Encoding
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}

	// 设置响应状态码
	w.WriteHeader(resp.StatusCode)

	// 使用流式传输复制响应体
	var bytesCopied int64
	if f, ok := w.(http.Flusher); ok {
		buf := make([]byte, defaultBufferSize)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				bytesCopied += int64(n)
				_, werr := w.Write(buf[:n])
				if werr != nil {
					log.Printf("Error writing response: %v", werr)
					return
				}
				f.Flush()
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				log.Printf("Error reading response: %v", rerr)
				break
			}
		}
	} else {
		bytesCopied, err = io.Copy(w, resp.Body)
		if err != nil {
			log.Printf("Error copying response: %v", err)
		}
	}

	// 记录访问日志，添加内容类型信息
	log.Printf("[%s] %s %s -> %s -> %d (%d bytes) [%v] Content-Type: %s, Encoding: %s",
		getClientIP(r), r.Method, r.URL.Path, targetURL,
		resp.StatusCode, bytesCopied, time.Since(startTime),
		resp.Header.Get("Content-Type"),
		resp.Header.Get("Content-Encoding"))
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}
