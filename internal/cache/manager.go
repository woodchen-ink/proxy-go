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

// 内存池用于复用缓冲区
var (
	bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 32*1024) // 32KB 缓冲区
		},
	}

	// 大缓冲区池（用于大文件）
	largeBufPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 1024*1024) // 1MB 缓冲区
		},
	}
)

// GetBuffer 从池中获取缓冲区
func GetBuffer(size int) []byte {
	if size <= 32*1024 {
		buf := bufferPool.Get().([]byte)
		if cap(buf) >= size {
			return buf[:size]
		}
		bufferPool.Put(buf)
	} else if size <= 1024*1024 {
		buf := largeBufPool.Get().([]byte)
		if cap(buf) >= size {
			return buf[:size]
		}
		largeBufPool.Put(buf)
	}
	// 如果池中的缓冲区不够大，创建新的
	return make([]byte, size)
}

// PutBuffer 将缓冲区放回池中
func PutBuffer(buf []byte) {
	if cap(buf) == 32*1024 {
		bufferPool.Put(buf)
	} else if cap(buf) == 1024*1024 {
		largeBufPool.Put(buf)
	}
	// 其他大小的缓冲区让GC处理
}

// LRU 缓存节点
type LRUNode struct {
	key   CacheKey
	value *CacheItem
	prev  *LRUNode
	next  *LRUNode
}

// LRU 缓存实现
type LRUCache struct {
	capacity int
	size     int
	head     *LRUNode
	tail     *LRUNode
	cache    map[CacheKey]*LRUNode
	mu       sync.RWMutex
}

// NewLRUCache 创建LRU缓存
func NewLRUCache(capacity int) *LRUCache {
	lru := &LRUCache{
		capacity: capacity,
		cache:    make(map[CacheKey]*LRUNode),
		head:     &LRUNode{},
		tail:     &LRUNode{},
	}
	lru.head.next = lru.tail
	lru.tail.prev = lru.head
	return lru
}

// Get 从LRU缓存中获取
func (lru *LRUCache) Get(key CacheKey) (*CacheItem, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if node, exists := lru.cache[key]; exists {
		lru.moveToHead(node)
		return node.value, true
	}
	return nil, false
}

// Put 向LRU缓存中添加
func (lru *LRUCache) Put(key CacheKey, value *CacheItem) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if node, exists := lru.cache[key]; exists {
		node.value = value
		lru.moveToHead(node)
	} else {
		newNode := &LRUNode{key: key, value: value}
		lru.cache[key] = newNode
		lru.addToHead(newNode)
		lru.size++

		if lru.size > lru.capacity {
			tail := lru.removeTail()
			delete(lru.cache, tail.key)
			lru.size--
		}
	}
}

// Delete 从LRU缓存中删除
func (lru *LRUCache) Delete(key CacheKey) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if node, exists := lru.cache[key]; exists {
		lru.removeNode(node)
		delete(lru.cache, key)
		lru.size--
	}
}

// moveToHead 将节点移到头部
func (lru *LRUCache) moveToHead(node *LRUNode) {
	lru.removeNode(node)
	lru.addToHead(node)
}

// addToHead 添加到头部
func (lru *LRUCache) addToHead(node *LRUNode) {
	node.prev = lru.head
	node.next = lru.head.next
	lru.head.next.prev = node
	lru.head.next = node
}

// removeNode 移除节点
func (lru *LRUCache) removeNode(node *LRUNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// removeTail 移除尾部节点
func (lru *LRUCache) removeTail() *LRUNode {
	lastNode := lru.tail.prev
	lru.removeNode(lastNode)
	return lastNode
}

// Range 遍历所有缓存项
func (lru *LRUCache) Range(fn func(key CacheKey, value *CacheItem) bool) {
	lru.mu.RLock()
	defer lru.mu.RUnlock()

	for key, node := range lru.cache {
		if !fn(key, node.value) {
			break
		}
	}
}

// Size 返回缓存大小
func (lru *LRUCache) Size() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return lru.size
}

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
	Priority        int // 缓存优先级
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
	items        sync.Map  // 保持原有的 sync.Map 用于文件缓存
	lruCache     *LRUCache // 新增LRU缓存用于热点数据
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

	// 缓存预热
	prewarming atomic.Bool
}

