package metrics

import (
	"fmt"
	"net/url"
	"proxy-go/internal/models"
	"strings"
	"sync"
	"sync/atomic"
)

// 分片计数容器: 引用来源 / 路径统计共用的分片 map, 状态码与延迟分桶

// extractRefererHost 从 Referer 头取出小写 host (不含端口); 解析失败返回空串
func extractRefererHost(referer string) string {
	if referer == "" {
		return ""
	}
	u, err := url.Parse(referer)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// 优化的状态码统计结构
type StatusCodeStats struct {
	mu    sync.RWMutex
	stats map[int]*int64 // 预分配常见状态码
}

// 优化的延迟分布统计
type LatencyBuckets struct {
	lt10ms     int64
	ms10_50    int64
	ms50_200   int64
	ms200_1000 int64
	gt1s       int64
}

// 优化的引用来源统计（使用分片减少锁竞争）
type RefererStats struct {
	shards []*RefererShard
	mask   uint64
}

type RefererShard struct {
	mu   sync.RWMutex
	data map[string]*models.PathMetrics
}

func NewRefererStats() *RefererStats {
	rs := &RefererStats{
		shards: make([]*RefererShard, refererShardCount),
		mask:   refererShardCount - 1,
	}
	for i := 0; i < refererShardCount; i++ {
		rs.shards[i] = &RefererShard{
			data: make(map[string]*models.PathMetrics),
		}
	}
	return rs
}

func (rs *RefererStats) hash(key string) uint64 {
	// 简单的字符串哈希函数
	var h uint64 = 14695981039346656037
	for _, b := range []byte(key) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

func (rs *RefererStats) getShard(key string) *RefererShard {
	return rs.shards[rs.hash(key)&rs.mask]
}

func (rs *RefererStats) Load(key string) (*models.PathMetrics, bool) {
	shard := rs.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	val, ok := shard.data[key]
	return val, ok
}

func (rs *RefererStats) Store(key string, value *models.PathMetrics) {
	shard := rs.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	shard.data[key] = value
}

func (rs *RefererStats) Range(f func(key string, value *models.PathMetrics) bool) {
	for _, shard := range rs.shards {
		shard.mu.RLock()
		for k, v := range shard.data {
			if !f(k, v) {
				shard.mu.RUnlock()
				return
			}
		}
		shard.mu.RUnlock()
	}
}

func (rs *RefererStats) Cleanup(cutoff int64) int {
	deleted := 0
	for _, shard := range rs.shards {
		shard.mu.Lock()
		for k, v := range shard.data {
			if v.LastAccessTime.Load() < cutoff {
				delete(shard.data, k)
				deleted++
			}
		}
		shard.mu.Unlock()
	}
	return deleted
}

func NewStatusCodeStats() *StatusCodeStats {
	s := &StatusCodeStats{
		stats: make(map[int]*int64),
	}
	// 预分配常见状态码
	commonCodes := []int{200, 201, 204, 301, 302, 304, 400, 401, 403, 404, 429, 500, 502, 503, 504}
	for _, code := range commonCodes {
		counter := new(int64)
		s.stats[code] = counter
	}
	return s
}

func (s *StatusCodeStats) Increment(code int) {
	s.mu.RLock()
	if counter, exists := s.stats[code]; exists {
		s.mu.RUnlock()
		atomic.AddInt64(counter, 1)
		return
	}
	s.mu.RUnlock()

	// 需要创建新的计数器
	s.mu.Lock()
	defer s.mu.Unlock()
	if counter, exists := s.stats[code]; exists {
		atomic.AddInt64(counter, 1)
	} else {
		counter := new(int64)
		*counter = 1
		s.stats[code] = counter
	}
}

func (s *StatusCodeStats) GetStats() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]int64)
	for code, counter := range s.stats {
		result[fmt.Sprintf("%d", code)] = atomic.LoadInt64(counter)
	}
	return result
}
