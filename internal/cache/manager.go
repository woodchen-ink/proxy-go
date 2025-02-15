package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CacheKey 用于标识缓存项的唯一键
type CacheKey struct {
	URL            string
	AcceptHeaders  string
	UserAgent      string
	VaryHeadersMap map[string]string // 存储 Vary 头部的值
}

// String 实现 Stringer 接口，用于生成唯一的字符串表示
func (k CacheKey) String() string {
	// 将 VaryHeadersMap 转换为有序的字符串
	var varyPairs []string
	for key, value := range k.VaryHeadersMap {
		varyPairs = append(varyPairs, key+"="+value)
	}
	sort.Strings(varyPairs)
	varyStr := strings.Join(varyPairs, "&")

	return fmt.Sprintf("%s|%s|%s|%s", k.URL, k.AcceptHeaders, k.UserAgent, varyStr)
}

// Equal 比较两个 CacheKey 是否相等
func (k CacheKey) Equal(other CacheKey) bool {
	if k.URL != other.URL || k.AcceptHeaders != other.AcceptHeaders || k.UserAgent != other.UserAgent {
		return false
	}

	if len(k.VaryHeadersMap) != len(other.VaryHeadersMap) {
		return false
	}

	for key, value := range k.VaryHeadersMap {
		if otherValue, ok := other.VaryHeadersMap[key]; !ok || value != otherValue {
			return false
		}
	}

	return true
}

// Hash 生成 CacheKey 的哈希值
func (k CacheKey) Hash() uint64 {
	h := fnv.New64a()
	h.Write([]byte(k.String()))
	return h.Sum64()
}

// CacheItem 表示一个缓存项
type CacheItem struct {
	FilePath     string
	ContentType  string
	Size         int64
	LastAccess   time.Time
	Hash         string
	ETag         string
	LastModified time.Time
	CacheControl string
	VaryHeaders  []string
	// 新增防穿透字段
	NegativeCache bool  // 标记是否为空结果缓存
	AccessCount   int64 // 访问计数
	CreatedAt     time.Time
}

// CacheStats 缓存统计信息
type CacheStats struct {
	TotalItems int     `json:"total_items"` // 缓存项数量
	TotalSize  int64   `json:"total_size"`  // 总大小
	HitCount   int64   `json:"hit_count"`   // 命中次数
	MissCount  int64   `json:"miss_count"`  // 未命中次数
	HitRate    float64 `json:"hit_rate"`    // 命中率
	BytesSaved int64   `json:"bytes_saved"` // 节省的带宽
	Enabled    bool    `json:"enabled"`     // 缓存开关状态
}

// CacheManager 缓存管理器
type CacheManager struct {
	cacheDir     string
	items        sync.Map
	maxAge       time.Duration
	cleanupTick  time.Duration
	maxCacheSize int64
	enabled      atomic.Bool  // 缓存开关
	hitCount     atomic.Int64 // 命中计数
	missCount    atomic.Int64 // 未命中计数
	bytesSaved   atomic.Int64 // 节省的带宽
}

// NewCacheManager 创建新的缓存管理器
func NewCacheManager(cacheDir string) (*CacheManager, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}

	cm := &CacheManager{
		cacheDir:     cacheDir,
		maxAge:       30 * time.Minute,
		cleanupTick:  5 * time.Minute,
		maxCacheSize: 10 * 1024 * 1024 * 1024, // 10GB
	}

	cm.enabled.Store(true) // 默认启用缓存

	// 启动清理协程
	go cm.cleanup()

	return cm, nil
}

// GenerateCacheKey 生成缓存键
func (cm *CacheManager) GenerateCacheKey(r *http.Request) CacheKey {
	return CacheKey{
		URL:           r.URL.String(),
		AcceptHeaders: r.Header.Get("Accept"),
		UserAgent:     r.Header.Get("User-Agent"),
	}
}

