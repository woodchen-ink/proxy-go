package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// 可访问性缓存项
type accessibilityCache struct {
	accessible bool
	timestamp  time.Time
}

var (
	// 文件大小缓存，过期时间5分钟
	sizeCache sync.Map
	// 可访问性缓存，过期时间30秒
	accessCache  sync.Map
	cacheTTL     = 5 * time.Minute
	accessTTL    = 30 * time.Second
	maxCacheSize = 10000 // 最大缓存条目数
)

// 清理过期缓存
func init() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			now := time.Now()
			// 清理文件大小缓存
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

			// 清理可访问性缓存
			accessCache.Range(func(key, value interface{}) bool {
				cache := value.(accessibilityCache)
				if now.Sub(cache.timestamp) > accessTTL {
					accessCache.Delete(key)
				}
				return true
			})
		}
	}()
}

// GenerateRequestID 生成唯一的请求ID
func GenerateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// 如果随机数生成失败，使用时间戳作为备选
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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

	// 如果配置了扩展名映射
	if pathConfig.ExtensionMap != nil {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			ext = ext[1:] // 移除开头的点
			// 检查是否在扩展名映射中
			if altTarget, exists := pathConfig.GetExtensionTarget(ext); exists {
				// 检查文件大小
				contentLength, err := GetFileSize(client, targetBase+path)
				if err != nil {
					log.Printf("[Route] %s -> %s (error getting size: %v)", path, targetBase, err)
					return targetBase
				}

				// 如果没有设置最小阈值，使用默认值 500KB
				minThreshold := pathConfig.SizeThreshold
				if minThreshold <= 0 {
					minThreshold = 500 * 1024
				}

				// 如果没有设置最大阈值，使用默认值 10MB
				maxThreshold := pathConfig.MaxSize
				if maxThreshold <= 0 {
					maxThreshold = 10 * 1024 * 1024
				}

				if contentLength > minThreshold && contentLength <= maxThreshold {
					// 创建一个带超时的 context
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()

					// 使用 channel 来接收备用源检查结果
					altChan := make(chan struct {
						accessible bool
						err        error
					}, 1)

					// 在 goroutine 中检查备用源可访问性
					go func() {
						accessible := isTargetAccessible(client, altTarget+path)
						select {
						case altChan <- struct {
							accessible bool
							err        error
						}{accessible: accessible}:
						case <-ctx.Done():
							// context 已取消，不需要发送结果
						}
					}()

					// 等待结果或超时
					select {
					case result := <-altChan:
						if result.accessible {
							log.Printf("[Route] %s -> %s (size: %s > %s and <= %s)",
								path, altTarget, FormatBytes(contentLength),
								FormatBytes(minThreshold), FormatBytes(maxThreshold))
							return altTarget
						}
						log.Printf("[Route] %s -> %s (fallback: alternative target not accessible)",
							path, targetBase)
					case <-ctx.Done():
						log.Printf("[Route] %s -> %s (fallback: alternative target check timeout)",
							path, targetBase)
					}
				} else if contentLength <= minThreshold {
					log.Printf("[Route] %s -> %s (size: %s <= %s)",
						path, targetBase, FormatBytes(contentLength), FormatBytes(minThreshold))
				} else {
					log.Printf("[Route] %s -> %s (size: %s > %s)",
						path, targetBase, FormatBytes(contentLength), FormatBytes(maxThreshold))
				}
			} else {
				log.Printf("[Route] %s -> %s (no extension mapping)", path, targetBase)
			}
		} else {
			log.Printf("[Route] %s -> %s (no extension)", path, targetBase)
		}
	} else {
		log.Printf("[Route] %s -> %s (no extension map)", path, targetBase)
	}

	return targetBase
}

// isTargetAccessible 检查目标URL是否可访问
func isTargetAccessible(client *http.Client, url string) bool {
	// 先查缓存
	if cache, ok := accessCache.Load(url); ok {
		cacheItem := cache.(accessibilityCache)
		if time.Since(cacheItem.timestamp) < accessTTL {
			return cacheItem.accessible
		}
		accessCache.Delete(url)
	}

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		log.Printf("[Check] Failed to create request for %s: %v", url, err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Check] Failed to access %s: %v", url, err)
		return false
	}
	defer resp.Body.Close()

	accessible := resp.StatusCode >= 200 && resp.StatusCode < 400
	// 缓存结果
	accessCache.Store(url, accessibilityCache{
		accessible: accessible,
		timestamp:  time.Now(),
	})

	return accessible
}

// SafeInt64 安全地将 interface{} 转换为 int64
func SafeInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	if i, ok := v.(int64); ok {
		return i
	}
	return 0
}

// SafeInt 安全地将 interface{} 转换为 int
func SafeInt(v interface{}) int {
	if v == nil {
		return 0
	}
	if i, ok := v.(int); ok {
		return i
	}
	return 0
}

// SafeString 安全地将 interface{} 转换为 string
func SafeString(v interface{}, defaultValue string) string {
	if v == nil {
		return defaultValue
	}
	if s, ok := v.(string); ok {
		return s
	}
	return defaultValue
}

// Max 返回两个 int64 中的较大值
func Max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// MaxFloat64 返回两个 float64 中的较大值
func MaxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
