package metrics

import (
	"context"
	"log"
	"proxy-go/internal/models"
	"proxy-go/pkg/sync"
	gosync "sync"
	"time"
)

// PathStatsPersistence 路径统计持久化数据结构
type PathStatsPersistence struct {
	// 路径统计数据
	PathStats map[string]models.PathMetricsJSON `json:"path_stats"`
	// 最后更新时间
	LastUpdate time.Time `json:"last_update"`
	// 数据版本（用于未来扩展）
	Version string `json:"version"`
}

// PathStatsStorage 路径统计存储管理器（D1 模式）
type PathStatsStorage struct {
	mu gosync.RWMutex
}

// NewPathStatsStorage 创建路径统计存储管理器
func NewPathStatsStorage(filePath string) *PathStatsStorage {
	// filePath 参数保留为了向后兼容，但不再使用
	return &PathStatsStorage{}
}

// Load 加载路径统计数据（从 D1）
func (pss *PathStatsStorage) Load() (*PathStatsPersistence, error) {
	pss.mu.RLock()
	defer pss.mu.RUnlock()

	// 如果 D1 未启用，返回空数据
	if !sync.IsEnabled() {
		return &PathStatsPersistence{
			PathStats:  make(map[string]models.PathMetricsJSON),
			LastUpdate: time.Now(),
			Version:    "1.0",
		}, nil
	}

	// 从 D1 加载数据
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pathStats, err := sync.LoadPathStats(ctx)
	if err != nil {
		log.Printf("[PathStatsStorage] 从 D1 加载失败: %v", err)
		return &PathStatsPersistence{
			PathStats:  make(map[string]models.PathMetricsJSON),
			LastUpdate: time.Now(),
			Version:    "1.0",
		}, nil
	}

	// 转换为 map 格式
	statsMap := make(map[string]models.PathMetricsJSON)
	for _, stat := range pathStats {
		statsMap[stat.Path] = models.PathMetricsJSON{
			Path:              stat.Path,
			RequestCount:      stat.RequestCount,
			ErrorCount:        stat.ErrorCount,
			BytesTransferred:  stat.BytesTransferred,
			Status2xx:         stat.Status2xx,
			Status3xx:         stat.Status3xx,
			Status4xx:         stat.Status4xx,
			Status5xx:         stat.Status5xx,
			CacheHits:         stat.CacheHits,
			CacheMisses:       stat.CacheMisses,
			CacheHitRate:      stat.CacheHitRate,
			BytesSaved:        stat.BytesSaved,
			AvgLatency:        stat.AvgLatency,
			LastAccessTime:    stat.LastAccessTime,
		}
	}

	log.Printf("[PathStatsStorage] 从 D1 加载了 %d 条路径统计", len(statsMap))

	return &PathStatsPersistence{
		PathStats:  statsMap,
		LastUpdate: time.Now(),
		Version:    "1.0",
	}, nil
}

// Save 保存路径统计数据（废弃 - 现在通过 D1Manager.SyncNow 保存）
func (pss *PathStatsStorage) Save(persistence *PathStatsPersistence) error {
	// 不再保存到本地文件，数据通过 D1 sync 保存
	// 保留方法为了向后兼容
	return nil
}

// SavePathStats 保存路径统计数据（数组格式）（废弃）
func (pss *PathStatsStorage) SavePathStats(pathStats []models.PathMetricsJSON) error {
	// 不再保存到本地文件
	return nil
}

// LoadPathStats 加载路径统计数据（返回数组格式）
func (pss *PathStatsStorage) LoadPathStats() ([]models.PathMetricsJSON, error) {
	persistence, err := pss.Load()
	if err != nil {
		return nil, err
	}

	// 转换为数组格式
	result := make([]models.PathMetricsJSON, 0, len(persistence.PathStats))
	for _, stat := range persistence.PathStats {
		result = append(result, stat)
	}

	return result, nil
}

// MergePathStats 合并路径统计数据（用于从持久化存储恢复时合并数据）
func MergePathStats(current, loaded []models.PathMetricsJSON) []models.PathMetricsJSON {
	// 使用 map 进行合并
	merged := make(map[string]models.PathMetricsJSON)

	// 先加载已有数据
	for _, stat := range loaded {
		merged[stat.Path] = stat
	}

	// 合并当前数据（累加）
	for _, stat := range current {
		if existing, ok := merged[stat.Path]; ok {
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

			// 更新最后访问时间（取最新的）
			if stat.LastAccessTime > existing.LastAccessTime {
				existing.LastAccessTime = stat.LastAccessTime
			}

			// 重新计算平均延迟和缓存命中率
			if existing.RequestCount > 0 {
				existing.AvgLatency = stat.AvgLatency // 使用最新的平均延迟
			}

			totalCacheRequests := existing.CacheHits + existing.CacheMisses
			if totalCacheRequests > 0 {
				existing.CacheHitRate = float64(existing.CacheHits) / float64(totalCacheRequests)
			}

			merged[stat.Path] = existing
		} else {
			// 新路径，直接添加
			merged[stat.Path] = stat
		}
	}

	// 转换为数组
	result := make([]models.PathMetricsJSON, 0, len(merged))
	for _, stat := range merged {
		result = append(result, stat)
	}

	return result
}