// Get 获取缓存项
func (cm *CacheManager) Get(key CacheKey, r *http.Request) (*CacheItem, bool, bool) {
	// 如果缓存被禁用，直接返回未命中
	if !cm.enabled.Load() {
		cm.missCount.Add(1)
		return nil, false, false
	}

	// 检查是否存在缓存项
	if value, ok := cm.items.Load(key); ok {
		item := value.(*CacheItem)

		// 检查文件是否存在
		if _, err := os.Stat(item.FilePath); err != nil {
			cm.items.Delete(key)
			cm.missCount.Add(1)
			return nil, false, false
		}

		// 检查是否为负缓存（防止缓存穿透）
		if item.NegativeCache {
			// 如果访问次数较少且是负缓存，允许重新验证
			if item.AccessCount < 10 {
				item.AccessCount++
				return nil, false, false
			}
			// 返回空结果，但标记为命中
			cm.hitCount.Add(1)
			return nil, true, true
		}

		// 检查 Vary 头部
		for _, varyHeader := range item.VaryHeaders {
			if r.Header.Get(varyHeader) != key.VaryHeadersMap[varyHeader] {
				cm.missCount.Add(1)
				return nil, false, false
			}
		}

		// 处理条件请求
		ifNoneMatch := r.Header.Get("If-None-Match")
		ifModifiedSince := r.Header.Get("If-Modified-Since")

		// ETag 匹配
		if ifNoneMatch != "" && item.ETag != "" {
			if ifNoneMatch == item.ETag {
				cm.hitCount.Add(1)
				return item, true, true
			}
		}

		// Last-Modified 匹配
		if ifModifiedSince != "" && !item.LastModified.IsZero() {
			if modifiedSince, err := time.Parse(time.RFC1123, ifModifiedSince); err == nil {
				if !item.LastModified.After(modifiedSince) {
					cm.hitCount.Add(1)
					return item, true, true
				}
			}
		}

		// 检查 Cache-Control
		if item.CacheControl != "" {
			if cm.isCacheExpired(item) {
				cm.items.Delete(key)
				cm.missCount.Add(1)
				return nil, false, false
			}
		}

		// 更新访问统计
		item.LastAccess = time.Now()
		item.AccessCount++
		cm.hitCount.Add(1)
		cm.bytesSaved.Add(item.Size)
		return item, true, false
	}

	cm.missCount.Add(1)
	return nil, false, false
}

// isCacheExpired 检查缓存是否过期
func (cm *CacheManager) isCacheExpired(item *CacheItem) bool {
	if item.CacheControl == "" {
		return false
	}

	// 解析 max-age
	if strings.Contains(item.CacheControl, "max-age=") {
		parts := strings.Split(item.CacheControl, "max-age=")
		if len(parts) > 1 {
			maxAge := strings.Split(parts[1], ",")[0]
			if seconds, err := strconv.Atoi(maxAge); err == nil {
				return time.Since(item.CreatedAt) > time.Duration(seconds)*time.Second
			}
		}
	}

	return false
}

// Put 添加缓存项
func (cm *CacheManager) Put(key CacheKey, resp *http.Response, body []byte) (*CacheItem, error) {
	// 检查缓存控制头
	if !cm.shouldCache(resp) {
		return nil, fmt.Errorf("response should not be cached")
	}

	// 生成文件名
	hash := sha256.Sum256([]byte(fmt.Sprintf("%v-%v-%v-%v", key.URL, key.AcceptHeaders, key.UserAgent, time.Now().UnixNano())))
	fileName := hex.EncodeToString(hash[:])
	filePath := filepath.Join(cm.cacheDir, fileName)

	// 使用更安全的文件权限
	if err := os.WriteFile(filePath, body, 0600); err != nil {
		return nil, fmt.Errorf("failed to write cache file: %v", err)
	}

	// 计算内容哈希
	contentHash := sha256.Sum256(body)

	// 解析缓存控制头
	cacheControl := resp.Header.Get("Cache-Control")
	lastModified := resp.Header.Get("Last-Modified")
	etag := resp.Header.Get("ETag")

	var lastModifiedTime time.Time
	if lastModified != "" {
		if t, err := time.Parse(time.RFC1123, lastModified); err == nil {
			lastModifiedTime = t
		}
	}

	// 处理 Vary 头部
	varyHeaders := strings.Split(resp.Header.Get("Vary"), ",")
	for i, h := range varyHeaders {
		varyHeaders[i] = strings.TrimSpace(h)
	}

	item := &CacheItem{
		FilePath:     filePath,
		ContentType:  resp.Header.Get("Content-Type"),
		Size:         int64(len(body)),
		LastAccess:   time.Now(),
		Hash:         hex.EncodeToString(contentHash[:]),
		ETag:         etag,
		LastModified: lastModifiedTime,
		CacheControl: cacheControl,
		VaryHeaders:  varyHeaders,
		CreatedAt:    time.Now(),
		AccessCount:  1,
	}

	// 检查是否有相同内容的缓存
	var existingItem *CacheItem
	cm.items.Range(func(k, v interface{}) bool {
		if i := v.(*CacheItem); i.Hash == item.Hash {
			existingItem = i
			return false
		}
		return true
	})

	if existingItem != nil {
		// 如果找到相同内容的缓存，删除新文件，复用现有缓存
		os.Remove(filePath)
		cm.items.Store(key, existingItem)
		log.Printf("[Cache] Found duplicate content for %s, reusing existing cache", key.URL)
		return existingItem, nil
	}

	// 存储新的缓存项
	cm.items.Store(key, item)
	log.Printf("[Cache] Cached %s (%s)", key.URL, formatBytes(item.Size))
	return item, nil
}

