package models

import (
	"sync/atomic"
)

// SafeStatusCodeStats 安全地将 interface{} 转换为状态码统计
func SafeStatusCodeStats(v interface{}) map[string]int64 {
	if v == nil {
		return make(map[string]int64)
	}
	if m, ok := v.(map[string]int64); ok {
		return m
	}
	return make(map[string]int64)
}

// SafePathMetrics 安全地将 interface{} 转换为路径指标
func SafePathMetrics(v interface{}) []PathMetrics {
	if v == nil {
		return []PathMetrics{}
	}
	if m, ok := v.([]PathMetrics); ok {
		return m
	}
	if m, ok := v.([]*PathMetrics); ok {
		result := make([]PathMetrics, len(m))
		for i, metric := range m {
			result[i] = PathMetrics{
				Path:             metric.Path,
				AvgLatency:       metric.AvgLatency,
				RequestCount:     atomic.Int64{},
				ErrorCount:       atomic.Int64{},
				TotalLatency:     atomic.Int64{},
				BytesTransferred: atomic.Int64{},
			}
			result[i].RequestCount.Store(metric.RequestCount.Load())
			result[i].ErrorCount.Store(metric.ErrorCount.Load())
			result[i].TotalLatency.Store(metric.TotalLatency.Load())
			result[i].BytesTransferred.Store(metric.BytesTransferred.Load())
		}
		return result
	}
	return []PathMetrics{}
}

// SafeRequestLogs 安全地将 interface{} 转换为请求日志
func SafeRequestLogs(v interface{}) []RequestLog {
	if v == nil {
		return []RequestLog{}
	}
	if m, ok := v.([]RequestLog); ok {
		return m
	}
	return []RequestLog{}
}
