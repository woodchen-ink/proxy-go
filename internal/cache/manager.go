package cache

import (
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"proxy-go/internal/config"
	"proxy-go/internal/utils"
	"sort"
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
	TotalItems        int     `json:"total_items"`         // 缓存项数量 (逻辑口径, 含同内容多 key)
	TotalSize         int64   `json:"total_size"`          // 缓存项总大小 (逻辑口径)
	DiskFileCount     int     `json:"disk_file_count"`     // 物理文件数量 (去重口径, 同 hash 只算一份)
	DiskSize          int64   `json:"disk_size"`           // 磁盘实际占用字节 (去重口径)
	HitCount          int64   `json:"hit_count"`           // 命中次数
	MissCount         int64   `json:"miss_count"`          // 未命中次数
	HitRate           float64 `json:"hit_rate"`            // 命中率
	BytesSaved        int64   `json:"bytes_saved"`         // 节省的带宽
	Enabled           bool    `json:"enabled"`             // 缓存开关状态
	FormatFallbackHit int64   `json:"format_fallback_hit"` // 格式回退命中次数
	ImageCacheHit     int64   `json:"image_cache_hit"`     // 图片缓存命中次数
	RegularCacheHit   int64   `json:"regular_cache_hit"`   // 常规缓存命中次数
}

// CacheManager 缓存管理器
type CacheManager struct {
	cacheDir string
	items    sync.Map // 保持原有的 sync.Map 用于文件缓存
	// hashIndex 是 hash -> *CacheItem 的二级索引，用于 O(1) 哈希去重
	// 避免 Put/Commit 时 Range 整个 items
	hashIndex    sync.Map
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

	// 新增：格式回退统计
	formatFallbackHit atomic.Int64 // 格式回退命中次数
	imageCacheHit     atomic.Int64 // 图片缓存命中次数
	regularCacheHit   atomic.Int64 // 常规缓存命中次数

	// 缓存规模计数 (增量维护, 避免 GetStats 每次全量 Range items)
	// - totalItems / totalSize: 逻辑口径, 按 items 里的 key 累计 (同内容不同 key 各算一份)
	// - diskFileCount / diskSize: 去重口径, 按 hashIndex 里的物理文件累计 (同 hash 只算一份)
	totalItems    atomic.Int64
	totalSize     atomic.Int64
	diskFileCount atomic.Int64
	diskSize      atomic.Int64

	// ExtensionMatcher缓存
	extensionMatcherCache *ExtensionMatcherCache
}

// addItemAccounting / subItemAccounting 维护逻辑口径计数 (items 维度), 在每次
// items.Store 新增 / items.Delete 删除一个 key 时成对调用。
// 注意: items.Store 覆盖已存在的 key 不应重复 +1, 调用方需自行保证只在真正新增时调用。
func (cm *CacheManager) addItemAccounting(size int64) {
	cm.totalItems.Add(1)
	cm.totalSize.Add(size)
}

func (cm *CacheManager) subItemAccounting(size int64) {
	cm.totalItems.Add(-1)
	cm.totalSize.Add(-size)
}

// addFileAccounting / subFileAccounting 维护去重口径计数 (hashIndex 维度, 即物理文件),
// 在 hashIndex.Store 新建 / CompareAndDelete 删除一个物理文件时成对调用。
func (cm *CacheManager) addFileAccounting(size int64) {
	cm.diskFileCount.Add(1)
	cm.diskSize.Add(size)
}

func (cm *CacheManager) subFileAccounting(size int64) {
	cm.diskFileCount.Add(-1)
	cm.diskSize.Add(-size)
}

// hashItem 承载"待删物理文件"的 hash 与其 item, 供批量清理复用。
type hashItem struct {
	hash string
	item *CacheItem
}

