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

// Metrics 定义指标结构，与前端期望的数据结构保持一致
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

	// 传输指标
	TotalBytes     int64   `json:"total_bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`

	// 状态码统计
	StatusCodeStats map[string]int64 `json:"status_code_stats"`

	// 路径统计
	TopPaths []models.PathMetricsJSON `json:"top_paths"`

	// 最近请求
	RecentRequests []models.RequestLog `json:"recent_requests"`

	// 引用来源统计
	TopReferers []models.PathMetricsJSON `json:"top_referers"`

	// 延迟统计
	LatencyStats struct {
		Min          string           `json:"min"`
		Max          string           `json:"max"`
		Distribution map[string]int64 `json:"distribution"`
	} `json:"latency_stats"`

	// 错误统计
	ErrorStats struct {
		ClientErrors int64            `json:"client_errors"`
		ServerErrors int64            `json:"server_errors"`
		Types        map[string]int64 `json:"types"`
	} `json:"error_stats"`

	// 带宽统计
	BandwidthHistory map[string]string `json:"bandwidth_history"`
	CurrentBandwidth string            `json:"current_bandwidth"`
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
			"top_paths":           make([]models.PathMetrics, 0),
			"recent_requests":     make([]models.RequestLog, 0),
			"top_referers":        make([]models.PathMetrics, 0),
			"latency_stats": map[string]interface{}{
				"min":          "0ms",
				"max":          "0ms",
				"distribution": make(map[string]int64),
			},
			"bandwidth_history": make(map[string]string),
			"current_bandwidth": "0 B/s",
		}
	}

	totalRequests := utils.SafeInt64(stats["total_requests"])
	totalErrors := utils.SafeInt64(stats["total_errors"])
	totalBytes := utils.SafeInt64(stats["total_bytes"])
	uptimeSeconds := uptime.Seconds()

	// 处理延迟统计数据
	latencyStats := make(map[string]interface{})
	if stats["latency_stats"] != nil {
		latencyStats = stats["latency_stats"].(map[string]interface{})
	}

	// 处理带宽历史数据
	bandwidthHistory := make(map[string]string)
	if stats["bandwidth_history"] != nil {
		for k, v := range stats["bandwidth_history"].(map[string]string) {
			bandwidthHistory[k] = v
		}
	}

	// 计算客户端错误和服务器错误数量
	var clientErrors, serverErrors int64
	statusCodeStats := models.SafeStatusCodeStats(stats["status_code_stats"])
	for code, count := range statusCodeStats {
		codeInt := utils.ParseInt(code, 0)
		if codeInt >= 400 && codeInt < 500 {
			clientErrors += count
		} else if codeInt >= 500 {
			serverErrors += count
		}
	}

	// 创建错误类型统计
	errorTypes := make(map[string]int64)
	if clientErrors > 0 {
		errorTypes["客户端错误"] = clientErrors
	}
	if serverErrors > 0 {
		errorTypes["服务器错误"] = serverErrors
	}

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
		StatusCodeStats:     statusCodeStats,
		TopPaths:            models.SafePathMetrics(stats["top_paths"]),
		RecentRequests:      models.SafeRequestLogs(stats["recent_requests"]),
		TopReferers:         models.SafePathMetrics(stats["top_referers"]),
		BandwidthHistory:    bandwidthHistory,
		CurrentBandwidth:    utils.SafeString(stats["current_bandwidth"], "0 B/s"),
	}

	// 填充延迟统计数据
	metrics.LatencyStats.Min = utils.SafeString(latencyStats["min"], "0ms")
	metrics.LatencyStats.Max = utils.SafeString(latencyStats["max"], "0ms")

	// 处理分布数据
	if distribution, ok := latencyStats["distribution"].(map[string]interface{}); ok {
		metrics.LatencyStats.Distribution = make(map[string]int64)
		for k, v := range distribution {
			if intValue, ok := v.(float64); ok {
				metrics.LatencyStats.Distribution[k] = int64(intValue)
			}
		}
	}

	// 填充错误统计数据
	metrics.ErrorStats.ClientErrors = clientErrors
	metrics.ErrorStats.ServerErrors = serverErrors
	metrics.ErrorStats.Types = errorTypes

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		log.Printf("Error encoding metrics: %v", err)
	}
}
