package service

import (
	"fmt"
	"log"
	"net/http"
	"proxy-go/internal/config"
	"proxy-go/internal/utils"
	"strings"
)

// RuleService 规则选择服务
type RuleService struct {
	cacheManager CacheManager
}

// CacheManager 缓存管理器接口
type CacheManager interface {
	GetExtensionMatcher(pathKey string, rules []config.ExtensionRule) *utils.ExtensionMatcher
}

// NewRuleService 创建规则选择服务
func NewRuleService(cacheManager CacheManager) *RuleService {
	return &RuleService{
		cacheManager: cacheManager,
	}
}

// SelectBestRule 选择最合适的规则
func (rs *RuleService) SelectBestRule(client *http.Client, pathConfig config.PathConfig, path string) (*config.ExtensionRule, bool, bool) {
	// 如果没有扩展名规则，返回nil
	if len(pathConfig.ExtRules) == 0 {
		return nil, false, false
	}

	// 提取扩展名
	ext := extractExtension(path)

	var matcher *utils.ExtensionMatcher

	// 尝试使用缓存管理器
	if rs.cacheManager != nil {
		pathKey := fmt.Sprintf("path_%p", &pathConfig)
		matcher = rs.cacheManager.GetExtensionMatcher(pathKey, pathConfig.ExtRules)
	} else {
		// 直接创建新的匹配器
		matcher = utils.NewExtensionMatcher(pathConfig.ExtRules)
	}

	// 获取匹配的规则
	matchingRules := matcher.GetMatchingRules(ext)
	if len(matchingRules) == 0 {
		return nil, false, false
	}

	// 获取文件大小
	contentLength, err := utils.GetFileSize(client, pathConfig.DefaultTarget+path)
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
				path, utils.FormatBytes(contentLength),
				utils.FormatBytes(rule.SizeThreshold), utils.FormatBytes(rule.MaxSize))

			// 检查目标是否可访问
			if utils.IsTargetAccessible(client, rule.Target+path) {
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

// RuleSelectionResult 规则选择结果
type RuleSelectionResult struct {
	Rule           *config.ExtensionRule
	Found          bool
	UsedAltTarget  bool
	TargetURL      string
	ShouldRedirect bool
}

// SelectRuleForRedirect 专门为302跳转优化的规则选择函数
func (rs *RuleService) SelectRuleForRedirect(client *http.Client, pathConfig config.PathConfig, path string) *RuleSelectionResult {
	result := &RuleSelectionResult{}

	// 快速检查：如果没有任何302跳转配置，直接返回
	if !pathConfig.RedirectMode && len(pathConfig.ExtRules) == 0 {
		return result
	}

	// 优先检查扩展名规则，即使根级别配置了302跳转
	if len(pathConfig.ExtRules) > 0 {
		// 尝试选择最佳规则（包括文件大小检测）
		if rule, found, usedAlt := rs.SelectBestRule(client, pathConfig, path); found && rule != nil && rule.RedirectMode {
			result.Rule = rule
			result.Found = found
			result.UsedAltTarget = usedAlt
			result.ShouldRedirect = true
			result.TargetURL = rule.Target
			return result
		}

		// 注意：这里不再进行"忽略大小"的回退匹配
		// 如果SelectBestRule没有找到合适的规则，说明：
		// 1. 扩展名不匹配，或者
		// 2. 扩展名匹配但文件大小不在配置范围内，或者
		// 3. 无法获取文件大小，或者
		// 4. 目标服务器不可访问
		// 在这些情况下，我们不应该强制使用扩展名规则
	}

	// 如果没有匹配的扩展名规则，且默认目标配置了302跳转，使用默认目标
	if pathConfig.RedirectMode {
		result.Found = true
		result.ShouldRedirect = true
		result.TargetURL = pathConfig.DefaultTarget
		return result
	}

	return result
}

// GetTargetURL 根据路径和配置决定目标URL
func (rs *RuleService) GetTargetURL(client *http.Client, r *http.Request, pathConfig config.PathConfig, path string) (string, bool) {
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
	rule, found, usedAlt := rs.SelectBestRule(client, pathConfig, path)
	if found && rule != nil {
		targetBase = rule.Target
		usedAltTarget = usedAlt
		log.Printf("[Route] %s -> %s (使用选中的规则)", path, targetBase)
	} else {
		// 如果无法获取文件大小，尝试使用扩展名直接匹配
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

// extractExtension 提取文件扩展名
func extractExtension(path string) string {
	lastDotIndex := strings.LastIndex(path, ".")
	if lastDotIndex > 0 && lastDotIndex < len(path)-1 {
		return strings.ToLower(path[lastDotIndex+1:])
	}
	return ""
}
