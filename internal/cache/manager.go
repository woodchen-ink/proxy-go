package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"proxy-go/internal/config"
	"proxy-go/internal/utils"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CacheKey 用于标识缓存项的唯一键
type CacheKey struct {
	URL           string
	AcceptHeaders string
	UserAgent     string
}

// String 实现 Stringer 接口，用于生成唯一的字符串表示
func (k CacheKey) String() string {
	return fmt.Sprintf("%s|%s|%s", k.URL, k.AcceptHeaders, k.UserAgent)
}

// Equal 比较两个 CacheKey 是否相等
func (k CacheKey) Equal(other CacheKey) bool {
	return k.URL == other.URL &&
		k.AcceptHeaders == other.AcceptHeaders &&
		k.UserAgent == other.UserAgent
}

// Hash 生成 CacheKey 的哈希值
func (k CacheKey) Hash() uint64 {
	h := fnv.New64a()
	h.Write([]byte(k.String()))
	return h.Sum64()
}

// CacheItem 表示一个缓存项
type CacheItem struct {
	FilePath        string
	ContentType     string
	ContentEncoding string
	Size            int64
	LastAccess      time.Time
	Hash            string
	CreatedAt       time.Time
	AccessCount     int64
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
	enabled      atomic.Bool   // 缓存开关
	hitCount     atomic.Int64  // 命中计数
	missCount    atomic.Int64  // 未命中计数
	bytesSaved   atomic.Int64  // 节省的带宽
	cleanupTimer *time.Ticker  // 添加清理定时器
	stopCleanup  chan struct{} // 添加停止信号通道

	// ExtensionMatcher缓存
	extensionMatcherCache *ExtensionMatcherCache
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
		stopCleanup:  make(chan struct{}),

		// 初始化ExtensionMatcher缓存
		extensionMatcherCache: NewExtensionMatcherCache(),
	}

	cm.enabled.Store(true) // 默认启用缓存

	// 尝试加载配置
	if err := cm.loadConfig(); err != nil {
		log.Printf("[Cache] Failed to load config: %v, using default values", err)
	}

	// 启动时清理过期和临时文件
	if err := cm.cleanStaleFiles(); err != nil {
		log.Printf("[Cache] Failed to clean stale files: %v", err)
	}

	// 启动清理协程
	cm.startCleanup()

	return cm, nil
}

// GenerateCacheKey 生成缓存键
func (cm *CacheManager) GenerateCacheKey(r *http.Request) CacheKey {
	// 处理 Vary 头部
	varyHeaders := make([]string, 0)
	for _, vary := range strings.Split(r.Header.Get("Vary"), ",") {
		vary = strings.TrimSpace(vary)
		if vary != "" {
			value := r.Header.Get(vary)
			varyHeaders = append(varyHeaders, vary+"="+value)
		}
	}
	sort.Strings(varyHeaders)

	return CacheKey{
		URL:           r.URL.String(),
		AcceptHeaders: r.Header.Get("Accept"),
		UserAgent:     r.Header.Get("User-Agent"),
	}
}

// Get 获取缓存项
func (cm *CacheManager) Get(key CacheKey, r *http.Request) (*CacheItem, bool, bool) {
	if !cm.enabled.Load() {
		return nil, false, false
	}

	// 检查缓存项是否存在
	value, ok := cm.items.Load(key)
	if !ok {
		cm.missCount.Add(1)
		return nil, false, false
	}

	item := value.(*CacheItem)

	// 验证文件是否存在
	if _, err := os.Stat(item.FilePath); err != nil {
		cm.items.Delete(key)
		cm.missCount.Add(1)
		return nil, false, false
	}

	// 检查是否过期（使用LastAccess而不是CreatedAt）
	if time.Since(item.LastAccess) > cm.maxAge {
		cm.items.Delete(key)
		os.Remove(item.FilePath)
		cm.missCount.Add(1)
		return nil, false, false
	}

	// 更新访问信息（重置过期时间）
	item.LastAccess = time.Now()
	atomic.AddInt64(&item.AccessCount, 1)
	cm.hitCount.Add(1)
	cm.bytesSaved.Add(item.Size)

	return item, true, false
}

// Put 添加缓存项
func (cm *CacheManager) Put(key CacheKey, resp *http.Response, body []byte) (*CacheItem, error) {
	// 只检查基本的响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status not OK")
	}

	// 计算内容哈希
	contentHash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(contentHash[:])

	// 检查是否存在相同哈希的缓存项
	var existingItem *CacheItem
	cm.items.Range(func(k, v interface{}) bool {
		if item := v.(*CacheItem); item.Hash == hashStr {
			if _, err := os.Stat(item.FilePath); err == nil {
				existingItem = item
				return false
			}
			cm.items.Delete(k)
		}
		return true
	})

	if existingItem != nil {
		cm.items.Store(key, existingItem)
		log.Printf("[Cache] HIT %s %s (%s) from %s", resp.Request.Method, key.URL, formatBytes(existingItem.Size), utils.GetRequestSource(resp.Request))
		return existingItem, nil
	}

	// 生成文件名并存储
	fileName := hashStr
	filePath := filepath.Join(cm.cacheDir, fileName)

	if err := os.WriteFile(filePath, body, 0600); err != nil {
		return nil, fmt.Errorf("failed to write cache file: %v", err)
	}

	item := &CacheItem{
		FilePath:        filePath,
		ContentType:     resp.Header.Get("Content-Type"),
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Size:            int64(len(body)),
		LastAccess:      time.Now(),
		Hash:            hashStr,
		CreatedAt:       time.Now(),
		AccessCount:     1,
	}

	cm.items.Store(key, item)
	log.Printf("[Cache] NEW %s %s (%s) from %s", resp.Request.Method, key.URL, formatBytes(item.Size), utils.GetRequestSource(resp.Request))
	return item, nil
}

