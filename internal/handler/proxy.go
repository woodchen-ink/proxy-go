package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"proxy-go/internal/config"
	"proxy-go/internal/utils"
	"strings"
	"time"
)

const (
	defaultBufferSize = 32 * 1024 // 32KB
)

type ProxyHandler struct {
	pathMap map[string]config.PathConfig
}

func NewProxyHandler(pathMap map[string]interface{}) *ProxyHandler {
	convertedMap := make(map[string]config.PathConfig)

	for path, target := range pathMap {
		convertedMap[path] = config.NewPathConfig(target)
	}

	return &ProxyHandler{
		pathMap: convertedMap,
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// 处理根路径请求
	if r.URL.Path == "/" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Welcome to CZL proxy.")
		log.Printf("[%s] %s %s -> %d (root path) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, http.StatusOK, time.Since(startTime))
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
			utils.GetClientIP(r), r.Method, r.URL.Path, time.Since(startTime))
		return
	}

	// 构建目标 URL
	targetPath := strings.TrimPrefix(r.URL.Path, matchedPrefix)

	// URL 解码，然后重新编码，确保特殊字符被正确处理
	decodedPath, err := url.QueryUnescape(targetPath)
	if err != nil {
		http.Error(w, "Error decoding path", http.StatusInternalServerError)
		log.Printf("[%s] %s %s -> 500 (error decoding path: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}

	// 确定目标基础URL
	targetBase := pathConfig.DefaultTarget

	// 检查文件扩展名
	if pathConfig.ExtensionMap != nil {
		ext := strings.ToLower(path.Ext(decodedPath))
		if ext != "" {
			ext = ext[1:] // 移除开头的点
			if alternativeTarget, exists := pathConfig.ExtensionMap[ext]; exists {
				targetBase = alternativeTarget
			}
		}
	}

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
		http.Error(w, "Error parsing target URL", http.StatusInternalServerError)
		log.Printf("[%s] %s %s -> 500 (error parsing URL: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}

	// 创建新的请求
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		log.Printf("[%s] %s %s -> 500 (error: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}

	// 复制原始请求的 header
	copyHeader(proxyReq.Header, r.Header)

	// 设置必要的头部，使用目标站点的 Host
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

	// 发送代理请求
	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true, // 禁用自动压缩
		},
	}
	resp, err := client.Do(proxyReq)

	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		log.Printf("[%s] %s %s -> 502 (proxy error: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)

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
		// 如果不支持 Flusher，使用普通的 io.Copy
		bytesCopied, err = io.Copy(w, resp.Body)
		if err != nil {
			log.Printf("Error copying response: %v", err)
		}
	}

	// 记录访问日志
	log.Printf("| %-6s | %3d | %12s | %15s | %10s | %-30s | %-50s -> %s",
		r.Method,                       // HTTP方法，左对齐占6位
		resp.StatusCode,                // 状态码，占3位
		time.Since(startTime),          // 处理时间，占12位
		utils.GetClientIP(r),           // IP地址，占15位
		utils.FormatBytes(bytesCopied), // 传输大小，占10位
		utils.GetRequestSource(r),      // 请求来源
		r.URL.Path,                     // 请求路径，左对齐占50位
		targetURL,                      // 目标URL
	)

}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
