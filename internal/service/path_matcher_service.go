package service

import (
	"proxy-go/internal/config"
	"sort"
	"strings"
)

// PathMatchResult 路径匹配结果
type PathMatchResult struct {
	Matched       bool
	MatchedPrefix string
	PathConfig    config.PathConfig
	TargetPath    string
}

// PathMatcher 路径匹配器
type PathMatcher struct {
	prefixes []string
	configs  map[string]config.PathConfig
}

type PathMatcherService struct {
	matcher *PathMatcher
}

func NewPathMatcherService(pathMap map[string]config.PathConfig) *PathMatcherService {
	return &PathMatcherService{
		matcher: newPathMatcher(pathMap),
	}
}

// newPathMatcher 创建新的路径匹配器
func newPathMatcher(pathMap map[string]config.PathConfig) *PathMatcher {
	pm := &PathMatcher{
		prefixes: make([]string, 0, len(pathMap)),
		configs:  make(map[string]config.PathConfig, len(pathMap)),
	}

	// 按长度降序排列前缀，确保最长匹配优先
	for prefix, cfg := range pathMap {
		pm.prefixes = append(pm.prefixes, prefix)
		pm.configs[prefix] = cfg
	}

	// 按长度降序排列
	sort.Slice(pm.prefixes, func(i, j int) bool {
		return len(pm.prefixes[i]) > len(pm.prefixes[j])
	})

	return pm
}

// MatchPath 匹配请求路径
func (s *PathMatcherService) MatchPath(requestPath string) *PathMatchResult {
	matchedPrefix, pathConfig, matched := s.matcher.match(requestPath)
	
	if !matched {
		return &PathMatchResult{
			Matched: false,
		}
	}
	
	// 计算目标路径
	targetPath := strings.TrimPrefix(requestPath, matchedPrefix)
	if !strings.HasPrefix(targetPath, "/") {
		targetPath = "/" + targetPath
	}
	
	return &PathMatchResult{
		Matched:       true,
		MatchedPrefix: matchedPrefix,
		PathConfig:    pathConfig,
		TargetPath:    targetPath,
	}
}

// UpdatePaths 更新路径配置
func (s *PathMatcherService) UpdatePaths(pathMap map[string]config.PathConfig) {
	s.matcher.update(pathMap)
}

// match 匹配路径
func (pm *PathMatcher) match(path string) (string, config.PathConfig, bool) {
	for _, prefix := range pm.prefixes {
		if strings.HasPrefix(path, prefix) {
			return prefix, pm.configs[prefix], true
		}
	}
	return "", config.PathConfig{}, false
}

// update 更新路径匹配器
func (pm *PathMatcher) update(pathMap map[string]config.PathConfig) {
	pm.prefixes = pm.prefixes[:0] // 重置slice但保留capacity
	pm.configs = make(map[string]config.PathConfig, len(pathMap))
	
	// 重新填充
	for prefix, cfg := range pathMap {
		pm.prefixes = append(pm.prefixes, prefix)
		pm.configs[prefix] = cfg
	}
	
	// 重新排序
	sort.Slice(pm.prefixes, func(i, j int) bool {
		return len(pm.prefixes[i]) > len(pm.prefixes[j])
	})
}

// GetAllPaths 获取所有配置的路径
func (s *PathMatcherService) GetAllPaths() map[string]config.PathConfig {
	result := make(map[string]config.PathConfig)
	for prefix, cfg := range s.matcher.configs {
		result[prefix] = cfg
	}
	return result
}

// HasPath 检查是否存在指定路径
func (s *PathMatcherService) HasPath(path string) bool {
	_, exists := s.matcher.configs[path]
	return exists
}

// GetPathConfig 获取指定路径的配置
func (s *PathMatcherService) GetPathConfig(path string) (config.PathConfig, bool) {
	cfg, exists := s.matcher.configs[path]
	return cfg, exists
}