// cleanup 定期清理过期的缓存项
func (cm *CacheManager) cleanup() {
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
			log.Printf("[Cache] DEL %s (expired)", key.URL)
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
	// 清除内存中的缓存项
	var keysToDelete []CacheKey
	cm.items.Range(func(key, value interface{}) bool {
		cacheKey := key.(CacheKey)
		keysToDelete = append(keysToDelete, cacheKey)
		return true
	})

	for _, key := range keysToDelete {
		cm.items.Delete(key)
	}

	// 清理缓存目录中的所有文件
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %v", err)
	}

	for _, entry := range entries {
		if entry.Name() == "config.json" {
			continue // 保留配置文件
		}
		filePath := filepath.Join(cm.cacheDir, entry.Name())
		if err := os.Remove(filePath); err != nil {
			log.Printf("[Cache] ERR Failed to remove file: %s", entry.Name())
		}
	}

	// 重置统计信息
	cm.hitCount.Store(0)
	cm.missCount.Store(0)
	cm.bytesSaved.Store(0)

	return nil
}

// cleanStaleFiles 清理过期和临时文件
func (cm *CacheManager) cleanStaleFiles() error {
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %v", err)
	}

	for _, entry := range entries {
		if entry.Name() == "config.json" {
			continue // 保留配置文件
		}

		filePath := filepath.Join(cm.cacheDir, entry.Name())

		// 清理临时文件
		if strings.HasPrefix(entry.Name(), "temp-") {
			if err := os.Remove(filePath); err != nil {
				log.Printf("[Cache] ERR Failed to remove temp file: %s", entry.Name())
			}
			continue
		}

		// 检查文件是否仍在缓存记录中
		fileFound := false
		cm.items.Range(func(_, value interface{}) bool {
			item := value.(*CacheItem)
			if item.FilePath == filePath {
				fileFound = true
				return false
			}
			return true
		})

		// 如果文件不在缓存记录中，删除它
		if !fileFound {
			if err := os.Remove(filePath); err != nil {
				log.Printf("[Cache] ERR Failed to remove stale file: %s", entry.Name())
			}
		}
	}

	return nil
}

// CreateTemp 创建临时缓存文件
func (cm *CacheManager) CreateTemp(key CacheKey, resp *http.Response) (*os.File, error) {
	if !cm.enabled.Load() {
		return nil, fmt.Errorf("cache is disabled")
	}

	// 创建临时文件
	tempFile, err := os.CreateTemp(cm.cacheDir, "temp-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %v", err)
	}

	return tempFile, nil
}

// Commit 提交缓存文件
func (cm *CacheManager) Commit(key CacheKey, tempPath string, resp *http.Response, size int64) error {
	if !cm.enabled.Load() {
		os.Remove(tempPath)
		return fmt.Errorf("cache is disabled")
	}

	// 生成最终的缓存文件名
	h := sha256.New()
	h.Write([]byte(key.String()))
	hashStr := hex.EncodeToString(h.Sum(nil))
	ext := filepath.Ext(key.URL)
	if ext == "" {
		ext = ".bin"
	}
	filePath := filepath.Join(cm.cacheDir, hashStr+ext)

	// 重命名临时文件
	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	// 创建缓存项
	item := &CacheItem{
		FilePath:        filePath,
		ContentType:     resp.Header.Get("Content-Type"),
		ContentEncoding: resp.Header.Get("Content-Encoding"),
		Size:            size,
		LastAccess:      time.Now(),
		Hash:            hashStr,
		CreatedAt:       time.Now(),
		AccessCount:     1,
	}

	cm.items.Store(key, item)
	cm.bytesSaved.Add(size)
	log.Printf("[Cache] NEW %s %s (%s)", resp.Request.Method, key.URL, formatBytes(size))
	return nil
}

// GetConfig 获取缓存配置
func (cm *CacheManager) GetConfig() CacheConfig {
	return CacheConfig{
		MaxAge:       int64(cm.maxAge.Minutes()),
		CleanupTick:  int64(cm.cleanupTick.Minutes()),
		MaxCacheSize: cm.maxCacheSize / (1024 * 1024 * 1024), // 转换为GB
	}
}

