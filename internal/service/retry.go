package service

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries   int           // 最大重试次数
	InitialDelay time.Duration // 初始延迟
	MaxDelay     time.Duration // 最大延迟
	Multiplier   float64       // 延迟倍增因子
}

// DefaultRetryConfig 默认重试配置
var DefaultRetryConfig = RetryConfig{
	MaxRetries:   2,                      // 最多重试2次 (总共3次请求)
	InitialDelay: 100 * time.Millisecond, // 初始延迟100ms
	MaxDelay:     2 * time.Second,        // 最大延迟2s
	Multiplier:   2.0,                    // 指数退避因子
}

// isRetriableError 判断错误是否可重试
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// 网络相关临时错误
	retriableErrors := []string{
		"timeout",
		"temporary failure",
		"connection reset",
		"connection refused", // 可能是临时的
		"no such host",       // DNS 临时失败
		"EOF",                // 连接异常关闭
		"broken pipe",
		"i/o timeout",
		"TLS handshake timeout",
		"net/http: request canceled",
	}

	for _, retryErr := range retriableErrors {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(retryErr)) {
			return true
		}
	}

	return false
}

// isRetriableStatusCode 判断HTTP状态码是否可重试
func isRetriableStatusCode(code int) bool {
	retriableCodes := map[int]bool{
		http.StatusRequestTimeout:      true, // 408
		http.StatusTooManyRequests:     true, // 429
		http.StatusInternalServerError: true, // 500
		http.StatusBadGateway:          true, // 502
		http.StatusServiceUnavailable:  true, // 503
		http.StatusGatewayTimeout:      true, // 504
	}
	return retriableCodes[code]
}

// ExecuteWithRetry 执行带重试的HTTP请求
func ExecuteWithRetry(client *http.Client, req *http.Request, config RetryConfig) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response
	delay := config.InitialDelay

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// 第一次不延迟
		if attempt > 0 {
			// 等待延迟或上下文取消
			select {
			case <-time.After(delay):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}

			// 指数退避
			delay = time.Duration(float64(delay) * config.Multiplier)
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}

			log.Printf("[Retry] Attempt %d/%d for %s (delay: %v, last error: %v)",
				attempt+1, config.MaxRetries+1, req.URL.String(), delay, lastErr)
		}

		// 克隆请求（因为请求体可能已被读取）
		reqClone := cloneRequest(req)

		// 执行请求
		resp, err := client.Do(reqClone)

		// 请求成功
		if err == nil {
			// 检查状态码是否可重试
			if !isRetriableStatusCode(resp.StatusCode) {
				return resp, nil
			}

			// 状态码可重试,保存响应并关闭
			lastResp = resp
			lastErr = fmt.Errorf("retriable status code: %d", resp.StatusCode)
			resp.Body.Close()
			continue
		}

		// 请求失败,检查错误是否可重试
		lastErr = err

		// 不可重试的错误,直接返回
		if !isRetriableError(err) {
			log.Printf("[Retry] Non-retriable error for %s: %v", req.URL.String(), err)
			return nil, err
		}

		// 可重试的错误,继续下一次尝试
		log.Printf("[Retry] Retriable error for %s: %v", req.URL.String(), err)
	}

	// 所有重试都失败了
	if lastResp != nil {
		// 返回最后一次的响应（即使状态码不好）
		log.Printf("[Retry] Max retries exceeded for %s, returning last response with status %d",
			req.URL.String(), lastResp.StatusCode)
		return lastResp, nil
	}

	// 返回最后一次的错误
	log.Printf("[Retry] Max retries exceeded for %s: %v", req.URL.String(), lastErr)
	return nil, fmt.Errorf("max retries exceeded: %v", lastErr)
}

// cloneRequest 克隆HTTP请求（处理请求体）
func cloneRequest(req *http.Request) *http.Request {
	// 创建新的请求
	reqClone := req.Clone(req.Context())

	// 如果有请求体,需要特殊处理
	// 注意: 这里假设请求体可以被多次读取（对于代理场景通常是true）
	// 对于POST等请求,调用者需要确保Body支持重复读取

	return reqClone
}

// RetryStats 重试统计信息
type RetryStats struct {
	TotalRequests   int64 // 总请求数
	RetriedRequests int64 // 重试的请求数
	SuccessAfterRetry int64 // 重试后成功的请求数
	FailedAfterRetry  int64 // 重试后仍失败的请求数
}

// RetryService 重试服务（可选，用于统计）
type RetryService struct {
	config RetryConfig
	stats  RetryStats
}

// NewRetryService 创建重试服务
func NewRetryService(config RetryConfig) *RetryService {
	return &RetryService{
		config: config,
	}
}

// GetStats 获取重试统计
func (rs *RetryService) GetStats() RetryStats {
	return rs.stats
}
