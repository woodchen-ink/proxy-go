package service

import (
	"errors"
	"net/url"
	"proxy-go/internal/cache"
	"proxy-go/internal/config"
	"strings"
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

// ClearCacheByURLs 清空指定 URL 列表的缓存
func (s *CacheService) ClearCacheByURLs(cacheType string, urls []string) (int, error) {
	switch cacheType {
	case "proxy":
		return s.proxyCache.ClearCacheByURLs(urls)
	case "mirror":
		return s.mirrorCache.ClearCacheByURLs(urls)
	case "all":
		proxyCount, err1 := s.proxyCache.ClearCacheByURLs(urls)
		mirrorCount, err2 := s.mirrorCache.ClearCacheByURLs(urls)

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

// ClearCacheByURL 清空单个 URL 对应的缓存。
func (s *CacheService) ClearCacheByURL(cacheType string, url string) (int, error) {
	normalizedURL := normalizeSingleCacheURL(url)

	switch cacheType {
	case "proxy":
		return s.proxyCache.ClearCacheByURL(normalizedURL)
	case "mirror":
		return s.mirrorCache.ClearCacheByURL(normalizedURL)
	case "all":
		proxyCount, err1 := s.proxyCache.ClearCacheByURL(normalizedURL)
		mirrorCount, err2 := s.mirrorCache.ClearCacheByURL(normalizedURL)

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

// normalizeSingleCacheURL 将完整 URL 或路径统一为缓存清理使用的路径。
func normalizeSingleCacheURL(rawURL string) string {
	normalized := strings.TrimSpace(rawURL)
	if normalized == "" {
		return ""
	}

	if strings.HasPrefix(normalized, "/") {
		return normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Path == "" {
		return normalized
	}

	return parsed.Path
}
