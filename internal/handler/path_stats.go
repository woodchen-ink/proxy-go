package handler

import (
	"encoding/json"
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/metrics"
	"proxy-go/internal/models"
	"strings"
)

type PathStatsHandler struct {
	collector *metrics.Collector
}

func NewPathStatsHandler(collector *metrics.Collector) *PathStatsHandler {
	return &PathStatsHandler{
		collector: collector,
	}
}

// GetAllPathStats 获取所有路径的统计信息
// 将详细路径的统计数据聚合到配置的路径前缀下
func (h *PathStatsHandler) GetAllPathStats(w http.ResponseWriter, r *http.Request) {
	// 获取原始统计数据
	rawStats := h.collector.GetPathStats()

	// 获取配置的路径前缀
	cfg := config.GetConfig()
	if cfg == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path_stats": []models.PathMetricsJSON{},
		})
		return
	}

	// 创建聚合后的统计数据映射
	aggregatedStats := make(map[string]*models.PathMetricsJSON)

	// 遍历原始统计数据，找到匹配的配置路径前缀
	for _, stat := range rawStats {
		// 找到匹配的配置路径前缀
		matchedPrefix := findMatchingPrefix(stat.Path, cfg.MAP)

		if matchedPrefix == "" {
			// 如果没有匹配的前缀，使用原始路径
			matchedPrefix = stat.Path
		}

		// 聚合到对应的前缀下
		if existing, ok := aggregatedStats[matchedPrefix]; ok {
			// 累加统计数据
			existing.RequestCount += stat.RequestCount
			existing.ErrorCount += stat.ErrorCount
			existing.BytesTransferred += stat.BytesTransferred
			existing.Status2xx += stat.Status2xx
			existing.Status3xx += stat.Status3xx
			existing.Status4xx += stat.Status4xx
			existing.Status5xx += stat.Status5xx
			existing.CacheHits += stat.CacheHits
			existing.CacheMisses += stat.CacheMisses
			existing.BytesSaved += stat.BytesSaved

			// 更新平均延迟（加权平均）
			if existing.RequestCount > 0 {
				// 这里简化处理，使用最新的延迟值
				existing.AvgLatency = stat.AvgLatency
			}

			// 更新最后访问时间（取最新的）
			if stat.LastAccessTime > existing.LastAccessTime {
				existing.LastAccessTime = stat.LastAccessTime
			}

			// 重新计算缓存命中率
			totalCacheRequests := existing.CacheHits + existing.CacheMisses
			if totalCacheRequests > 0 {
				existing.CacheHitRate = float64(existing.CacheHits) / float64(totalCacheRequests)
			}
		} else {
			// 创建新的统计项
			statCopy := stat
			statCopy.Path = matchedPrefix
			aggregatedStats[matchedPrefix] = &statCopy
		}
	}

	// 转换为数组
	result := make([]models.PathMetricsJSON, 0, len(aggregatedStats))
	for _, stat := range aggregatedStats {
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path_stats": result,
	})
}

// findMatchingPrefix 查找请求路径匹配的配置路径前缀
func findMatchingPrefix(requestPath string, configMap map[string]config.PathConfig) string {
	var longestMatch string
	var longestLength int

	for configPath := range configMap {
		// 跳过根路径
		if configPath == "/" {
			continue
		}

		// 检查是否以配置路径为前缀
		if strings.HasPrefix(requestPath, configPath) {
			// 确保匹配的是完整的路径段（避免 /abc 匹配到 /abcd）
			if len(requestPath) == len(configPath) ||
			   (len(requestPath) > len(configPath) && requestPath[len(configPath)] == '/') {
				if len(configPath) > longestLength {
					longestMatch = configPath
					longestLength = len(configPath)
				}
			}
		}
	}

	return longestMatch
}
