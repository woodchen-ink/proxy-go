package metrics

import (
	"fmt"
	"log"
	"proxy-go/internal/models"
	"sort"
	"strings"
	"time"
)

// 逐路径累计统计: 对外读取、本地文件持久化与按前缀清零

// GetPathStats 获取所有路径的统计信息
func (c *Collector) GetPathStats() []models.PathMetricsJSON {
	var pathMetrics []*models.PathMetrics
	c.pathStats.Range(func(key string, value *models.PathMetrics) bool {
		stats := value
		requestCount := stats.GetRequestCount()
		if requestCount > 0 {
			totalLatency := stats.GetTotalLatency()
			avgLatencyMs := float64(totalLatency) / float64(requestCount) / float64(time.Millisecond)
			stats.AvgLatency = fmt.Sprintf("%.2fms", avgLatencyMs)
			pathMetrics = append(pathMetrics, stats)
		}
		return true
	})

	// 按请求数降序排序
	sort.Slice(pathMetrics, func(i, j int) bool {
		countI := pathMetrics[i].GetRequestCount()
		countJ := pathMetrics[j].GetRequestCount()
		if countI != countJ {
			return countI > countJ
		}
		return pathMetrics[i].Path < pathMetrics[j].Path
	})

	// 转换为 JSON 格式
	result := make([]models.PathMetricsJSON, len(pathMetrics))
	for i, metric := range pathMetrics {
		result[i] = metric.ToJSON()
	}

	return result
}

// loadPathStats 加载持久化的路径统计数据
func (c *Collector) loadPathStats() error {
	loadedStats, err := c.pathStatsStorage.LoadPathStats()
	if err != nil {
		return fmt.Errorf("failed to load path stats: %w", err)
	}

	if len(loadedStats) == 0 {
		log.Printf("[Collector] 没有找到持久化的路径统计数据，从空开始")
		return nil
	}

	// 将加载的数据恢复到内存中
	for _, stat := range loadedStats {
		pathMetrics := &models.PathMetrics{
			Path: stat.Path,
		}

		// 恢复计数器
		pathMetrics.RequestCount.Store(stat.RequestCount)
		pathMetrics.ErrorCount.Store(stat.ErrorCount)
		pathMetrics.BytesTransferred.Store(stat.BytesTransferred)
		pathMetrics.TotalLatency.Store(stat.TotalLatency)
		pathMetrics.LastAccessTime.Store(stat.LastAccessTime)

		// 恢复状态码统计
		pathMetrics.Status2xx.Store(stat.Status2xx)
		pathMetrics.Status3xx.Store(stat.Status3xx)
		pathMetrics.Status4xx.Store(stat.Status4xx)
		pathMetrics.Status5xx.Store(stat.Status5xx)

		// 恢复缓存统计
		pathMetrics.CacheHits.Store(stat.CacheHits)
		pathMetrics.CacheMisses.Store(stat.CacheMisses)
		pathMetrics.BytesSaved.Store(stat.BytesSaved)

		// 存储到 pathStats
		c.pathStats.Store(stat.Path, pathMetrics)
	}

	log.Printf("[Collector] 已加载 %d 条路径统计数据", len(loadedStats))
	return nil
}

// savePathStats 保存路径统计数据到持久化存储
func (c *Collector) savePathStats() error {
	stats := c.GetPathStats()
	if err := c.pathStatsStorage.SavePathStats(stats); err != nil {
		return fmt.Errorf("failed to save path stats: %w", err)
	}
	return nil
}

// startPersistenceTask 启动定期持久化任务
func (c *Collector) startPersistenceTask() {
	c.stopWG.Add(1)
	go func() {
		defer c.stopWG.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Metrics] persistence task panic: %v", r)
			}
		}()
		// 每5分钟持久化一次路径统计数据
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := c.savePathStats(); err != nil {
					log.Printf("[Collector] 定期持久化路径统计失败: %v", err)
				}
			case <-c.stopChan:
				// 退出前最后保存一次
				if err := c.savePathStats(); err != nil {
					log.Printf("[Collector] 关闭时持久化路径统计失败: %v", err)
				}
				return
			}
		}
	}()
}

// zeroPathStats 清零内存中匹配前缀的路径计数, prefix 为空清全部; 返回清零条数
//
// 只动内存, 不碰 D1 —— 集群侧动作 (marker / 删行) 由调用方决定, 这样发起端与
// 心跳应用端能共用同一份清零逻辑
func (c *Collector) zeroPathStats(prefix string) int {
	count := 0
	c.pathStats.Range(func(key string, stats *models.PathMetrics) bool {
		if prefix != "" {
			// 确保匹配的是完整的路径段（避免 /abc 匹配到 /abcd）
			if !strings.HasPrefix(key, prefix) {
				return true
			}
			if len(key) != len(prefix) && key[len(prefix)] != '/' {
				return true
			}
		}
		stats.RequestCount.Store(0)
		stats.ErrorCount.Store(0)
		stats.BytesTransferred.Store(0)
		stats.TotalLatency.Store(0)
		stats.Status2xx.Store(0)
		stats.Status3xx.Store(0)
		stats.Status4xx.Store(0)
		stats.Status5xx.Store(0)
		stats.CacheHits.Store(0)
		stats.CacheMisses.Store(0)
		stats.BytesSaved.Store(0)
		stats.LastAccessTime.Store(time.Now().Unix())
		count++
		return true
	})
	return count
}