// deleteKeysAndHashes 从 items 删除给定 key 集合、从 hashIndex 删除给定物理文件集合,
// 并同步扣减逻辑口径 / 去重口径计数。ClearCacheByPrefix / ByURLs / ByURL 共用此逻辑,
// 避免三处各写一遍删除 + 记账。
func (cm *CacheManager) deleteKeysAndHashes(keys []CacheKey, hashes []hashItem) {
	for _, key := range keys {
		if v, loaded := cm.items.LoadAndDelete(key); loaded {
			if item, ok := v.(*CacheItem); ok {
				cm.subItemAccounting(item.Size)
			}
		}
	}
	for _, h := range hashes {
		if cm.hashIndex.CompareAndDelete(h.hash, h.item) {
			cm.subFileAccounting(h.item.Size)
		}
	}
}

// storeItem 把 key -> item 写入 items, 并按"是否真正新增 key"维护逻辑口径计数:
// 覆盖同一 key 时先扣掉旧项再计新项 (幂等), 新增 key 时直接计一份, 避免重复 Put / 复用
// 已缓存文件时把 totalItems 记多。物理文件维度 (diskXxx) 不在这里维护, 由 hashIndex 增删处负责。
func (cm *CacheManager) storeItem(key CacheKey, item *CacheItem) {
	if old, loaded := cm.items.Swap(key, item); loaded {
		if oldItem, ok := old.(*CacheItem); ok {
			cm.subItemAccounting(oldItem.Size)
		}
	}
	cm.addItemAccounting(item.Size)
}

// NewCacheManager 创建新的缓存管理器
func NewCacheManager(cacheDir string, initialConfig *config.CacheConfig) (*CacheManager, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %v", err)
	}

	cm := &CacheManager{
		cacheDir:    cacheDir,
		lruCache:    NewLRUCache(10000), // 10000个热点缓存项
		stopCleanup: make(chan struct{}),

		// 初始化ExtensionMatcher缓存
		extensionMatcherCache: NewExtensionMatcherCache(),
	}

	cm.enabled.Store(true) // 默认启用缓存

	// 应用初始配置，对于0值使用默认值
	if initialConfig != nil && initialConfig.MaxAge > 0 && initialConfig.CleanupTick > 0 && initialConfig.MaxCacheSize > 0 {
		cm.maxAge = time.Duration(initialConfig.MaxAge) * time.Minute
		cm.cleanupTick = time.Duration(initialConfig.CleanupTick) * time.Minute
		cm.maxCacheSize = initialConfig.MaxCacheSize * 1024 * 1024 * 1024 // 转换为字节
	} else {
		// 使用默认值（当配置为nil或包含0值时）
		// 默认值定义在 config 包, 与客户端缓存头共用, 避免两边各自硬编码后悄悄漂移
		cm.maxAge = time.Duration(config.DefaultCacheMaxAge) * time.Minute
		cm.cleanupTick = time.Duration(config.DefaultCacheCleanupTick) * time.Minute
		cm.maxCacheSize = config.DefaultCacheMaxSizeGB * 1024 * 1024 * 1024
		log.Printf("[Cache] Using default cache config (maxAge: %dmin, cleanupTick: %dmin, maxSize: %dGB)",
			config.DefaultCacheMaxAge, config.DefaultCacheCleanupTick, config.DefaultCacheMaxSizeGB)
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
//
// cfImageOpt 为 true 时, 图片请求按"标准化图片格式 + 标准化浏览器"分桶 (用于 Cloudflare Images
// 这类按 Accept 返回不同格式的源, 避免 webp/jpeg 互相覆盖); 为 false 时所有请求只用 URL 做 key,
// 让同一原文件在所有设备间共享同一份缓存, 显著提升命中率。源站不做格式协商时务必保持 false。
func (cm *CacheManager) GenerateCacheKey(r *http.Request, cfImageOpt bool) CacheKey {
	url := r.URL.String()

	if cfImageOpt && utils.IsImageRequest(r.URL.Path) {
		acceptHeaders := r.Header.Get("Accept")
		userAgent := r.Header.Get("User-Agent")
		imageFormat := cm.parseImageFormatPreference(acceptHeaders)

		return CacheKey{
			URL:           url,
			AcceptHeaders: imageFormat,
			UserAgent:     cm.normalizeUserAgent(userAgent),
		}
	}

	// 默认: 仅按 URL 缓存, 不区分 Accept / UA, 保证最高命中率
	return CacheKey{URL: url}
}

// Get 获取缓存项
//
// cfImageOpt 与 GenerateCacheKey 的语义保持一致: 仅当启用时才会对图片请求执行格式回退
// (avif → webp → jpeg 等), 否则按精确 key 命中即可。
func (cm *CacheManager) Get(key CacheKey, r *http.Request, cfImageOpt bool) (*CacheItem, bool, bool) {
	if !cm.enabled.Load() {
		return nil, false, false
	}

	if cfImageOpt && utils.IsImageRequest(r.URL.Path) {
		return cm.getImageWithFallback(key, r)
	}

	return cm.getRegularItem(key)
}

// getRegularItem 获取常规缓存项（原有逻辑）
func (cm *CacheManager) getRegularItem(key CacheKey) (*CacheItem, bool, bool) {
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
		cm.regularCacheHit.Add(1)
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
		cm.subItemAccounting(item.Size)
		if cm.hashIndex.CompareAndDelete(item.Hash, item) {
			cm.subFileAccounting(item.Size)
		}
		cm.missCount.Add(1)
		return nil, false, false
	}

	// 检查是否过期（使用LastAccess而不是CreatedAt）
	if time.Since(item.LastAccess) > cm.maxAge {
		cm.items.Delete(key)
		cm.subItemAccounting(item.Size)
		if cm.hashIndex.CompareAndDelete(item.Hash, item) {
			cm.subFileAccounting(item.Size)
		}
		os.Remove(item.FilePath)
		cm.missCount.Add(1)
		return nil, false, false
	}

	// 更新访问信息（重置过期时间）
	item.LastAccess = time.Now()
	atomic.AddInt64(&item.AccessCount, 1)
	cm.hitCount.Add(1)
	cm.regularCacheHit.Add(1)
	cm.bytesSaved.Add(item.Size)

	// 将缓存项添加到LRU缓存
	cm.lruCache.Put(key, item)

	return item, true, false
}

