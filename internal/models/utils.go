package models

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