// NewCacheManager 创建新的缓存管理器
func NewCacheManager(cacheDir string) (*CacheManager, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}

	cm := &CacheManager{
		cacheDir:     cacheDir,
		lruCache:     NewLRUCache(10000), // 10000个热点缓存项
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

	// 启动缓存预热
	go cm.prewarmCache()

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

	// 检查LRU缓存
	if item, found := cm.lruCache.Get(key); found {
		// 检查LRU缓存项是否过期
		if time.Since(item.LastAccess) > cm.maxAge {
			cm.lruCache.Delete(key)
			cm.missCount.Add(1)
			return nil, false, false
		}
		// 更新访问时间
		item.LastAccess = time.Now()
		atomic.AddInt64(&item.AccessCount, 1)
		cm.hitCount.Add(1)
		return item, true, false
	}

	// 检查文件缓存
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

	// 将缓存项添加到LRU缓存
	cm.lruCache.Put(key, item)

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
	method := "GET"
	if resp.Request != nil {
		method = resp.Request.Method
	}
	log.Printf("[Cache] NEW %s %s (%s) from %s", method, key.URL, formatBytes(item.Size), utils.GetRequestSource(resp.Request))
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

// prewarmCache 启动缓存预热
func (cm *CacheManager) prewarmCache() {
	// 模拟一些请求来预热缓存
	// 实际应用中，这里会从数据库或外部服务加载热点数据
	// 例如，从数据库加载最近访问频率高的URL
	// 或者从外部API获取热门资源

	// 示例：加载最近访问的URL
	// 假设我们有一个数据库或文件，记录最近访问的URL和它们的哈希
	// 这里我们简单地加载一些示例URL
	exampleUrls := []string{
		"https://example.com/api/data",
		"https://api.github.com/repos/golang/go/releases",
		"https://api.openai.com/v1/models",
		"https://api.openai.com/v1/chat/completions",
	}

	for _, url := range exampleUrls {
		// 生成一个随机的Accept Headers和UserAgent
		acceptHeaders := "application/json"
		userAgent := "Mozilla/5.0 (compatible; ProxyGo/1.0)"

		// 模拟一个HTTP请求
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Printf("[Cache] ERR Failed to create request for prewarming: %v", err)
			continue
		}
		req.Header.Set("Accept", acceptHeaders)
		req.Header.Set("User-Agent", userAgent)

		// 生成缓存键
		cacheKey := cm.GenerateCacheKey(req)

		// 尝试从LRU缓存获取
		if _, found := cm.lruCache.Get(cacheKey); found {
			log.Printf("[Cache] WARN %s (prewarmed)", cacheKey.URL)
			continue
		}

		// 尝试从文件缓存获取
		if _, ok := cm.items.Load(cacheKey); ok {
			log.Printf("[Cache] WARN %s (prewarmed)", cacheKey.URL)
			continue
		}

		// 模拟一个HTTP响应
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Request:    req,
		}
		resp.Header.Set("Content-Type", "application/json")
		resp.Header.Set("Content-Encoding", "gzip")
		resp.Header.Set("X-Cache", "HIT") // 模拟缓存命中

		// 模拟一个HTTP请求体
		body := []byte(`{"message": "Hello from prewarmed cache"}`)

		// 添加到LRU缓存
		contentHash := sha256.Sum256(body)
		cm.lruCache.Put(cacheKey, &CacheItem{
			FilePath:        "", // 文件缓存，这里不需要
			ContentType:     "application/json",
			ContentEncoding: "gzip",
			Size:            int64(len(body)),
			LastAccess:      time.Now(),
			Hash:            hex.EncodeToString(contentHash[:]),
			CreatedAt:       time.Now(),
			AccessCount:     1,
		})

		log.Printf("[Cache] PREWARM %s", cacheKey.URL)
	}
}