// cleanup 定期清理过期的缓存项
//
// 单趟 Range 收集 (key, item) 快照, 过期项直接入删除队列, 未过期项留作容量淘汰候选。
// 超出 maxCacheSize 时对候选按 LastAccess 升序补删最旧项, 全程只遍历一次 items,
// 不再对每个待删项回头 Range 反查 key (原实现最坏 O(n²), 140 万条目触发容量淘汰会卡死)。
func (cm *CacheManager) cleanup() {
	type entry struct {
		key  CacheKey
		item *CacheItem
	}

	var totalSize int64
	var keysToDelete []CacheKey
	var survivors []entry // 未过期项, 作为容量淘汰候选

	cm.items.Range(func(k, v interface{}) bool {
		key := k.(CacheKey)
		item := v.(*CacheItem)
		totalSize += item.Size

		if time.Since(item.LastAccess) > cm.maxAge {
			keysToDelete = append(keysToDelete, key)
			totalSize -= item.Size // 过期项即将删除, 不计入容量
		} else {
			survivors = append(survivors, entry{key, item})
		}
		return true
	})

	// 如果剩余总大小仍超过限制，按最后访问时间升序补删最旧的存活项
	if totalSize > cm.maxCacheSize {
		sort.Slice(survivors, func(i, j int) bool {
			return survivors[i].item.LastAccess.Before(survivors[j].item.LastAccess)
		})
		for _, e := range survivors {
			if totalSize <= cm.maxCacheSize {
				break
			}
			keysToDelete = append(keysToDelete, e.key)
			totalSize -= e.item.Size
		}
	}

	// 删除过期和超出大小限制的缓存项
	for _, key := range keysToDelete {
		if item, ok := cm.items.Load(key); ok {
			cacheItem := item.(*CacheItem)
			os.Remove(cacheItem.FilePath)
			cm.items.Delete(key)
			cm.subItemAccounting(cacheItem.Size)
			if cm.hashIndex.CompareAndDelete(cacheItem.Hash, cacheItem) {
				cm.subFileAccounting(cacheItem.Size)
			}
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
//
// 规模字段 (TotalItems / TotalSize / DiskFileCount / DiskSize) 全部读增量维护的原子计数,
// O(1) 返回, 不再对 items 做全量 Range; 仪表盘可高频轮询而不产生正比于缓存规模的 CPU 开销。
func (cm *CacheManager) GetStats() CacheStats {
	hitCount := cm.hitCount.Load()
	missCount := cm.missCount.Load()
	totalRequests := hitCount + missCount
	hitRate := float64(0)
	if totalRequests > 0 {
		hitRate = float64(hitCount) / float64(totalRequests) * 100
	}

	return CacheStats{
		TotalItems:        int(cm.totalItems.Load()),
		TotalSize:         cm.totalSize.Load(),
		DiskFileCount:     int(cm.diskFileCount.Load()),
		DiskSize:          cm.diskSize.Load(),
		HitCount:          hitCount,
		MissCount:         missCount,
		HitRate:           hitRate,
		BytesSaved:        cm.bytesSaved.Load(),
		Enabled:           cm.enabled.Load(),
		FormatFallbackHit: cm.formatFallbackHit.Load(),
		ImageCacheHit:     cm.imageCacheHit.Load(),
		RegularCacheHit:   cm.regularCacheHit.Load(),
	}
}

// SetEnabled 设置缓存开关状态
func (cm *CacheManager) SetEnabled(enabled bool) {
	cm.enabled.Store(enabled)
}

// GetConfig 获取缓存配置
func (cm *CacheManager) GetConfig() config.CacheConfig {
	return config.CacheConfig{
		MaxAge:       int64(cm.maxAge.Minutes()),
		CleanupTick:  int64(cm.cleanupTick.Minutes()),
		MaxCacheSize: cm.maxCacheSize / (1024 * 1024 * 1024), // 转换为GB
	}
}

// UpdateConfig 更新缓存配置
func (cm *CacheManager) UpdateConfig(cacheConfig *config.CacheConfig) error {
	if cacheConfig.MaxAge <= 0 || cacheConfig.CleanupTick <= 0 || cacheConfig.MaxCacheSize <= 0 {
		return fmt.Errorf("invalid config values: all values must be positive")
	}

	cm.maxAge = time.Duration(cacheConfig.MaxAge) * time.Minute
	cm.maxCacheSize = cacheConfig.MaxCacheSize * 1024 * 1024 * 1024 // 转换为字节

	// 如果清理间隔发生变化，重启清理协程
	newCleanupTick := time.Duration(cacheConfig.CleanupTick) * time.Minute
	if cm.cleanupTick != newCleanupTick {
		cm.cleanupTick = newCleanupTick
		// 停止当前的清理协程
		cm.stopCleanup <- struct{}{}
		// 启动新的清理协程
		cm.startCleanup()
	}

	return nil
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

// InvalidateCacheItem 使指定缓存项失效（删除内存记录和文件）
func (cm *CacheManager) InvalidateCacheItem(key CacheKey) {
	// 从sync.Map中获取缓存项
	if value, ok := cm.items.Load(key); ok {
		item := value.(*CacheItem)
		// 删除文件
		if err := os.Remove(item.FilePath); err != nil {
			log.Printf("[Cache] Failed to remove cache file %s: %v", item.FilePath, err)
		}
		// 删除内存记录及哈希索引, 同步扣减两套计数
		cm.items.Delete(key)
		cm.subItemAccounting(item.Size)
		if cm.hashIndex.CompareAndDelete(item.Hash, item) {
			cm.subFileAccounting(item.Size)
		}
		log.Printf("[Cache] Invalidated cache item for key: %s", key.URL)
	}

	// 同时从LRU缓存中删除
	if cm.lruCache != nil {
		cm.lruCache.Delete(key)
	}
}
