package service

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
)

// TargetHealth 目标服务器健康状态
type TargetHealth struct {
	URL            string        // 目标URL
	IsHealthy      bool          // 是否健康
	LastCheck      time.Time     // 上次检查时间
	LastSuccess    time.Time     // 上次成功时间
	FailCount      int           // 连续失败次数
	SuccessCount   int           // 连续成功次数
	TotalRequests  int64         // 总请求数
	FailedRequests int64         // 失败请求数
	AvgLatency     time.Duration // 平均延迟
	LastError      string        // 最后一次错误信息
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Enabled           bool          // 是否启用健康检查
	CheckInterval     time.Duration // 检查间隔
	Timeout           time.Duration // 检查超时时间
	FailThreshold     int           // 失败阈值（连续失败多少次标记为不健康）
	SuccessThreshold  int           // 成功阈值（连续成功多少次恢复健康）
	UnhealthyDuration time.Duration // 不健康持续时间（超过此时间后重新检查）
}

// DefaultHealthCheckConfig 默认健康检查配置
var DefaultHealthCheckConfig = HealthCheckConfig{
	Enabled:           true,
	CheckInterval:     30 * time.Second, // 每30秒检查一次
	Timeout:           5 * time.Second,  // 5秒超时
	FailThreshold:     3,                // 连续失败3次标记为不健康
	SuccessThreshold:  2,                // 连续成功2次恢复健康
	UnhealthyDuration: 5 * time.Minute,  // 不健康5分钟后重新检查
}

// HealthChecker 健康检查器
type HealthChecker struct {
	config  HealthCheckConfig
	targets sync.Map // map[string]*TargetHealth
	client  *http.Client
	stopCh  chan struct{}
	mu      sync.RWMutex
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(config HealthCheckConfig) *HealthChecker {
	hc := &HealthChecker{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // 不跟随重定向
			},
		},
		stopCh: make(chan struct{}),
	}

	// 如果启用了主动健康检查，启动后台检查协程
	if config.Enabled {
		go hc.startBackgroundCheck()
	}

	return hc
}

// RecordRequest 记录请求结果（被动健康检查）
func (hc *HealthChecker) RecordRequest(url string, success bool, latency time.Duration, err error) {
	if !hc.config.Enabled {
		return
	}

	// 获取或创建健康状态
	value, _ := hc.targets.LoadOrStore(url, &TargetHealth{
		URL:       url,
		IsHealthy: true,
		LastCheck: time.Now(),
	})

	health := value.(*TargetHealth)
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// 更新统计信息
	health.TotalRequests++
	health.LastCheck = time.Now()

	if success {
		health.SuccessCount++
		health.FailCount = 0
		health.LastSuccess = time.Now()

		// 更新平均延迟（移动平均）
		if health.AvgLatency == 0 {
			health.AvgLatency = latency
		} else {
			health.AvgLatency = (health.AvgLatency*9 + latency) / 10
		}

		// 连续成功达到阈值，标记为健康
		if health.SuccessCount >= hc.config.SuccessThreshold {
			if !health.IsHealthy {
				log.Printf("[Health] Target %s recovered to healthy (success count: %d)",
					url, health.SuccessCount)
			}
			health.IsHealthy = true
		}
	} else {
		health.FailedRequests++
		health.FailCount++
		health.SuccessCount = 0

		if err != nil {
			health.LastError = err.Error()
		}

		// 连续失败达到阈值，标记为不健康
		if health.FailCount >= hc.config.FailThreshold {
			if health.IsHealthy {
				log.Printf("[Health] Target %s marked as unhealthy (fail count: %d, error: %s)",
					url, health.FailCount, health.LastError)
			}
			health.IsHealthy = false
		}
	}
}

// IsHealthy 检查目标是否健康
func (hc *HealthChecker) IsHealthy(url string) bool {
	if !hc.config.Enabled {
		return true // 未启用健康检查时，默认都健康
	}

	value, ok := hc.targets.Load(url)
	if !ok {
		return true // 未记录的目标默认健康
	}

	health := value.(*TargetHealth)
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	// 如果不健康但距离上次检查超过了不健康持续时间，给一次重试机会
	if !health.IsHealthy && time.Since(health.LastCheck) > hc.config.UnhealthyDuration {
		log.Printf("[Health] Target %s unhealthy duration exceeded, allowing retry", url)
		return true
	}

	return health.IsHealthy
}

