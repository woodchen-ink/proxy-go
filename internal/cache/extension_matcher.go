package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"proxy-go/internal/config"
	"proxy-go/internal/utils"
	"sync"
	"time"
)

// ExtensionMatcherCacheItem 扩展名匹配器缓存项
type ExtensionMatcherCacheItem struct {
	Matcher   *utils.ExtensionMatcher
	Hash      string // 配置的哈希值，用于检测配置变化
	CreatedAt time.Time
	LastUsed  time.Time
	UseCount  int64
}

// ExtensionMatcherCache 扩展名匹配器缓存管理器
type ExtensionMatcherCache struct {
	cache       sync.Map
	maxAge      time.Duration
	cleanupTick time.Duration
	stopCleanup chan struct{}
	mu          sync.RWMutex
}

// NewExtensionMatcherCache 创建新的扩展名匹配器缓存
func NewExtensionMatcherCache() *ExtensionMatcherCache {
	emc := &ExtensionMatcherCache{
		maxAge:      10 * time.Minute, // 缓存10分钟
		cleanupTick: 2 * time.Minute,  // 每2分钟清理一次
		stopCleanup: make(chan struct{}),
	}

	// 启动清理协程
	go emc.startCleanup()

	return emc
}

// generateConfigHash 生成配置的哈希值
func (emc *ExtensionMatcherCache) generateConfigHash(rules []config.ExtensionRule) string {
	// 将规则序列化为JSON
	data, err := json.Marshal(rules)
	if err != nil {
		// 如果序列化失败，使用时间戳作为哈希
		return hex.EncodeToString([]byte(time.Now().String()))
	}

	// 计算SHA256哈希
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// GetOrCreate 获取或创建扩展名匹配器
func (emc *ExtensionMatcherCache) GetOrCreate(pathKey string, rules []config.ExtensionRule) *utils.ExtensionMatcher {
	// 如果没有规则，直接创建新的匹配器
	if len(rules) == 0 {
		return utils.NewExtensionMatcher(rules)
	}

	// 生成配置哈希
	configHash := emc.generateConfigHash(rules)

	// 尝试从缓存获取
	if value, ok := emc.cache.Load(pathKey); ok {
		item := value.(*ExtensionMatcherCacheItem)

		// 检查配置是否变化
		if item.Hash == configHash {
			// 配置未变化，更新使用信息
			emc.mu.Lock()
			item.LastUsed = time.Now()
			item.UseCount++
			emc.mu.Unlock()

			log.Printf("[ExtensionMatcherCache] HIT %s (使用次数: %d)", pathKey, item.UseCount)
			return item.Matcher
		} else {
			// 配置已变化，删除旧缓存
			emc.cache.Delete(pathKey)
			log.Printf("[ExtensionMatcherCache] CONFIG_CHANGED %s", pathKey)
		}
	}

	// 创建新的匹配器
	matcher := utils.NewExtensionMatcher(rules)

	// 创建缓存项
	item := &ExtensionMatcherCacheItem{
		Matcher:   matcher,
		Hash:      configHash,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
		UseCount:  1,
	}

	// 存储到缓存
	emc.cache.Store(pathKey, item)
	log.Printf("[ExtensionMatcherCache] NEW %s (规则数量: %d)", pathKey, len(rules))

	return matcher
}

// InvalidatePath 使指定路径的缓存失效
func (emc *ExtensionMatcherCache) InvalidatePath(pathKey string) {
	if _, ok := emc.cache.LoadAndDelete(pathKey); ok {
		log.Printf("[ExtensionMatcherCache] INVALIDATED %s", pathKey)
	}
}

// InvalidateAll 清空所有缓存
func (emc *ExtensionMatcherCache) InvalidateAll() {
	count := 0
	emc.cache.Range(func(key, value interface{}) bool {
		emc.cache.Delete(key)
		count++
		return true
	})
	log.Printf("[ExtensionMatcherCache] INVALIDATED_ALL (清理了 %d 个缓存项)", count)
}

// GetStats 获取缓存统计信息
func (emc *ExtensionMatcherCache) GetStats() ExtensionMatcherCacheStats {
	stats := ExtensionMatcherCacheStats{
		MaxAge:      int64(emc.maxAge.Minutes()),
		CleanupTick: int64(emc.cleanupTick.Minutes()),
	}

	emc.cache.Range(func(key, value interface{}) bool {
		item := value.(*ExtensionMatcherCacheItem)
		stats.TotalItems++
		stats.TotalUseCount += item.UseCount

		// 计算平均年龄
		age := time.Since(item.CreatedAt)
		stats.AverageAge += int64(age.Minutes())

		return true
	})

	if stats.TotalItems > 0 {
		stats.AverageAge /= int64(stats.TotalItems)
	}

	return stats
}

// ExtensionMatcherCacheStats 扩展名匹配器缓存统计信息
type ExtensionMatcherCacheStats struct {
	TotalItems    int   `json:"total_items"`     // 缓存项数量
	TotalUseCount int64 `json:"total_use_count"` // 总使用次数
	AverageAge    int64 `json:"average_age"`     // 平均年龄（分钟）
	MaxAge        int64 `json:"max_age"`         // 最大缓存时间（分钟）
	CleanupTick   int64 `json:"cleanup_tick"`    // 清理间隔（分钟）
}

// startCleanup 启动清理协程
func (emc *ExtensionMatcherCache) startCleanup() {
	ticker := time.NewTicker(emc.cleanupTick)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			emc.cleanup()
		case <-emc.stopCleanup:
			return
		}
	}
}

// cleanup 清理过期的缓存项
func (emc *ExtensionMatcherCache) cleanup() {
	now := time.Now()
	expiredKeys := make([]interface{}, 0)

	// 收集过期的键
	emc.cache.Range(func(key, value interface{}) bool {
		item := value.(*ExtensionMatcherCacheItem)
		if now.Sub(item.LastUsed) > emc.maxAge {
			expiredKeys = append(expiredKeys, key)
		}
		return true
	})

	// 删除过期的缓存项
	for _, key := range expiredKeys {
		emc.cache.Delete(key)
	}

	if len(expiredKeys) > 0 {
		log.Printf("[ExtensionMatcherCache] CLEANUP 清理了 %d 个过期缓存项", len(expiredKeys))
	}
}

// Stop 停止缓存管理器
func (emc *ExtensionMatcherCache) Stop() {
	close(emc.stopCleanup)
}

// UpdateConfig 更新缓存配置
func (emc *ExtensionMatcherCache) UpdateConfig(maxAge, cleanupTick time.Duration) {
	emc.mu.Lock()
	defer emc.mu.Unlock()

	emc.maxAge = maxAge
	emc.cleanupTick = cleanupTick

	log.Printf("[ExtensionMatcherCache] CONFIG_UPDATED maxAge=%v cleanupTick=%v", maxAge, cleanupTick)
}
