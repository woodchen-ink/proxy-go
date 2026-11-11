package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// 缓存清理: 全部 / 按路径前缀 / 按 URL, 以及磁盘孤儿文件清扫

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

	// 清空哈希索引
	cm.hashIndex.Range(func(k, _ interface{}) bool {
		cm.hashIndex.Delete(k)
		return true
	})

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
	cm.formatFallbackHit.Store(0)
	cm.imageCacheHit.Store(0)
	cm.regularCacheHit.Store(0)

	// 缓存已全部清空, 规模计数直接归零
	cm.totalItems.Store(0)
	cm.totalSize.Store(0)
	cm.diskFileCount.Store(0)
	cm.diskSize.Store(0)

	return nil
}

// ClearCacheByPrefix 清除指定路径前缀的缓存
func (cm *CacheManager) ClearCacheByPrefix(pathPrefix string) (int, error) {
	// 规范化路径前缀（确保没有尾部斜杠）
	pathPrefix = strings.TrimSuffix(pathPrefix, "/")

	// 清除内存中匹配的缓存项，并收集需要删除的文件
	var keysToDelete []CacheKey
	var hashesToDelete []hashItem
	filesToDelete := make(map[string]bool)

	cm.items.Range(func(key, value interface{}) bool {
		cacheKey := key.(CacheKey)
		// 检查 URL 是否以指定路径前缀开头
		if strings.HasPrefix(cacheKey.URL, pathPrefix) {
			keysToDelete = append(keysToDelete, cacheKey)

			// 获取缓存项，从中获取文件路径
			if item, ok := value.(*CacheItem); ok && item.FilePath != "" {
				// 从完整路径中提取文件名
				filename := filepath.Base(item.FilePath)
				filesToDelete[filename] = true
				if item.Hash != "" {
					hashesToDelete = append(hashesToDelete, hashItem{item.Hash, item})
				}
			}
		}
		return true
	})

	// 从内存删除缓存项与物理文件索引, 同步扣减两套计数
	cm.deleteKeysAndHashes(keysToDelete, hashesToDelete)

	// 清理缓存目录中匹配的文件
	deletedFiles := 0
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		log.Printf("[Cache] WARN Failed to read cache directory: %v", err)
		// 即使读取目录失败，内存清理仍然成功
		return len(keysToDelete), nil
	}

	// 删除匹配的缓存文件
	for _, entry := range entries {
		if entry.Name() == "config.json" || strings.HasSuffix(entry.Name(), ".json") {
			continue // 保留配置文件和其他 JSON 文件
		}

		if filesToDelete[entry.Name()] {
			filePath := filepath.Join(cm.cacheDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				log.Printf("[Cache] WARN Failed to remove file: %s", entry.Name())
			} else {
				deletedFiles++
			}
		}
	}

	log.Printf("[Cache] Cleared %d cache items (%d files) for path prefix: %s", len(keysToDelete), deletedFiles, pathPrefix)
	return len(keysToDelete), nil
}

// ClearCacheByURLs 清除指定 URL 列表的缓存
func (cm *CacheManager) ClearCacheByURLs(urls []string) (int, error) {
	if len(urls) == 0 {
		return 0, nil
	}

	// 规范化 URL 列表（去除尾部斜杠）
	urlSet := make(map[string]bool)
	for _, url := range urls {
		normalizedURL := strings.TrimSuffix(url, "/")
		urlSet[normalizedURL] = true
	}

	// 清除内存中匹配的缓存项，并收集需要删除的文件
	var keysToDelete []CacheKey
	var hashesToDelete []hashItem
	filesToDelete := make(map[string]bool)

	cm.items.Range(func(key, value interface{}) bool {
		cacheKey := key.(CacheKey)
		// 检查 URL 是否在指定列表中（精确匹配）
		normalizedCacheURL := strings.TrimSuffix(cacheKey.URL, "/")
		if urlSet[normalizedCacheURL] {
			keysToDelete = append(keysToDelete, cacheKey)

			// 获取缓存项，从中获取文件路径
			if item, ok := value.(*CacheItem); ok && item.FilePath != "" {
				// 从完整路径中提取文件名
				filename := filepath.Base(item.FilePath)
				filesToDelete[filename] = true
				if item.Hash != "" {
					hashesToDelete = append(hashesToDelete, hashItem{item.Hash, item})
				}
			}
		}
		return true
	})

	// 从内存删除缓存项与物理文件索引, 同步扣减两套计数
	cm.deleteKeysAndHashes(keysToDelete, hashesToDelete)

	// 清理缓存目录中匹配的文件
	deletedFiles := 0
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		log.Printf("[Cache] WARN Failed to read cache directory: %v", err)
		// 即使读取目录失败，内存清理仍然成功
		return len(keysToDelete), nil
	}

	// 删除匹配的缓存文件
	for _, entry := range entries {
		if entry.Name() == "config.json" || strings.HasSuffix(entry.Name(), ".json") {
			continue // 保留配置文件和其他 JSON 文件
		}

		if filesToDelete[entry.Name()] {
			filePath := filepath.Join(cm.cacheDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				log.Printf("[Cache] WARN Failed to remove file: %s", entry.Name())
			} else {
				deletedFiles++
			}
		}
	}

	log.Printf("[Cache] Cleared %d cache items (%d files) for %d specific URLs", len(keysToDelete), deletedFiles, len(urls))
	return len(keysToDelete), nil
}

