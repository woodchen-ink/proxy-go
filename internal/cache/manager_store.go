package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"proxy-go/internal/utils"
	"time"
)

// 写入缓存: 小响应直接 Put, 大响应先写临时文件再 Commit; 相同内容按哈希复用同一个磁盘文件

// Put 添加缓存项
func (cm *CacheManager) Put(key CacheKey, resp *http.Response, body []byte) (*CacheItem, error) {
	// 只检查基本的响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status not OK")
	}

	// 计算内容哈希
	contentHash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(contentHash[:])

	// 检查是否存在相同哈希的缓存项（O(1) 哈希索引查找）
	if existing := cm.lookupByHash(hashStr); existing != nil {
		cm.storeItem(key, existing)
		log.Printf("[Cache] HIT %s %s (%s) from %s", resp.Request.Method, key.URL, formatBytes(existing.Size), utils.GetRequestSource(resp.Request))
		return existing, nil
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

	cm.storeItem(key, item)
	if _, loaded := cm.hashIndex.LoadOrStore(hashStr, item); !loaded {
		cm.addFileAccounting(item.Size)
	}
	method := "GET"
	if resp.Request != nil {
		method = resp.Request.Method
	}
	log.Printf("[Cache] NEW %s %s (%s) from %s", method, key.URL, formatBytes(item.Size), utils.GetRequestSource(resp.Request))
	return item, nil
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

	// 读取临时文件内容以计算哈希
	tempData, err := os.ReadFile(tempPath)
	if err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to read temp file: %v", err)
	}

	// 计算内容哈希，与Put方法保持一致
	contentHash := sha256.Sum256(tempData)
	hashStr := hex.EncodeToString(contentHash[:])

	// 检查是否存在相同哈希的缓存项（O(1) 哈希索引查找）
	if existing := cm.lookupByHash(hashStr); existing != nil {
		// 删除临时文件，使用现有缓存
		os.Remove(tempPath)
		cm.storeItem(key, existing)
		log.Printf("[Cache] HIT %s %s (%s) from %s", resp.Request.Method, key.URL, formatBytes(existing.Size), utils.GetRequestSource(resp.Request))
		return nil
	}

	// 生成最终的缓存文件名（使用内容哈希）
	fileName := hashStr
	filePath := filepath.Join(cm.cacheDir, fileName)

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

	cm.storeItem(key, item)
	if _, loaded := cm.hashIndex.LoadOrStore(hashStr, item); !loaded {
		cm.addFileAccounting(item.Size)
	}
	cm.bytesSaved.Add(size)
	log.Printf("[Cache] NEW %s %s (%s)", resp.Request.Method, key.URL, formatBytes(size))
	return nil
}

// lookupByHash 通过 hash 索引快速查找现有缓存项；如果索引存在但底层文件已丢失，
// 会顺带把陈旧的索引清掉再返回 nil（保持索引一致性）
func (cm *CacheManager) lookupByHash(hashStr string) *CacheItem {
	value, ok := cm.hashIndex.Load(hashStr)
	if !ok {
		return nil
	}
	item := value.(*CacheItem)
	if _, err := os.Stat(item.FilePath); err != nil {
		// 文件已被外部清理，移除索引
		cm.hashIndex.CompareAndDelete(hashStr, item)
		return nil
	}
	return item
}
