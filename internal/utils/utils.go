package utils

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"proxy-go/internal/config"
	"sort"
	"strings"
	"sync"
	"time"
)

// 文件大小缓存项
type fileSizeCache struct {
	size      int64
	timestamp time.Time
}

var (
	// 文件大小缓存，过期时间5分钟
	sizeCache    sync.Map
	cacheTTL     = 5 * time.Minute
	maxCacheSize = 10000 // 最大缓存条目数
)

// 清理过期缓存
func init() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			now := time.Now()
			var items []struct {
				key       interface{}
				timestamp time.Time
			}
			sizeCache.Range(func(key, value interface{}) bool {
				cache := value.(fileSizeCache)
				if now.Sub(cache.timestamp) > cacheTTL {
					sizeCache.Delete(key)
				} else {
					items = append(items, struct {
						key       interface{}
						timestamp time.Time
					}{key, cache.timestamp})
				}
				return true
			})
			if len(items) > maxCacheSize {
				sort.Slice(items, func(i, j int) bool {
					return items[i].timestamp.Before(items[j].timestamp)
				})
				for i := 0; i < len(items)/2; i++ {
					sizeCache.Delete(items[i].key)
				}
			}
		}
	}()
}

func GetClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// 获取请求来源
func GetRequestSource(r *http.Request) string {
	referer := r.Header.Get("Referer")
	if referer != "" {
		return fmt.Sprintf(" (from: %s)", referer)
	}
	return ""
}

func FormatBytes(bytes int64) string {
	const (
		MB = 1024 * 1024
		KB = 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d Bytes", bytes)
	}
}

// 判断是否是图片请求
func IsImageRequest(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	imageExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".avif": true,
	}
	return imageExts[ext]
}

// GetFileSize 发送HEAD请求获取文件大小
func GetFileSize(client *http.Client, url string) (int64, error) {
	// 先查缓存
	if cache, ok := sizeCache.Load(url); ok {
		cacheItem := cache.(fileSizeCache)
		if time.Since(cacheItem.timestamp) < cacheTTL {
			return cacheItem.size, nil
		}
		sizeCache.Delete(url)
	}

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0, err
	}

	// 设置超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// 缓存结果
	if resp.ContentLength > 0 {
		sizeCache.Store(url, fileSizeCache{
			size:      resp.ContentLength,
			timestamp: time.Now(),
		})
	}

	return resp.ContentLength, nil
}

// GetTargetURL 根据路径和配置决定目标URL
func GetTargetURL(client *http.Client, r *http.Request, pathConfig config.PathConfig, path string) string {
	// 默认使用默认目标
	targetBase := pathConfig.DefaultTarget

	// 如果没有设置阈值，使用默认值 200KB
	threshold := pathConfig.SizeThreshold
	if threshold <= 0 {
		threshold = 200 * 1024
	}

	// 检查文件扩展名
	if pathConfig.ExtensionMap != nil {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			ext = ext[1:] // 移除开头的点
			// 先检查是否在扩展名映射中
			if altTarget, exists := pathConfig.GetExtensionTarget(ext); exists {
				// 检查文件大小
				contentLength := r.ContentLength
				if contentLength <= 0 {
					// 如果无法获取 Content-Length，尝试发送 HEAD 请求
					if size, err := GetFileSize(client, pathConfig.DefaultTarget+path); err == nil {
						contentLength = size
						log.Printf("[FileSize] Path: %s, Size: %s (from %s)",
							path, FormatBytes(contentLength),
							func() string {
								if isCacheHit(pathConfig.DefaultTarget + path) {
									return "cache"
								}
								return "HEAD request"
							}())
					} else {
						log.Printf("[FileSize] Failed to get size for %s: %v", path, err)
					}
				} else {
					log.Printf("[FileSize] Path: %s, Size: %s (from Content-Length)",
						path, FormatBytes(contentLength))
				}

				// 只有当文件大于阈值时才使用扩展名映射的目标
				if contentLength > threshold {
					log.Printf("[Route] %s -> %s (size: %s > %s)",
						path, altTarget, FormatBytes(contentLength), FormatBytes(threshold))
					targetBase = altTarget
				} else {
					log.Printf("[Route] %s -> %s (size: %s <= %s)",
						path, targetBase, FormatBytes(contentLength), FormatBytes(threshold))
				}
			}
		}
	}

	return targetBase
}

// 检查是否命中缓存
func isCacheHit(url string) bool {
	if cache, ok := sizeCache.Load(url); ok {
		return time.Since(cache.(fileSizeCache).timestamp) < cacheTTL
	}
	return false
}