// ClearCacheByURL 清除单个 URL 的缓存，按路径语义忽略 query 和 fragment。
func (cm *CacheManager) ClearCacheByURL(rawURL string) (int, error) {
	targetURL := normalizeCacheMatchURL(rawURL)
	if targetURL == "" {
		return 0, nil
	}

	var keysToDelete []CacheKey
	filesToDelete := make(map[string]bool)

	var hashesToDelete []hashItem
	cm.items.Range(func(key, value interface{}) bool {
		cacheKey := key.(CacheKey)
		if normalizeCacheMatchURL(cacheKey.URL) == targetURL {
			keysToDelete = append(keysToDelete, cacheKey)

			if item, ok := value.(*CacheItem); ok && item.FilePath != "" {
				filename := filepath.Base(item.FilePath)
				filesToDelete[filename] = true
				if item.Hash != "" {
					hashesToDelete = append(hashesToDelete, hashItem{item.Hash, item})
				}
			}
		}
		return true
	})

	// 从内存删除缓存项与物理文件索引, 同步扣减两套计数
	cm.deleteKeysAndHashes(keysToDelete, hashesToDelete)

	deletedFiles := 0
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		log.Printf("[Cache] WARN Failed to read cache directory: %v", err)
		return len(keysToDelete), nil
	}

	for _, entry := range entries {
		if entry.Name() == "config.json" || strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		if filesToDelete[entry.Name()] {
			filePath := filepath.Join(cm.cacheDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				log.Printf("[Cache] WARN Failed to remove file: %s", entry.Name())
			} else {
				deletedFiles++
			}
		}
	}

	log.Printf("[Cache] Cleared %d cache items (%d files) for single URL: %s", len(keysToDelete), deletedFiles, targetURL)
	return len(keysToDelete), nil
}

// HasURLCached 以 O(1) 判断给定 URL 的默认缓存键是否存在条目 (仅内存查找, 不触发全量遍历)。
//
// 用途: 回源返回 404 时先廉价探测本地是否缓存过该 URL, 命中才调用 ClearCacheByURL (全量遍历 + ReadDir)。
// 机器人海量探测不存在路径时, 此处 items.Load 立即 miss, 不会引发昂贵清理。
//
// 局限: 只查默认键 CacheKey{URL}, 不覆盖图片 Accept/UA 变体键 (变体残留靠上游关 Vary 收敛 + 源站侧延迟补刀兜底)。
func (cm *CacheManager) HasURLCached(rawURL string) bool {
	if !cm.enabled.Load() {
		return false
	}

	_, ok := cm.items.Load(CacheKey{URL: strings.TrimSpace(rawURL)})
	return ok
}

// normalizeCacheMatchURL 用于缓存清理匹配，忽略 query 和 fragment，并统一尾斜杠。
func normalizeCacheMatchURL(rawURL string) string {
	normalized := strings.TrimSpace(rawURL)
	if normalized == "" {
		return ""
	}

	if index := strings.IndexAny(normalized, "?#"); index >= 0 {
		normalized = normalized[:index]
	}

	if normalized != "/" {
		normalized = strings.TrimSuffix(normalized, "/")
	}

	return normalized
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
