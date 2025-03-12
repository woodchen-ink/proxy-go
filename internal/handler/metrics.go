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

	// 处理状态码统计数据
	statusCodeStats := models.SafeStatusCodeStats(stats["status_code_stats"])

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
	if stats["latency_stats"] != nil {
		latencyStatsMap, ok := stats["latency_stats"].(map[string]interface{})
		if ok {
			distribution, ok := latencyStatsMap["distribution"]
			if ok && distribution != nil {
				// 尝试直接使用 map[string]int64 类型
				if distributionMap, ok := distribution.(map[string]int64); ok {
					metrics.LatencyStats.Distribution = distributionMap
				} else if distributionMap, ok := distribution.(map[string]interface{}); ok {
					// 如果不是 map[string]int64，尝试转换 map[string]interface{}
					metrics.LatencyStats.Distribution = make(map[string]int64)
					for k, v := range distributionMap {
						if intValue, ok := v.(float64); ok {
							metrics.LatencyStats.Distribution[k] = int64(intValue)
						} else if intValue, ok := v.(int64); ok {
							metrics.LatencyStats.Distribution[k] = intValue
						}
					}
				} else {
					log.Printf("[MetricsHandler] distribution类型未知: %T", distribution)
				}
			}
		}
	}

	// 如果分布数据为空，初始化一个空的分布
	if metrics.LatencyStats.Distribution == nil {
		metrics.LatencyStats.Distribution = make(map[string]int64)
		// 添加默认的延迟桶
		metrics.LatencyStats.Distribution["lt10ms"] = 0
		metrics.LatencyStats.Distribution["10-50ms"] = 0
		metrics.LatencyStats.Distribution["50-200ms"] = 0
		metrics.LatencyStats.Distribution["200-1000ms"] = 0
		metrics.LatencyStats.Distribution["gt1s"] = 0
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		log.Printf("Error encoding metrics: %v", err)
	}
}
