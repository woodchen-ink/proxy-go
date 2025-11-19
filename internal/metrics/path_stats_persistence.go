package metrics

import (
	"encoding/json"
	"log"
	"os"
	"proxy-go/internal/models"
	"sync"
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

// PathStatsStorage 路径统计存储管理器
type PathStatsStorage struct {
	filePath string
	mu       sync.RWMutex
}

// NewPathStatsStorage 创建路径统计存储管理器
func NewPathStatsStorage(filePath string) *PathStatsStorage {
	return &PathStatsStorage{
		filePath: filePath,
	}
}

// Load 加载路径统计数据
func (pss *PathStatsStorage) Load() (*PathStatsPersistence, error) {
	pss.mu.RLock()
	defer pss.mu.RUnlock()

	// 检查文件是否存在
	if _, err := os.Stat(pss.filePath); os.IsNotExist(err) {
		// 文件不存在，返回空数据
		return &PathStatsPersistence{
			PathStats:  make(map[string]models.PathMetricsJSON),
			LastUpdate: time.Now(),
			Version:    "1.0",
		}, nil
	}

	// 读取文件
	data, err := os.ReadFile(pss.filePath)
	if err != nil {
		return nil, err
	}

	// 解析JSON
	var persistence PathStatsPersistence
	if err := json.Unmarshal(data, &persistence); err != nil {
		return nil, err
	}

	// 确保map不为nil
	if persistence.PathStats == nil {
		persistence.PathStats = make(map[string]models.PathMetricsJSON)
	}

	return &persistence, nil
}

// Save 保存路径统计数据
func (pss *PathStatsStorage) Save(persistence *PathStatsPersistence) error {
	pss.mu.Lock()
	defer pss.mu.Unlock()

	persistence.LastUpdate = time.Now()
	persistence.Version = "1.0"

	// 序列化为JSON
	data, err := json.MarshalIndent(persistence, "", "  ")
	if err != nil {
		return err
	}

	// 写入临时文件
	tempFile := pss.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	// 重命名为正式文件（原子操作）
	if err := os.Rename(tempFile, pss.filePath); err != nil {
		return err
	}

	log.Printf("[PathStatsStorage] 路径统计数据已保存: %d 条路径记录",
		len(persistence.PathStats))

	return nil
}

// SavePathStats 保存当前路径统计数据（从 Collector 调用）
func (pss *PathStatsStorage) SavePathStats(pathStats []models.PathMetricsJSON) error {
	// 转换为 map 格式
	statsMap := make(map[string]models.PathMetricsJSON)
	for _, stat := range pathStats {
		statsMap[stat.Path] = stat
	}

	persistence := &PathStatsPersistence{
		PathStats:  statsMap,
		LastUpdate: time.Now(),
		Version:    "1.0",
	}

	return pss.Save(persistence)
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
