package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"proxy-go/internal/metrics"
	"proxy-go/internal/models"
	"proxy-go/internal/utils"
	"runtime"
	"time"
)

// Metrics 定义指标结构
type Metrics struct {
	// 基础指标
	Uptime         string  `json:"uptime"`
	ActiveRequests int64   `json:"active_requests"`
	TotalRequests  int64   `json:"total_requests"`
	TotalErrors    int64   `json:"total_errors"`
	ErrorRate      float64 `json:"error_rate"`

	// 系统指标
	NumGoroutine int    `json:"num_goroutine"`
	MemoryUsage  string `json:"memory_usage"`

	// 性能指标
	AverageResponseTime string  `json:"avg_response_time"`
	RequestsPerSecond   float64 `json:"requests_per_second"`

	// 新增字段
	TotalBytes      int64                    `json:"total_bytes"`
	BytesPerSecond  float64                  `json:"bytes_per_second"`
	StatusCodeStats map[string]int64         `json:"status_code_stats"`
	TopPaths        []models.PathMetricsJSON `json:"top_paths"`
	RecentRequests  []models.RequestLog      `json:"recent_requests"`
	TopReferers     []models.PathMetricsJSON `json:"top_referers"`
}

// MetricsHandler 处理指标请求
func (h *ProxyHandler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(h.startTime)
	collector := metrics.GetCollector()
	stats := collector.GetStats()

	if stats == nil {
		stats = map[string]interface{}{
			"uptime":              metrics.FormatUptime(uptime),
			"active_requests":     int64(0),
			"total_requests":      int64(0),
			"total_errors":        int64(0),
			"error_rate":          float64(0),
			"num_goroutine":       runtime.NumGoroutine(),
			"memory_usage":        "0 B",
			"avg_response_time":   "0 ms",
			"total_bytes":         int64(0),
			"bytes_per_second":    float64(0),
			"requests_per_second": float64(0),
			"status_code_stats":   make(map[string]int64),
			"latency_percentiles": make([]float64, 0),
			"top_paths":           make([]models.PathMetrics, 0),
			"recent_requests":     make([]models.RequestLog, 0),
			"top_referers":        make([]models.PathMetrics, 0),
		}
	}

	totalRequests := utils.SafeInt64(stats["total_requests"])
	totalErrors := utils.SafeInt64(stats["total_errors"])
	totalBytes := utils.SafeInt64(stats["total_bytes"])
	uptimeSeconds := uptime.Seconds()

	metrics := Metrics{
		Uptime:              metrics.FormatUptime(uptime),
		ActiveRequests:      utils.SafeInt64(stats["active_requests"]),
		TotalRequests:       totalRequests,
		TotalErrors:         totalErrors,
		ErrorRate:           float64(totalErrors) / float64(utils.Max(totalRequests, 1)),
		NumGoroutine:        utils.SafeInt(stats["num_goroutine"]),
		MemoryUsage:         utils.SafeString(stats["memory_usage"], "0 B"),
		AverageResponseTime: utils.SafeString(stats["avg_response_time"], "0 ms"),
		TotalBytes:          totalBytes,
		BytesPerSecond:      float64(totalBytes) / utils.MaxFloat64(uptimeSeconds, 1),
		RequestsPerSecond:   float64(totalRequests) / utils.MaxFloat64(uptimeSeconds, 1),
		StatusCodeStats:     models.SafeStatusCodeStats(stats["status_code_stats"]),
		TopPaths:            models.SafePathMetrics(stats["top_paths"]),
		RecentRequests:      models.SafeRequestLogs(stats["recent_requests"]),
		TopReferers:         models.SafePathMetrics(stats["top_referers"]),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		log.Printf("Error encoding metrics: %v", err)
	}
}