// GetHealth 获取目标健康状态
func (hc *HealthChecker) GetHealth(url string) *TargetHealth {
	value, ok := hc.targets.Load(url)
	if !ok {
		return &TargetHealth{
			URL:       url,
			IsHealthy: true,
		}
	}

	health := value.(*TargetHealth)
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	// 返回副本，避免并发修改
	return &TargetHealth{
		URL:            health.URL,
		IsHealthy:      health.IsHealthy,
		LastCheck:      health.LastCheck,
		LastSuccess:    health.LastSuccess,
		FailCount:      health.FailCount,
		SuccessCount:   health.SuccessCount,
		TotalRequests:  health.TotalRequests,
		FailedRequests: health.FailedRequests,
		AvgLatency:     health.AvgLatency,
		LastError:      health.LastError,
	}
}

// GetAllHealth 获取所有目标的健康状态
func (hc *HealthChecker) GetAllHealth() map[string]*TargetHealth {
	result := make(map[string]*TargetHealth)

	hc.targets.Range(func(key, value interface{}) bool {
		url := key.(string)
		health := value.(*TargetHealth)

		hc.mu.RLock()
		result[url] = &TargetHealth{
			URL:            health.URL,
			IsHealthy:      health.IsHealthy,
			LastCheck:      health.LastCheck,
			LastSuccess:    health.LastSuccess,
			FailCount:      health.FailCount,
			SuccessCount:   health.SuccessCount,
			TotalRequests:  health.TotalRequests,
			FailedRequests: health.FailedRequests,
			AvgLatency:     health.AvgLatency,
			LastError:      health.LastError,
		}
		hc.mu.RUnlock()

		return true
	})

	return result
}

// startBackgroundCheck 启动后台主动健康检查
func (hc *HealthChecker) startBackgroundCheck() {
	ticker := time.NewTicker(hc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.checkAllTargets()
		case <-hc.stopCh:
			log.Println("[Health] Background health check stopped")
			return
		}
	}
}

// checkAllTargets 检查所有目标
func (hc *HealthChecker) checkAllTargets() {
	hc.targets.Range(func(key, value interface{}) bool {
		url := key.(string)
		health := value.(*TargetHealth)

		// 只检查不健康的目标或距离上次检查超过检查间隔的目标
		hc.mu.RLock()
		shouldCheck := !health.IsHealthy || time.Since(health.LastCheck) > hc.config.CheckInterval
		hc.mu.RUnlock()

		if shouldCheck {
			go hc.checkTarget(url)
		}

		return true
	})
}

// checkTarget 主动检查单个目标
func (hc *HealthChecker) checkTarget(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), hc.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		log.Printf("[Health] Failed to create health check request for %s: %v", url, err)
		hc.RecordRequest(url, false, 0, err)
		return
	}

	start := time.Now()
	resp, err := hc.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		log.Printf("[Health] Health check failed for %s: %v", url, err)
		hc.RecordRequest(url, false, latency, err)
		return
	}
	defer resp.Body.Close()

	// 2xx 和 3xx 状态码认为是健康的
	success := resp.StatusCode >= 200 && resp.StatusCode < 400
	hc.RecordRequest(url, success, latency, nil)

	if success {
		log.Printf("[Health] Health check succeeded for %s (status: %d, latency: %v)",
			url, resp.StatusCode, latency)
	} else {
		log.Printf("[Health] Health check failed for %s (status: %d)",
			url, resp.StatusCode)
	}
}

// Stop 停止健康检查器
func (hc *HealthChecker) Stop() {
	if hc.config.Enabled {
		close(hc.stopCh)
	}
}

// ResetTarget 重置目标健康状态
func (hc *HealthChecker) ResetTarget(url string) {
	hc.targets.Delete(url)
	log.Printf("[Health] Reset health status for %s", url)
}