// shouldCache 检查响应是否应该被缓存
func (cm *CacheManager) shouldCache(resp *http.Response) bool {
	// 检查状态码
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
		return false
	}

	// 解析 Cache-Control 头
	cacheControl := resp.Header.Get("Cache-Control")
	if strings.Contains(cacheControl, "no-store") ||
		strings.Contains(cacheControl, "no-cache") ||
		strings.Contains(cacheControl, "private") {
		return false
	}

	return true
}

// cleanup 定期清理过期的缓存项
func (cm *CacheManager) cleanup() {
	ticker := time.NewTicker(cm.cleanupTick)
	for range ticker.C {
		var totalSize int64
		var keysToDelete []CacheKey

		// 收集需要删除的键和计算总大小
		cm.items.Range(func(k, v interface{}) bool {
			key := k.(CacheKey)
			item := v.(*CacheItem)
			totalSize += item.Size

			if time.Since(item.LastAccess) > cm.maxAge {
				keysToDelete = append(keysToDelete, key)
			}
			return true
		})

		// 如果总大小超过限制，按最后访问时间排序删除
		if totalSize > cm.maxCacheSize {
			var items []*CacheItem
			cm.items.Range(func(k, v interface{}) bool {
				items = append(items, v.(*CacheItem))
				return true
			})

			// 按最后访问时间排序
			sort.Slice(items, func(i, j int) bool {
				return items[i].LastAccess.Before(items[j].LastAccess)
			})

			// 删除最旧的项直到总大小小于限制
			for _, item := range items {
				if totalSize <= cm.maxCacheSize {
					break
				}
				cm.items.Range(func(k, v interface{}) bool {
					if v.(*CacheItem) == item {
						keysToDelete = append(keysToDelete, k.(CacheKey))
						totalSize -= item.Size
						return false
					}
					return true
				})
			}
		}

		// 删除过期和超出大小限制的缓存项
		for _, key := range keysToDelete {
			if item, ok := cm.items.Load(key); ok {
				cacheItem := item.(*CacheItem)
				os.Remove(cacheItem.FilePath)
				cm.items.Delete(key)
				log.Printf("[Cache] Removed expired item: %s", key.URL)
			}
		}
	}
}

// formatBytes 格式化字节大小
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// GetStats 获取缓存统计信息
func (cm *CacheManager) GetStats() CacheStats {
	var totalItems int
	var totalSize int64

	cm.items.Range(func(_, value interface{}) bool {
		item := value.(*CacheItem)
		totalItems++
		totalSize += item.Size
		return true
	})

	hitCount := cm.hitCount.Load()
	missCount := cm.missCount.Load()
	totalRequests := hitCount + missCount
	hitRate := float64(0)
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests) * 100
	}

	return CacheStats{
		TotalItems: totalItems,
		TotalSize:  totalSize,
		HitCount:   hitCount,
		MissCount:  missCount,
		HitRate:    hitRate,
		BytesSaved: cm.bytesSaved.Load(),
		Enabled:    cm.enabled.Load(),
	}
}

// SetEnabled 设置缓存开关状态
func (cm *CacheManager) SetEnabled(enabled bool) {
	cm.enabled.Store(enabled)
}

// ClearCache 清空缓存
func (cm *CacheManager) ClearCache() error {
	// 删除所有缓存文件
	var keysToDelete []CacheKey
	cm.items.Range(func(key, value interface{}) bool {
		cacheKey := key.(CacheKey)
		item := value.(*CacheItem)
		os.Remove(item.FilePath)
		keysToDelete = append(keysToDelete, cacheKey)
		return true
	})

	// 清除缓存项
	for _, key := range keysToDelete {
		cm.items.Delete(key)
	}

	// 重置统计信息
	cm.hitCount.Store(0)
	cm.missCount.Store(0)
	cm.bytesSaved.Store(0)

	return nil
}