// UpdateConfig 更新缓存配置
func (cm *CacheManager) UpdateConfig(maxAge, cleanupTick, maxCacheSize int64) error {
	if maxAge <= 0 || cleanupTick <= 0 || maxCacheSize <= 0 {
		return fmt.Errorf("invalid config values: all values must be positive")
	}

	cm.maxAge = time.Duration(maxAge) * time.Minute
	cm.maxCacheSize = maxCacheSize * 1024 * 1024 * 1024 // 转换为字节

	// 如果清理间隔发生变化，重启清理协程
	newCleanupTick := time.Duration(cleanupTick) * time.Minute
	if cm.cleanupTick != newCleanupTick {
		cm.cleanupTick = newCleanupTick
		// 停止当前的清理协程
		cm.stopCleanup <- struct{}{}
		// 启动新的清理协程
		cm.startCleanup()
	}

	// 保存配置到文件
	if err := cm.saveConfig(); err != nil {
		log.Printf("[Cache] Failed to save config: %v", err)
	}

	return nil
}

// CacheConfig 缓存配置结构
type CacheConfig struct {
	MaxAge       int64 `json:"max_age"`        // 最大缓存时间（分钟）
	CleanupTick  int64 `json:"cleanup_tick"`   // 清理间隔（分钟）
	MaxCacheSize int64 `json:"max_cache_size"` // 最大缓存大小（GB）
}

// startCleanup 启动清理协程
func (cm *CacheManager) startCleanup() {
	cm.cleanupTimer = time.NewTicker(cm.cleanupTick)
	go func() {
		for {
			select {
			case <-cm.cleanupTimer.C:
				cm.cleanup()
			case <-cm.stopCleanup:
				cm.cleanupTimer.Stop()
				return
			}
		}
	}()
}

// saveConfig 保存配置到文件
func (cm *CacheManager) saveConfig() error {
	config := CacheConfig{
		MaxAge:       int64(cm.maxAge.Minutes()),
		CleanupTick:  int64(cm.cleanupTick.Minutes()),
		MaxCacheSize: cm.maxCacheSize / (1024 * 1024 * 1024), // 转换为GB
	}

	configPath := filepath.Join(cm.cacheDir, "config.json")
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// loadConfig 从文件加载配置
func (cm *CacheManager) loadConfig() error {
	configPath := filepath.Join(cm.cacheDir, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 如果配置文件不存在，使用默认配置并保存
			return cm.saveConfig()
		}
		return fmt.Errorf("failed to read config file: %v", err)
	}

	var config CacheConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %v", err)
	}

	// 更新配置
	cm.maxAge = time.Duration(config.MaxAge) * time.Minute
	cm.maxCacheSize = config.MaxCacheSize * 1024 * 1024 * 1024 // 转换为字节

	// 如果清理间隔发生变化，重启清理协程
	newCleanupTick := time.Duration(config.CleanupTick) * time.Minute
	if cm.cleanupTick != newCleanupTick {
		cm.cleanupTick = newCleanupTick
		if cm.cleanupTimer != nil {
			cm.stopCleanup <- struct{}{}
		}
		cm.startCleanup()
	}

	return nil
}

// GetExtensionMatcher 获取缓存的ExtensionMatcher
func (cm *CacheManager) GetExtensionMatcher(pathKey string, rules []config.ExtensionRule) *utils.ExtensionMatcher {
	if cm.extensionMatcherCache == nil {
		return utils.NewExtensionMatcher(rules)
	}
	return cm.extensionMatcherCache.GetOrCreate(pathKey, rules)
}

// InvalidateExtensionMatcherPath 使指定路径的ExtensionMatcher缓存失效
func (cm *CacheManager) InvalidateExtensionMatcherPath(pathKey string) {
	if cm.extensionMatcherCache != nil {
		cm.extensionMatcherCache.InvalidatePath(pathKey)
	}
}

// InvalidateAllExtensionMatchers 清空所有ExtensionMatcher缓存
func (cm *CacheManager) InvalidateAllExtensionMatchers() {
	if cm.extensionMatcherCache != nil {
		cm.extensionMatcherCache.InvalidateAll()
	}
}

// GetExtensionMatcherStats 获取ExtensionMatcher缓存统计信息
func (cm *CacheManager) GetExtensionMatcherStats() ExtensionMatcherCacheStats {
	if cm.extensionMatcherCache != nil {
		return cm.extensionMatcherCache.GetStats()
	}
	return ExtensionMatcherCacheStats{}
}

// UpdateExtensionMatcherConfig 更新ExtensionMatcher缓存配置
func (cm *CacheManager) UpdateExtensionMatcherConfig(maxAge, cleanupTick time.Duration) {
	if cm.extensionMatcherCache != nil {
		cm.extensionMatcherCache.UpdateConfig(maxAge, cleanupTick)
	}
}

// Stop 停止缓存管理器（包括ExtensionMatcher缓存）
func (cm *CacheManager) Stop() {
	// 停止主缓存清理
	if cm.cleanupTimer != nil {
		cm.cleanupTimer.Stop()
	}
	close(cm.stopCleanup)

	// 停止ExtensionMatcher缓存
	if cm.extensionMatcherCache != nil {
		cm.extensionMatcherCache.Stop()
	}
}
