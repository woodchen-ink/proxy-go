package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBufferSize = 32 * 1024 // 32KB
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, defaultBufferSize)
	},
}

type ProxyHandler struct {
	pathMap   map[string]config.PathConfig
	client    *http.Client
	limiter   *rate.Limiter
	startTime time.Time
	config    *config.Config
	auth      *authManager
}

// 修改参数类型
func NewProxyHandler(cfg *config.Config) *ProxyHandler {
	transport := &http.Transport{
		MaxIdleConns:        100,              // 最大空闲连接数
		MaxIdleConnsPerHost: 10,               // 每个 host 的最大空闲连接数
		IdleConnTimeout:     90 * time.Second, // 空闲连接超时时间
	}

	return &ProxyHandler{
		pathMap: cfg.MAP,
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		limiter:   rate.NewLimiter(rate.Limit(5000), 10000),
		startTime: time.Now(),
		config:    cfg,
		auth:      newAuthManager(),
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	collector := metrics.GetCollector()
	collector.BeginRequest()
	defer collector.EndRequest()

	if !h.limiter.Allow() {
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	start := time.Now()

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
		http.Error(w, "Error decoding path", http.StatusInternalServerError)
		log.Printf("[%s] %s %s -> 500 (error decoding path: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(start))
		return
	}

	// 确定���标基础URL
	targetBase := pathConfig.DefaultTarget

	// 检查文件扩展名
	if pathConfig.ExtensionMap != nil {
		ext := strings.ToLower(path.Ext(decodedPath))
		if ext != "" {
			ext = ext[1:] // 移除开头的点
			targetBase = pathConfig.GetTargetForExt(ext)
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
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(start))
		return
	}

	// 创建新的请求
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		return
	}

	// 复制原始请求头
	copyHeader(proxyReq.Header, r.Header)

	// 特别处理图片请求
	// if utils.IsImageRequest(r.URL.Path) {
	// 	// 设置优化的 Accept 头
	// 	accept := r.Header.Get("Accept")
	// 	if accept != "" {
	// 		proxyReq.Header.Set("Accept", accept)
	// 	} else {
	// 		proxyReq.Header.Set("Accept", "image/avif,image/webp,image/jpeg,image/png,*/*;q=0.8")
	// 	}

	// 	// 设置 Cloudflare 特定的头部
	// 	proxyReq.Header.Set("CF-Accept-Content", "image/avif,image/webp")
	// 	proxyReq.Header.Set("CF-Optimize-Images", "on")

	// 	// 删除可能影响缓存的头部
	// 	proxyReq.Header.Del("If-None-Match")
	// 	proxyReq.Header.Del("If-Modified-Since")
	// 	proxyReq.Header.Set("Cache-Control", "no-cache")
	// }
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

	// 发送代理请求
	resp, err := h.client.Do(proxyReq)

	if err != nil {
		http.Error(w, "Error forwarding request", http.StatusBadGateway)
		log.Printf("[%s] %s %s -> 502 (proxy error: %v) [%v]",
			utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(start))
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)

	// 删除严格的 CSP
	w.Header().Del("Content-Security-Policy")

	// 设置响应状态码
	w.WriteHeader(resp.StatusCode)

	// 根据响应大小选择不同的处理策略
	contentLength := resp.ContentLength
	if contentLength > 0 && contentLength < 1<<20 { // 1MB 以下的小响应
		// 直接读取到内存并一次性写入
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "Error reading response", http.StatusInternalServerError)
			return
		}
		written, _ := w.Write(body)
		collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(start), int64(written), utils.GetClientIP(r))
	} else {
		// 大响应使用流式传输
		var bytesCopied int64
		if f, ok := w.(http.Flusher); ok {
			buf := bufferPool.Get().([]byte)
			defer bufferPool.Put(buf)
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
			time.Since(start),              // 处理时间，占12位
			utils.GetClientIP(r),           // IP地址，占15位
			utils.FormatBytes(bytesCopied), // 传输大小，占10位
			utils.GetRequestSource(r),      // 请求来源
			r.URL.Path,                     // 请求路径，左对齐占50位
			targetURL,                      // 目标URL
		)

		collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(start), bytesCopied, utils.GetClientIP(r))
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
