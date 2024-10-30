package middleware

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

type CDNJSConfig struct {
	Path       string // 固定路径，例如 "/cdnjs"
	TargetHost string // 目标主机，例如 "cdnjs.cloudflare.com"
	TargetURL  string // 目标URL，例如 "https://cdnjs.cloudflare.com"
}

// CDNJSMiddleware 处理固定路径的代理
func CDNJSMiddleware(configs []CDNJSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查是否匹配任何固定路径
			for _, cfg := range configs {
				if strings.HasPrefix(r.URL.Path, cfg.Path) {
					// 创建新的请求
					targetPath := strings.TrimPrefix(r.URL.Path, cfg.Path)
					targetURL := cfg.TargetURL + targetPath

					proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
					if err != nil {
						http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
						return
					}

					// 复制原始请求的 header
					for key, values := range r.Header {
						for _, value := range values {
							proxyReq.Header.Add(key, value)
						}
					}

					// 设置必要的头部
					proxyReq.Host = cfg.TargetHost
					proxyReq.Header.Set("Host", cfg.TargetHost)
					proxyReq.Header.Set("X-Real-IP", getClientIP(r))
					proxyReq.Header.Set("X-Scheme", r.URL.Scheme)

					// 发送代理请求
					client := &http.Client{}
					resp, err := client.Do(proxyReq)
					if err != nil {
						http.Error(w, "Error forwarding request", http.StatusBadGateway)
						return
					}
					defer resp.Body.Close()

					// 复制响应头
					for key, values := range resp.Header {
						for _, value := range values {
							w.Header().Add(key, value)
						}
					}

					// 设置响应状态码
					w.WriteHeader(resp.StatusCode)

					// 复制响应体
					if _, err := io.Copy(w, resp.Body); err != nil {
						// 已经发送了响应头，只能记录错误
						log.Printf("Error copying response: %v", err)
					}
					return
				}
			}

			// 如果没有匹配的固定路径，继续下一个处理器
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP 获取客户端IP
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
