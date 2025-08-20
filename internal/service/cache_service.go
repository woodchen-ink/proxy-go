package service

import (
	"errors"
	"proxy-go/internal/cache"
)

type CacheService struct {
	proxyCache  *cache.CacheManager
	mirrorCache *cache.CacheManager
}

func NewCacheService(proxyCache, mirrorCache *cache.CacheManager) *CacheService {
	return &CacheService{
		proxyCache:  proxyCache,
		mirrorCache: mirrorCache,
	}
}

// CacheConfig 缓存配置结构
type CacheConfig struct {
	MaxAge       int64 `json:"max_age"`
	CleanupTick  int64 `json:"cleanup_tick"`
	MaxCacheSize int64 `json:"max_cache_size"`
}

// GetCacheStats 获取缓存统计信息
func (s *CacheService) GetCacheStats() map[string]cache.CacheStats {
	return map[string]cache.CacheStats{
		"proxy":  s.proxyCache.GetStats(),
		"mirror": s.mirrorCache.GetStats(),
	}
}

// GetCacheConfig 获取缓存配置
func (s *CacheService) GetCacheConfig() map[string]cache.CacheConfig {
	return map[string]cache.CacheConfig{
		"proxy":  s.proxyCache.GetConfig(),
		"mirror": s.mirrorCache.GetConfig(),
	}
}

// UpdateCacheConfig 更新指定类型的缓存配置
func (s *CacheService) UpdateCacheConfig(cacheType string, config CacheConfig) error {
	var targetCache *cache.CacheManager
	
	switch cacheType {
	case "proxy":
		targetCache = s.proxyCache
	case "mirror":
		targetCache = s.mirrorCache
	default:
		return errors.New("invalid cache type")
	}

	return targetCache.UpdateConfig(config.MaxAge, config.CleanupTick, config.MaxCacheSize)
}

// SetCacheEnabled 设置缓存开关状态
func (s *CacheService) SetCacheEnabled(cacheType string, enabled bool) error {
	switch cacheType {
	case "proxy":
		s.proxyCache.SetEnabled(enabled)
	case "mirror":
		s.mirrorCache.SetEnabled(enabled)
	default:
		return errors.New("invalid cache type")
	}
	
	return nil
}

// ClearCache 清空指定类型的缓存
func (s *CacheService) ClearCache(cacheType string) error {
	switch cacheType {
	case "proxy":
		return s.proxyCache.ClearCache()
	case "mirror":
		return s.mirrorCache.ClearCache()
	case "all":
		if err := s.proxyCache.ClearCache(); err != nil {
			return err
		}
		return s.mirrorCache.ClearCache()
	default:
		return errors.New("invalid cache type")
	}
}