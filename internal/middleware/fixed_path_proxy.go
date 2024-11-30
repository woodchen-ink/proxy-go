package middleware

import (
	"errors"
	"io"
	"log"
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/utils"
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
			startTime := time.Now()
			collector := metrics.GetCollector()
			collector.BeginRequest()
			defer collector.EndRequest()

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
							utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
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
					proxyReq.Header.Set("X-Real-IP", utils.GetClientIP(r))
					proxyReq.Header.Set("X-Scheme", r.URL.Scheme)

					// 发送代理请求
					client := &http.Client{}
					resp, err := client.Do(proxyReq)
					if err != nil {
						http.Error(w, "Error forwarding request", http.StatusBadGateway)
						log.Printf("[%s] %s %s -> 502 (proxy error: %v) [%v]",
							utils.GetClientIP(r), r.Method, r.URL.Path, err, time.Since(startTime))
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
						log.Printf("[%s] Error copying response: %v", utils.GetClientIP(r), err)
					}

					// 记录统计信息
					collector.RecordRequest(r.URL.Path, resp.StatusCode, time.Since(startTime), bytesCopied, utils.GetClientIP(r))

					return
				}
			}

			// 如果没有匹配的固定路径，继续下一个处理器
			next.ServeHTTP(w, r)
		})
	}
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
