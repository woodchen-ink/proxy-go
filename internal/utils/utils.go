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

// RuleSelectionResult 规则选择结果，用于缓存和传递结果
type RuleSelectionResult struct {
	Rule           *config.ExtensionRule
	Found          bool
	UsedAltTarget  bool
	TargetURL      string
	ShouldRedirect bool
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

// extractExtension 提取文件扩展名（优化版本）
func extractExtension(path string) string {
	lastDotIndex := strings.LastIndex(path, ".")
	if lastDotIndex > 0 && lastDotIndex < len(path)-1 {
		return strings.ToLower(path[lastDotIndex+1:])
	}
	return ""
}

// SelectBestRule 根据文件大小和扩展名选择最合适的规则（优化版本）
// 返回值: (选中的规则, 是否找到匹配的规则, 是否使用了备用目标)
func SelectBestRule(client *http.Client, pathConfig config.PathConfig, path string) (*config.ExtensionRule, bool, bool) {
	// 如果没有扩展名规则，返回nil
	if len(pathConfig.ExtRules) == 0 {
		return nil, false, false
	}

	// 提取扩展名
	ext := extractExtension(path)

	// 创建扩展名匹配器（可以考虑缓存这个匹配器）
	matcher := NewExtensionMatcher(pathConfig.ExtRules)

	// 获取匹配的规则
	matchingRules := matcher.GetMatchingRules(ext)
	if len(matchingRules) == 0 {
		return nil, false, false
	}

	// 获取文件大小
	contentLength, err := GetFileSize(client, pathConfig.DefaultTarget+path)
	if err != nil {
		log.Printf("[SelectRule] %s -> 获取文件大小出错: %v", path, err)
		// 如果无法获取文件大小，返回第一个匹配的规则
		if len(matchingRules) > 0 {
			log.Printf("[SelectRule] %s -> 基于扩展名直接匹配规则", path)
			return matchingRules[0], true, true
		}
		return nil, false, false
	}

	// 根据文件大小找出最匹配的规则（规则已经预排序）
	for _, rule := range matchingRules {
		// 检查文件大小是否在阈值范围内
		if contentLength >= rule.SizeThreshold && contentLength <= rule.MaxSize {
			// 找到匹配的规则
			log.Printf("[SelectRule] %s -> 选中规则 (文件大小: %s, 在区间 %s 到 %s 之间)",
				path, FormatBytes(contentLength),
				FormatBytes(rule.SizeThreshold), FormatBytes(rule.MaxSize))

			// 检查目标是否可访问（使用带缓存的检查）
			if isTargetAccessible(client, rule.Target+path) {
				return rule, true, true
			} else {
				log.Printf("[SelectRule] %s -> 规则目标不可访问，继续查找", path)
				// 继续查找下一个匹配的规则
				continue
			}
		}
	}

	// 没有找到合适的规则
	return nil, false, false
}

// SelectRuleForRedirect 专门为302跳转优化的规则选择函数
func SelectRuleForRedirect(client *http.Client, pathConfig config.PathConfig, path string) *RuleSelectionResult {
	result := &RuleSelectionResult{}

	// 快速检查：如果没有任何302跳转配置，直接返回
	if !pathConfig.RedirectMode && len(pathConfig.ExtRules) == 0 {
		return result
	}

	// 如果默认目标配置了302跳转，优先使用
	if pathConfig.RedirectMode {
		result.Found = true
		result.ShouldRedirect = true
		result.TargetURL = pathConfig.DefaultTarget
		return result
	}

	// 检查扩展名规则
	if len(pathConfig.ExtRules) > 0 {
		ext := extractExtension(path)
		matcher := NewExtensionMatcher(pathConfig.ExtRules)

		// 快速检查：如果没有任何302跳转规则，跳过复杂逻辑
		if !matcher.HasRedirectRule() {
			return result
		}

		// 尝试选择最佳规则
		if rule, found, usedAlt := SelectBestRule(client, pathConfig, path); found && rule != nil && rule.RedirectMode {
			result.Rule = rule
			result.Found = found
			result.UsedAltTarget = usedAlt
			result.ShouldRedirect = true
			result.TargetURL = rule.Target
			return result
		}

		// 回退到简单的扩展名匹配
		if rule, found := pathConfig.GetProcessedExtRule(ext); found && rule.RedirectMode {
			result.Rule = rule
			result.Found = found
			result.UsedAltTarget = true
			result.ShouldRedirect = true
			result.TargetURL = rule.Target
			return result
		}

		// 检查通配符规则
		if rule, found := pathConfig.GetProcessedExtRule("*"); found && rule.RedirectMode {
			result.Rule = rule
			result.Found = found
			result.UsedAltTarget = true
			result.ShouldRedirect = true
			result.TargetURL = rule.Target
			return result
		}
	}

	return result
}

// GetTargetURL 根据路径和配置决定目标URL（优化版本）
func GetTargetURL(client *http.Client, r *http.Request, pathConfig config.PathConfig, path string) (string, bool) {
	// 默认使用默认目标
	targetBase := pathConfig.DefaultTarget
	usedAltTarget := false

	// 如果没有扩展名规则，直接返回默认目标
	if len(pathConfig.ExtRules) == 0 {
		ext := extractExtension(path)
		if ext == "" {
			log.Printf("[Route] %s -> %s (无扩展名)", path, targetBase)
		}
		return targetBase, false
	}

	// 使用新的统一规则选择逻辑
	rule, found, usedAlt := SelectBestRule(client, pathConfig, path)
	if found && rule != nil {
		targetBase = rule.Target
		usedAltTarget = usedAlt
		log.Printf("[Route] %s -> %s (使用选中的规则)", path, targetBase)
	} else {
		// 如果无法获取文件大小，尝试使用扩展名直接匹配（优化点）
		ext := extractExtension(path)
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
