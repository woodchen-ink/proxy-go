package middleware

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"proxy-go/internal/config"
	"strings"
	"syscall"
	"time"
)

type FixedPathConfig struct {
	Path       string `json:"Path"`
	TargetHost string `json:"TargetHost"`
	TargetURL  string `json:"TargetURL"`
}

func FixedPathProxyMiddleware(configs []config.FixedPathConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now() // 添加时间记录
			// 检查是否匹配任何固定路径
			for _, cfg := range configs {
				if strings.HasPrefix(r.URL.Path, cfg.Path) {
					// 创建新的请求
					targetPath := strings.TrimPrefix(r.URL.Path, cfg.Path)
					targetURL := cfg.TargetURL + targetPath

					proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
					if err != nil {
						http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
						log.Printf("[%s] %s %s -> 500 (error creating request: %v) [%v]",
							getClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
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
						log.Printf("[%s] %s %s -> 502 (proxy error: %v) [%v]",
							getClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
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
					bytesCopied, err := io.Copy(w, resp.Body)
					if err := handleCopyError(err); err != nil {
						log.Printf("[%s] Error copying response: %v", getClientIP(r), err)
					}

					// 记录成功的请求
					log.Printf("[%s] %s %s -> %s -> %d (%s) [%v]",
						getClientIP(r), r.Method, r.URL.Path, targetURL,
						resp.StatusCode, formatBytes(bytesCopied), time.Since(startTime))

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

func handleCopyError(err error) error {
	if err == nil {
		return nil
	}

	// 忽略常见的连接关闭错误
	if errors.Is(err, syscall.EPIPE) || // broken pipe
		errors.Is(err, syscall.ECONNRESET) || // connection reset by peer
		strings.Contains(err.Error(), "broken pipe") ||
		strings.Contains(err.Error(), "connection reset by peer") {
		return nil
	}

	return err
}

// formatBytes 将字节数转换为可读的格式（MB/KB/Bytes）
func formatBytes(bytes int64) string {
	const (
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}
