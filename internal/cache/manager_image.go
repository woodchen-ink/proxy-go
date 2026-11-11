package cache

import (
	"log"
	"net/http"
	"strings"
)

// 图片缓存键: 按 Accept 格式 + 浏览器分桶, 命中失败时按 avif → webp → jpeg 顺序回退

// parseImageFormatPreference 解析图片格式偏好，返回标准化的格式标识
func (cm *CacheManager) parseImageFormatPreference(accept string) string {
	if accept == "" {
		return "image/jpeg" // 默认格式
	}

	accept = strings.ToLower(accept)

	// 按优先级检查现代图片格式
	switch {
	case strings.Contains(accept, "image/avif"):
		return "image/avif"
	case strings.Contains(accept, "image/webp"):
		return "image/webp"
	case strings.Contains(accept, "image/jpeg") || strings.Contains(accept, "image/jpg"):
		return "image/jpeg"
	case strings.Contains(accept, "image/png"):
		return "image/png"
	case strings.Contains(accept, "image/gif"):
		return "image/gif"
	case strings.Contains(accept, "image/*"):
		return "image/auto" // 自动格式
	default:
		return "image/jpeg" // 默认格式
	}
}

// normalizeUserAgent 标准化UserAgent，减少缓存键的变化
func (cm *CacheManager) normalizeUserAgent(ua string) string {
	if ua == "" {
		return "default"
	}

	ua = strings.ToLower(ua)

	// 根据主要浏览器类型进行分类
	switch {
	case strings.Contains(ua, "chrome") && !strings.Contains(ua, "edge"):
		return "chrome"
	case strings.Contains(ua, "firefox"):
		return "firefox"
	case strings.Contains(ua, "safari") && !strings.Contains(ua, "chrome"):
		return "safari"
	case strings.Contains(ua, "edge"):
		return "edge"
	case strings.Contains(ua, "bot") || strings.Contains(ua, "crawler"):
		return "bot"
	default:
		return "other"
	}
}

// getImageWithFallback 获取图片缓存项，支持格式回退
func (cm *CacheManager) getImageWithFallback(key CacheKey, r *http.Request) (*CacheItem, bool, bool) {
	// 首先尝试精确匹配
	if item, found, notModified := cm.getRegularItem(key); found {
		cm.imageCacheHit.Add(1)
		return item, found, notModified
	}

	// 如果精确匹配失败，尝试格式回退
	if item, found, notModified := cm.tryFormatFallback(key, r); found {
		cm.formatFallbackHit.Add(1)
		return item, found, notModified
	}

	return nil, false, false
}

// tryFormatFallback 尝试格式回退
func (cm *CacheManager) tryFormatFallback(originalKey CacheKey, r *http.Request) (*CacheItem, bool, bool) {
	requestedFormat := originalKey.AcceptHeaders

	// 定义格式回退顺序
	fallbackFormats := cm.getFormatFallbackOrder(requestedFormat)

	for _, format := range fallbackFormats {
		fallbackKey := CacheKey{
			URL:           originalKey.URL,
			AcceptHeaders: format,
			UserAgent:     originalKey.UserAgent,
		}

		if item, found, notModified := cm.getRegularItem(fallbackKey); found {
			// 找到了兼容格式，检查是否真的兼容
			if cm.isFormatCompatible(requestedFormat, format, item.ContentType) {
				log.Printf("[Cache] 格式回退: %s -> %s (%s)", requestedFormat, format, originalKey.URL)
				return item, found, notModified
			}
		}
	}

	return nil, false, false
}

// getFormatFallbackOrder 获取格式回退顺序
func (cm *CacheManager) getFormatFallbackOrder(requestedFormat string) []string {
	switch requestedFormat {
	case "image/avif":
		return []string{"image/webp", "image/jpeg", "image/png"}
	case "image/webp":
		return []string{"image/jpeg", "image/png", "image/avif"}
	case "image/jpeg":
		return []string{"image/webp", "image/png", "image/avif"}
	case "image/png":
		return []string{"image/webp", "image/jpeg", "image/avif"}
	case "image/auto":
		return []string{"image/webp", "image/avif", "image/jpeg", "image/png"}
	default:
		return []string{"image/jpeg", "image/webp", "image/png"}
	}
}

// isFormatCompatible 检查格式是否兼容
func (cm *CacheManager) isFormatCompatible(requestedFormat, cachedFormat, actualContentType string) bool {
	// 如果是自动格式，接受任何现代格式
	if requestedFormat == "image/auto" {
		return true
	}

	// 现代浏览器通常可以处理多种格式
	modernFormats := map[string]bool{
		"image/webp": true,
		"image/avif": true,
		"image/jpeg": true,
		"image/png":  true,
	}

	// 检查实际内容类型是否为现代格式
	if actualContentType != "" {
		return modernFormats[strings.ToLower(actualContentType)]
	}

	return modernFormats[cachedFormat]
}
