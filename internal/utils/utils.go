package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	neturl "net/url"
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
func GetTargetURL(client *http.Client, r *http.Request, pathConfig config.PathConfig, path string) (string, bool) {
	// 默认使用默认目标
	targetBase := pathConfig.DefaultTarget
	usedAltTarget := false

	// 获取文件扩展名（使用优化的字符串处理）
	ext := ""
	lastDotIndex := strings.LastIndex(path, ".")
	if lastDotIndex > 0 && lastDotIndex < len(path)-1 {
		ext = strings.ToLower(path[lastDotIndex+1:])
	}

	// 如果没有扩展名规则，直接返回默认目标
	if len(pathConfig.ExtRules) == 0 {
		if ext == "" {
			log.Printf("[Route] %s -> %s (无扩展名)", path, targetBase)
		}
		return targetBase, false
	}

	// 确保有扩展名规则
	if ext == "" {
		log.Printf("[Route] %s -> %s (无扩展名)", path, targetBase)
		// 即使没有扩展名，也要尝试匹配 * 通配符规则
	}

	// 获取文件大小
	contentLength, err := GetFileSize(client, targetBase+path)
	if err != nil {
		log.Printf("[Route] %s -> %s (获取文件大小出错: %v)", path, targetBase, err)

		// 如果无法获取文件大小，尝试使用扩展名直接匹配（优化点）
		if altTarget, exists := pathConfig.GetProcessedExtTarget(ext); exists {
			usedAltTarget = true
			targetBase = altTarget
			log.Printf("[Route] %s -> %s (基于扩展名直接匹配)", path, targetBase)
		} else if altTarget, exists := pathConfig.GetProcessedExtTarget("*"); exists {
			// 尝试使用通配符
			usedAltTarget = true
			targetBase = altTarget
			log.Printf("[Route] %s -> %s (基于通配符匹配)", path, targetBase)
		}

		return targetBase, usedAltTarget
	}

	// 获取匹配的扩展名规则
	matchingRules := []config.ExtensionRule{}
	wildcardRules := []config.ExtensionRule{} // 存储通配符规则

	// 找出所有匹配当前扩展名的规则
	for _, rule := range pathConfig.ExtRules {
		// 处理阈值默认值
		if rule.SizeThreshold < 0 {
			rule.SizeThreshold = 0 // 默认不限制
		}

		if rule.MaxSize <= 0 {
			rule.MaxSize = 1<<63 - 1 // 设置为最大值表示不限制
		}

		// 检查是否包含通配符
		for _, e := range rule.Extensions {
			if e == "*" {
				wildcardRules = append(wildcardRules, rule)
				break
			}
		}

		// 检查具体扩展名匹配
		for _, e := range rule.Extensions {
			if e == ext {
				matchingRules = append(matchingRules, rule)
				break
			}
		}
	}

	// 如果没有找到匹配的具体扩展名规则，使用通配符规则
	if len(matchingRules) == 0 {
		if len(wildcardRules) > 0 {
			log.Printf("[Route] %s -> 使用通配符规则", path)
			matchingRules = wildcardRules
		} else {
			return targetBase, false
		}
	}

	// 按阈值排序规则（优化点：使用更高效的排序）
	sort.Slice(matchingRules, func(i, j int) bool {
		if matchingRules[i].SizeThreshold == matchingRules[j].SizeThreshold {
			return matchingRules[i].MaxSize > matchingRules[j].MaxSize
		}
		return matchingRules[i].SizeThreshold < matchingRules[j].SizeThreshold
	})

	// 根据文件大小找出最匹配的规则
	for i := range matchingRules {
		rule := &matchingRules[i]

		// 检查文件大小是否在阈值范围内
		if contentLength >= rule.SizeThreshold && contentLength <= rule.MaxSize {
			// 找到匹配的规则
			log.Printf("[Route] %s -> %s (文件大小: %s, 在区间 %s 到 %s 之间)",
				path, rule.Target, FormatBytes(contentLength),
				FormatBytes(rule.SizeThreshold), FormatBytes(rule.MaxSize))

			// 检查目标是否可访问（使用带缓存的检查）
			if isTargetAccessible(client, rule.Target+path) {
				targetBase = rule.Target
				usedAltTarget = true
			} else {
				log.Printf("[Route] %s -> %s (回退: 备用目标不可访问)",
					path, targetBase)
			}

			break
		}
	}

	return targetBase, usedAltTarget
}

// isTargetAccessible 检查目标URL是否可访问
func isTargetAccessible(client *http.Client, targetURL string) bool {
	// 先查缓存
	if cache, ok := accessCache.Load(targetURL); ok {
		cacheItem := cache.(accessibilityCache)
		if time.Since(cacheItem.timestamp) < accessTTL {
			return cacheItem.accessible
		}
		accessCache.Delete(targetURL)
	}

	req, err := http.NewRequest("HEAD", targetURL, nil)
	if err != nil {
		log.Printf("[Check] Failed to create request for %s: %v", targetURL, err)
		return false
	}

	// 添加浏览器User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// 设置Referer为目标域名
	if parsedURL, parseErr := neturl.Parse(targetURL); parseErr == nil {
		req.Header.Set("Referer", fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Check] Failed to access %s: %v", targetURL, err)
		return false
	}
	defer resp.Body.Close()

	accessible := resp.StatusCode >= 200 && resp.StatusCode < 400
	// 缓存结果
	accessCache.Store(targetURL, accessibilityCache{
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

// ParseInt 将字符串解析为整数，如果解析失败则返回默认值
func ParseInt(s string, defaultValue int) int {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	if err != nil {
		return defaultValue
	}
	return result
}
