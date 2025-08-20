package service

import (
	"log"
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
	AverageResponseTime    string  `json:"avg_response_time"`
	RequestsPerSecond      float64 `json:"requests_per_second"`
	CurrentSessionRequests int64   `json:"current_session_requests"`

	// 传输指标
	TotalBytes     int64   `json:"total_bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`

	// 状态码统计
	StatusCodeStats map[string]int64 `json:"status_code_stats"`

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

type MetricsService struct {
	startTime time.Time
}

func NewMetricsService(startTime time.Time) *MetricsService {
	return &MetricsService{
		startTime: startTime,
	}
}

// GetMetrics 获取系统指标数据
func (s *MetricsService) GetMetrics() *Metrics {
	uptime := time.Since(s.startTime)
	collector := metrics.GetCollector()
	stats := collector.GetStats()

	// 处理空stats的情况
	if stats == nil {
		stats = s.getDefaultStats(uptime)
	}

	totalRequests := utils.SafeInt64(stats["total_requests"])
	totalErrors := utils.SafeInt64(stats["total_errors"])
	totalBytes := utils.SafeInt64(stats["total_bytes"])
	uptimeSeconds := uptime.Seconds()

	// 构建指标数据
	metricsData := &Metrics{
		Uptime:                 metrics.FormatUptime(uptime),
		ActiveRequests:         utils.SafeInt64(stats["active_requests"]),
		TotalRequests:          totalRequests,
		TotalErrors:            totalErrors,
		ErrorRate:              float64(totalErrors) / float64(utils.Max(totalRequests, 1)),
		NumGoroutine:           utils.SafeInt(stats["num_goroutine"]),
		MemoryUsage:            utils.SafeString(stats["memory_usage"], "0 B"),
		AverageResponseTime:    utils.SafeString(stats["avg_response_time"], "0 ms"),
		TotalBytes:             totalBytes,
		BytesPerSecond:         float64(totalBytes) / utils.MaxFloat64(uptimeSeconds, 1),
		RequestsPerSecond:      utils.SafeFloat64(stats["requests_per_second"]),
		CurrentSessionRequests: utils.SafeInt64(stats["current_session_requests"]),
		StatusCodeStats:        models.SafeStatusCodeStats(stats["status_code_stats"]),
		RecentRequests:         models.SafeRequestLogs(stats["recent_requests"]),
		TopReferers:            models.SafePathMetrics(stats["top_referers"]),
		BandwidthHistory:       s.processBandwidthHistory(stats["bandwidth_history"]),
		CurrentBandwidth:       utils.SafeString(stats["current_bandwidth"], "0 B/s"),
	}

	// 处理延迟统计数据
	s.processLatencyStats(metricsData, stats)

	return metricsData
}

// getDefaultStats 获取默认的统计数据
func (s *MetricsService) getDefaultStats(uptime time.Duration) map[string]interface{} {
	return map[string]interface{}{
		"uptime":                   metrics.FormatUptime(uptime),
		"active_requests":          int64(0),
		"total_requests":           int64(0),
		"total_errors":             int64(0),
		"error_rate":               float64(0),
		"num_goroutine":            runtime.NumGoroutine(),
		"memory_usage":             "0 B",
		"avg_response_time":        "0 ms",
		"total_bytes":              int64(0),
		"bytes_per_second":         float64(0),
		"requests_per_second":      float64(0),
		"current_session_requests": int64(0),
		"status_code_stats":        make(map[string]int64),
		"recent_requests":          make([]models.RequestLog, 0),
		"top_referers":             make([]models.PathMetrics, 0),
		"latency_stats": map[string]interface{}{
			"min":          "0ms",
			"max":          "0ms",
			"distribution": make(map[string]int64),
		},
		"bandwidth_history": make(map[string]string),
		"current_bandwidth": "0 B/s",
	}
}

// processBandwidthHistory 处理带宽历史数据
func (s *MetricsService) processBandwidthHistory(data interface{}) map[string]string {
	bandwidthHistory := make(map[string]string)
	if data != nil {
		if historyMap, ok := data.(map[string]string); ok {
			for k, v := range historyMap {
				bandwidthHistory[k] = v
			}
		}
	}
	return bandwidthHistory
}

// processLatencyStats 处理延迟统计数据
func (s *MetricsService) processLatencyStats(metricsData *Metrics, stats map[string]interface{}) {
	latencyStats := make(map[string]interface{})
	if stats["latency_stats"] != nil {
		latencyStats = stats["latency_stats"].(map[string]interface{})
	}

	metricsData.LatencyStats.Min = utils.SafeString(latencyStats["min"], "0ms")
	metricsData.LatencyStats.Max = utils.SafeString(latencyStats["max"], "0ms")

	// 处理分布数据
	if stats["latency_stats"] != nil {
		latencyStatsMap, ok := stats["latency_stats"].(map[string]interface{})
		if ok {
			distribution, ok := latencyStatsMap["distribution"]
			if ok && distribution != nil {
				metricsData.LatencyStats.Distribution = s.processLatencyDistribution(distribution)
			}
		}
	}

	// 如果分布数据为空，初始化默认分布
	if metricsData.LatencyStats.Distribution == nil {
		metricsData.LatencyStats.Distribution = s.getDefaultLatencyDistribution()
	}
}

// processLatencyDistribution 处理延迟分布数据
func (s *MetricsService) processLatencyDistribution(distribution interface{}) map[string]int64 {
	// 尝试直接使用 map[string]int64 类型
	if distributionMap, ok := distribution.(map[string]int64); ok {
		return distributionMap
	}
	
	// 如果不是 map[string]int64，尝试转换 map[string]interface{}
	if distributionMap, ok := distribution.(map[string]interface{}); ok {
		result := make(map[string]int64)
		for k, v := range distributionMap {
			if intValue, ok := v.(float64); ok {
				result[k] = int64(intValue)
			} else if intValue, ok := v.(int64); ok {
				result[k] = intValue
			}
		}
		return result
	}
	
	log.Printf("[MetricsService] distribution类型未知: %T", distribution)
	return s.getDefaultLatencyDistribution()
}

// getDefaultLatencyDistribution 获取默认的延迟分布
func (s *MetricsService) getDefaultLatencyDistribution() map[string]int64 {
	return map[string]int64{
		"lt10ms":      0,
		"10-50ms":     0,
		"50-200ms":    0,
		"200-1000ms":  0,
		"gt1s":        0,
	}
}