package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	neturl "net/url"
	"path/filepath"
	"proxy-go/internal/config"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Goroutine 池相关结构
type GoroutinePool struct {
	maxWorkers int
	taskQueue  chan func()
	wg         sync.WaitGroup
	once       sync.Once
	stopped    int32
}

// 全局 goroutine 池
var (
	globalPool     *GoroutinePool
	poolOnce       sync.Once
	defaultWorkers = runtime.NumCPU() * 4 // 默认工作协程数量
)

// GetGoroutinePool 获取全局 goroutine 池
func GetGoroutinePool() *GoroutinePool {
	poolOnce.Do(func() {
		globalPool = NewGoroutinePool(defaultWorkers)
	})
	return globalPool
}

// NewGoroutinePool 创建新的 goroutine 池
func NewGoroutinePool(maxWorkers int) *GoroutinePool {
	if maxWorkers <= 0 {
		maxWorkers = runtime.NumCPU() * 2
	}

	pool := &GoroutinePool{
		maxWorkers: maxWorkers,
		taskQueue:  make(chan func(), maxWorkers*10), // 缓冲区为工作协程数的10倍
	}

	// 启动工作协程
	for i := 0; i < maxWorkers; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	return pool
}

// worker 工作协程
func (p *GoroutinePool) worker() {
	defer p.wg.Done()

	for {
		select {
		case task, ok := <-p.taskQueue:
			if !ok {
				return // 通道关闭，退出
			}

			// 执行任务，捕获 panic
			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("[GoroutinePool] Worker panic: %v\n", r)
					}
				}()
				task()
			}()
		}
	}
}

// Submit 提交任务到池中
func (p *GoroutinePool) Submit(task func()) error {
	if atomic.LoadInt32(&p.stopped) == 1 {
		return fmt.Errorf("goroutine pool is stopped")
	}

	select {
	case p.taskQueue <- task:
		return nil
	case <-time.After(100 * time.Millisecond): // 100ms 超时
		return fmt.Errorf("goroutine pool is busy")
	}
}

// SubmitWithTimeout 提交任务到池中，带超时
func (p *GoroutinePool) SubmitWithTimeout(task func(), timeout time.Duration) error {
	if atomic.LoadInt32(&p.stopped) == 1 {
		return fmt.Errorf("goroutine pool is stopped")
	}

	select {
	case p.taskQueue <- task:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("goroutine pool submit timeout")
	}
}

// Stop 停止 goroutine 池
func (p *GoroutinePool) Stop() {
	p.once.Do(func() {
		atomic.StoreInt32(&p.stopped, 1)
		close(p.taskQueue)
		p.wg.Wait()
	})
}

// Size 返回池中工作协程数量
func (p *GoroutinePool) Size() int {
	return p.maxWorkers
}

// QueueSize 返回当前任务队列大小
func (p *GoroutinePool) QueueSize() int {
	return len(p.taskQueue)
}

// 异步执行函数的包装器
func GoSafe(fn func()) {
	pool := GetGoroutinePool()
	err := pool.Submit(fn)
	if err != nil {
		// 如果池满了，直接启动 goroutine（降级处理）
		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[GoSafe] Panic: %v\n", r)
				}
			}()
			fn()
		}()
	}
}

// 带超时的异步执行
func GoSafeWithTimeout(fn func(), timeout time.Duration) error {
	pool := GetGoroutinePool()
	return pool.SubmitWithTimeout(fn, timeout)
}

// 文件大小缓存相关
type fileSizeCache struct {
	size      int64
	timestamp time.Time
}

type accessibilityCache struct {
	accessible bool
	timestamp  time.Time
}

// 全局缓存
var (
	sizeCache   sync.Map
	accessCache sync.Map
	cacheTTL    = 5 * time.Minute
	accessTTL   = 2 * time.Minute
)

// 初始化函数
func init() {
	// 启动定期清理缓存的协程
	GoSafe(func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			cleanExpiredCache()
		}
	})
}

