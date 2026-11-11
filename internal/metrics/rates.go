package metrics

import (
	"proxy-go/internal/utils"
	"time"
)

// 滑动窗口速率: 当前带宽 / 带宽历史 / 近 5 分钟 RPS

// updateBandwidthStats 更新带宽统计
func (c *Collector) updateBandwidthStats(bytes int64) {
	c.bandwidthStats.Lock()
	defer c.bandwidthStats.Unlock()

	now := time.Now()
	if now.Sub(c.bandwidthStats.lastUpdate) >= c.bandwidthStats.window {
		// 保存当前时间窗口的数据
		key := c.bandwidthStats.lastUpdate.Format(bandwidthHistoryKeyFmt)
		c.bandwidthStats.history[key] = c.bandwidthStats.current

		// 清理旧数据（保留最近5个时间窗口）— 利用字典序找最小 key
		if len(c.bandwidthStats.history) > 5 {
			var oldestKey string
			for k := range c.bandwidthStats.history {
				if oldestKey == "" || k < oldestKey {
					oldestKey = k
				}
			}
			delete(c.bandwidthStats.history, oldestKey)
		}

		// 重置当前窗口
		c.bandwidthStats.current = bytes
		c.bandwidthStats.lastUpdate = now
	} else {
		c.bandwidthStats.current += bytes
	}
}

// getCurrentBandwidth 获取当前带宽
func (c *Collector) getCurrentBandwidth() float64 {
	c.bandwidthStats.RLock()
	defer c.bandwidthStats.RUnlock()

	now := time.Now()
	duration := now.Sub(c.bandwidthStats.lastUpdate).Seconds()
	if duration == 0 {
		return 0
	}
	return float64(c.bandwidthStats.current) / duration
}

// updateRequestsWindow 更新请求窗口统计
func (c *Collector) updateRequestsWindow(count int64) {
	c.requestsWindow.Lock()
	defer c.requestsWindow.Unlock()

	now := time.Now()

	// 如果是第一次调用，初始化时间
	if c.requestsWindow.lastUpdate.IsZero() {
		c.requestsWindow.lastUpdate = now
	}

	// 计算当前时间桶的索引
	timeSinceLastUpdate := now.Sub(c.requestsWindow.lastUpdate)

	// 如果时间跨度超过桶大小，需要移动到新桶
	if timeSinceLastUpdate >= c.requestsWindow.bucketSize {
		bucketsToMove := int(timeSinceLastUpdate / c.requestsWindow.bucketSize)

		if bucketsToMove >= len(c.requestsWindow.buckets) {
			// 如果移动的桶数超过总桶数，清空所有桶
			for i := range c.requestsWindow.buckets {
				c.requestsWindow.buckets[i] = 0
			}
		} else {
			// 向右移动桶数据（新数据在索引0）
			copy(c.requestsWindow.buckets[bucketsToMove:], c.requestsWindow.buckets[:len(c.requestsWindow.buckets)-bucketsToMove])
			// 清空前面的桶
			for i := 0; i < bucketsToMove; i++ {
				c.requestsWindow.buckets[i] = 0
			}
		}

		// 更新时间为当前桶的开始时间
		c.requestsWindow.lastUpdate = now.Truncate(c.requestsWindow.bucketSize)
	}

	// 将请求数加到第一个桶（当前时间桶）
	if len(c.requestsWindow.buckets) > 0 {
		c.requestsWindow.buckets[0] += count
	}
}

// getRecentRequestsPerSecond 获取最近5分钟的平均每秒请求数
func (c *Collector) getRecentRequestsPerSecond() float64 {
	c.requestsWindow.RLock()
	defer c.requestsWindow.RUnlock()

	// 统计所有桶的总请求数
	var totalRequests int64
	for _, bucket := range c.requestsWindow.buckets {
		totalRequests += bucket
	}

	// 计算实际的时间窗口（可能不满5分钟）
	now := time.Now()
	actualWindow := c.requestsWindow.window

	// 如果程序运行时间不足5分钟，使用实际运行时间
	if runTime := now.Sub(c.startTime); runTime < c.requestsWindow.window {
		actualWindow = runTime
	}

	if actualWindow.Seconds() == 0 {
		return 0
	}

	return float64(totalRequests) / actualWindow.Seconds()
}

// getBandwidthHistory 获取带宽历史记录
func (c *Collector) getBandwidthHistory() map[string]string {
	c.bandwidthStats.RLock()
	defer c.bandwidthStats.RUnlock()

	history := make(map[string]string)
	for k, v := range c.bandwidthStats.history {
		history[k] = utils.FormatBytes(v) + "/min"
	}
	return history
}
