package service

import (
	"errors"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
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


// GetCacheStats 获取缓存统计信息
func (s *CacheService) GetCacheStats() map[string]cache.CacheStats {
	return map[string]cache.CacheStats{
		"proxy":  s.proxyCache.GetStats(),
		"mirror": s.mirrorCache.GetStats(),
	}
}

// GetCacheConfig 获取缓存配置
func (s *CacheService) GetCacheConfig() map[string]config.CacheConfig {
	return map[string]config.CacheConfig{
		"proxy":  s.proxyCache.GetConfig(),
		"mirror": s.mirrorCache.GetConfig(),
	}
}

// UpdateCacheConfig 更新指定类型的缓存配置
func (s *CacheService) UpdateCacheConfig(cacheType string, config config.CacheConfig) error {
	var targetCache *cache.CacheManager
	
	switch cacheType {
	case "proxy":
		targetCache = s.proxyCache
	case "mirror":
		targetCache = s.mirrorCache
	default:
		return errors.New("invalid cache type")
	}

	return targetCache.UpdateConfig(&config)
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

// ClearCacheByPath 清空指定路径前缀的缓存
func (s *CacheService) ClearCacheByPath(cacheType string, pathPrefix string) (int, error) {
	switch cacheType {
	case "proxy":
		return s.proxyCache.ClearCacheByPrefix(pathPrefix)
	case "mirror":
		return s.mirrorCache.ClearCacheByPrefix(pathPrefix)
	case "all":
		proxyCount, err1 := s.proxyCache.ClearCacheByPrefix(pathPrefix)
		mirrorCount, err2 := s.mirrorCache.ClearCacheByPrefix(pathPrefix)

		// 如果任一缓存清理失败，返回错误
		if err1 != nil {
			return proxyCount, err1
		}
		if err2 != nil {
			return proxyCount + mirrorCount, err2
		}

		return proxyCount + mirrorCount, nil
	default:
		return 0, errors.New("invalid cache type")
	}
}