// 清理过期缓存
func cleanExpiredCache() {
	now := time.Now()

	// 清理文件大小缓存
	sizeCache.Range(func(key, value interface{}) bool {
		if cache, ok := value.(fileSizeCache); ok {
			if now.Sub(cache.timestamp) > cacheTTL {
				sizeCache.Delete(key)
			}
		}
		return true
	})

	// 清理可访问性缓存
	accessCache.Range(func(key, value interface{}) bool {
		if cache, ok := value.(accessibilityCache); ok {
			if now.Sub(cache.timestamp) > accessTTL {
				accessCache.Delete(key)
			}
		}
		return true
	})
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

// 获取请求来源
func GetRequestSource(r *http.Request) string {
	if r == nil {
		return ""
	}
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

// GetFileSize 发送HEAD请求获取文件大小（保持向后兼容）
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

// ExtensionMatcher 扩展名匹配器，用于优化扩展名匹配性能
type ExtensionMatcher struct {
	exactMatches    map[string][]*config.ExtensionRule // 精确匹配的扩展名
	wildcardRules   []*config.ExtensionRule            // 通配符规则
	hasRedirectRule bool                               // 是否有任何302跳转规则
}

// NewExtensionMatcher 创建扩展名匹配器
func NewExtensionMatcher(rules []config.ExtensionRule) *ExtensionMatcher {
	matcher := &ExtensionMatcher{
		exactMatches:  make(map[string][]*config.ExtensionRule),
		wildcardRules: make([]*config.ExtensionRule, 0),
	}

	for i := range rules {
		rule := &rules[i]

		// 处理阈值默认值
		if rule.SizeThreshold < 0 {
			rule.SizeThreshold = 0
		}
		if rule.MaxSize <= 0 {
			rule.MaxSize = 1<<63 - 1
		}

		// 检查是否有302跳转规则
		if rule.RedirectMode {
			matcher.hasRedirectRule = true
		}

		// 分类存储规则
		for _, ext := range rule.Extensions {
			if ext == "*" {
				matcher.wildcardRules = append(matcher.wildcardRules, rule)
			} else {
				if matcher.exactMatches[ext] == nil {
					matcher.exactMatches[ext] = make([]*config.ExtensionRule, 0, 1)
				}
				matcher.exactMatches[ext] = append(matcher.exactMatches[ext], rule)
			}
		}
	}

	// 预排序所有规则组
	for ext := range matcher.exactMatches {
		sortRulesByThreshold(matcher.exactMatches[ext])
	}
	sortRulesByThreshold(matcher.wildcardRules)

	return matcher
}

// sortRulesByThreshold 按阈值排序规则
func sortRulesByThreshold(rules []*config.ExtensionRule) {
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].SizeThreshold == rules[j].SizeThreshold {
			return rules[i].MaxSize > rules[j].MaxSize
		}
		return rules[i].SizeThreshold < rules[j].SizeThreshold
	})
}

// GetMatchingRules 获取匹配的规则
func (em *ExtensionMatcher) GetMatchingRules(ext string) []*config.ExtensionRule {
	// 先查找精确匹配
	if rules, exists := em.exactMatches[ext]; exists {
		return rules
	}
	// 返回通配符规则
	return em.wildcardRules
}

// HasRedirectRule 检查是否有任何302跳转规则
func (em *ExtensionMatcher) HasRedirectRule() bool {
	return em.hasRedirectRule
}

// IsTargetAccessible 检查目标URL是否可访问
func IsTargetAccessible(client *http.Client, targetURL string) bool {
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

func SafeFloat64(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case int32:
		return float64(val)
	default:
		return 0
	}
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

// ClearAccessibilityCache 清理可访问性缓存
func ClearAccessibilityCache() {
	count := 0
	accessCache.Range(func(key, value interface{}) bool {
		accessCache.Delete(key)
		count++
		return true
	})
	if count > 0 {
		log.Printf("[AccessibilityCache] 清理了 %d 个可访问性缓存项", count)
	}
}

// ClearFileSizeCache 清理文件大小缓存
func ClearFileSizeCache() {
	count := 0
	sizeCache.Range(func(key, value interface{}) bool {
		sizeCache.Delete(key)
		count++
		return true
	})
	if count > 0 {
		log.Printf("[FileSizeCache] 清理了 %d 个文件大小缓存项", count)
	}
}
