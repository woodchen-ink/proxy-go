package metrics

import (
	"context"
	"fmt"
	"log"
	"proxy-go/internal/models"
	"proxy-go/pkg/sync"
	gosync "sync"
	"time"
)

// 跨节点累计统计聚合
//
// 背景: 每个节点只在自己内存里累计指标, 每 30 分钟把绝对值按 node_id 写进 D1。
// 任何一个节点的后台都要能看到全集群合计, 而管理员登录时只会落到其中一个节点上。
//
// 口径 = D1 中其他节点的合计 + 本节点内存实时值
//
//	其他节点走 D1 (最多滞后一个上报周期), 本节点走内存 (实时), 两边不重叠也不遗漏。
//	刻意不用"D1 全节点合计"直接展示: 那样本节点最近一个周期的流量会缺失。
//
// 节点集合由 D1 里出现过的 node_id 决定, 新节点第一次上报后自动进入合计, 无需注册或改配置。

// peerTTL 其他节点数据的缓存时长
//
// 上游数据本身就是 30 分钟粒度, 没必要跟着仪表盘轮询打 D1;
// 同时避免多个管理页并发刷新时对 worker 产生放大请求
const peerTTL = 60 * time.Second

// ClusterInfo 描述本次聚合实际覆盖的范围, 随指标接口一起返回给前端
type ClusterInfo struct {
	NodeID     string   `json:"node_id"`     // 当前响应来自哪个节点
	Nodes      []string `json:"nodes"`       // 参与合计的全部节点 (含自己)
	Aggregated bool     `json:"aggregated"`  // 是否真的做了跨节点合计
	Degraded   bool     `json:"degraded"`    // true 表示拉取其他节点失败, 当前只有本节点数据
}

// PeerStats 其他节点累计统计的一次快照 (不含本节点)
type PeerStats struct {
	pathStats   []sync.PathStat
	statusCodes map[string]int64
	latency     map[string]int64
	nodes       []string
	degraded    bool
	fetchedAt   time.Time
}

var (
	peerMu gosync.Mutex
	// peerCache 当前对外快照 (可能是带 degraded 标记的陈旧副本)
	peerCache *PeerStats
	// peerLastGood 最近一次成功拉取的结果, 供后续连续失败时持续兜底
	peerLastGood *PeerStats
)

// GetPeerStats 取其他节点的累计统计, TTL 内复用缓存
//
// 永不返回 nil: D1 未启用或拉取失败时返回空快照 (degraded=true),
// 调用方照常只展示本节点数据, 不会因为同步链路故障而让整个仪表盘失败
func GetPeerStats() *PeerStats {
	peerMu.Lock()
	defer peerMu.Unlock()

	if peerCache != nil && time.Since(peerCache.fetchedAt) < peerTTL {
		return peerCache
	}

	fresh := fetchPeerStats()
	if !fresh.degraded {
		peerLastGood = fresh
	}
	peerCache = resolvePeerSnapshot(fresh, peerLastGood)
	return peerCache
}

// resolvePeerSnapshot 决定本轮对外快照: 成功用新结果, 失败则沿用最近一次成功的数据并标记降级
//
// 拉取失败时继续沿用旧数据, 避免 D1 抖动让面板上的数字瞬间掉一半; 累计量本来变化就慢,
// 用上一周期的值远好过直接归零。兜底必须基于"最近一次成功"而不是"上一次对外快照",
// 否则连续失败第二次就会把已经降级的空快照当成有效值接着用, 数字仍然归零。
//
// 复制成新对象再打标记, 不就地改 lastGood: 前端据此提示数据陈旧, 且已持有旧指针的
// goroutine 仍在读它, 就地改字段构成数据竞争
func resolvePeerSnapshot(fresh, lastGood *PeerStats) *PeerStats {
	if !fresh.degraded || lastGood == nil || lastGood.degraded {
		return fresh
	}
	stale := *lastGood
	stale.degraded = true
	stale.fetchedAt = time.Now()
	return &stale
}

// fetchPeerStats 实际发起 D1 查询, 任一子查询失败即整体标记为降级
func fetchPeerStats() *PeerStats {
	result := &PeerStats{
		statusCodes: map[string]int64{},
		latency:     map[string]int64{},
		nodes:       []string{},
		fetchedAt:   time.Now(),
	}

	if !sync.IsEnabled() {
		// 单节点部署 (未接 D1) 不算降级: 本节点数据本来就是全部数据
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pathStats, err := sync.LoadPeerPathStats(ctx)
	if err != nil {
		log.Printf("[Cluster] 拉取其他节点路径统计失败, 降级为仅本节点: %v", err)
		result.degraded = true
		return result
	}
	result.pathStats = pathStats

	if statusCodes, err := sync.LoadPeerStatusCodes(ctx); err != nil {
		log.Printf("[Cluster] 拉取其他节点状态码失败: %v", err)
		result.degraded = true
	} else if statusCodes != nil {
		result.statusCodes = statusCodes
	}

	if latency, err := sync.LoadPeerLatencyDistribution(ctx); err != nil {
		log.Printf("[Cluster] 拉取其他节点延迟分布失败: %v", err)
		result.degraded = true
	} else if latency != nil {
		result.latency = latency
	}

	if nodes, err := sync.ListMetricNodes(ctx); err != nil {
		log.Printf("[Cluster] 读取节点列表失败: %v", err)
	} else {
		result.nodes = nodes
	}

	return result
}

// Info 汇报本次聚合覆盖范围
func (s *PeerStats) Info() ClusterInfo {
	self := sync.NodeID()
	nodes := s.nodes
	if len(nodes) == 0 {
		nodes = []string{self}
	}
	return ClusterInfo{
		NodeID:     self,
		Nodes:      nodes,
		Aggregated: len(s.pathStats) > 0 || len(nodes) > 1,
		Degraded:   s.degraded,
	}
}

// MergePathStats 把本节点实时路径统计与其他节点的 D1 值按 path 相加
//
// 派生值必须重算而不是相加: 命中率按合并后的 hits/(hits+misses),
// 平均延迟按合并后的 总延迟/总请求数 (直接平均各节点的平均值会丢失请求数权重)
func (s *PeerStats) MergePathStats(local []models.PathMetricsJSON) []models.PathMetricsJSON {
	merged := make(map[string]*models.PathMetricsJSON, len(local)+len(s.pathStats))

	for i := range local {
		stat := local[i]
		merged[stat.Path] = &stat
	}

	for _, peer := range s.pathStats {
		existing, ok := merged[peer.Path]
		if !ok {
			merged[peer.Path] = &models.PathMetricsJSON{
				Path:             peer.Path,
				RequestCount:     peer.RequestCount,
				ErrorCount:       peer.ErrorCount,
				BytesTransferred: peer.BytesTransferred,
				TotalLatency:     peer.TotalLatencyNs,
				LastAccessTime:   peer.LastAccessTime,
				Status2xx:        peer.Status2xx,
				Status3xx:        peer.Status3xx,
				Status4xx:        peer.Status4xx,
				Status5xx:        peer.Status5xx,
				CacheHits:        peer.CacheHits,
				CacheMisses:      peer.CacheMisses,
				BytesSaved:       peer.BytesSaved,
			}
			continue
		}

		existing.RequestCount += peer.RequestCount
		existing.ErrorCount += peer.ErrorCount
		existing.BytesTransferred += peer.BytesTransferred
		existing.TotalLatency += peer.TotalLatencyNs
		existing.Status2xx += peer.Status2xx
		existing.Status3xx += peer.Status3xx
		existing.Status4xx += peer.Status4xx
		existing.Status5xx += peer.Status5xx
		existing.CacheHits += peer.CacheHits
		existing.CacheMisses += peer.CacheMisses
		existing.BytesSaved += peer.BytesSaved
		if peer.LastAccessTime > existing.LastAccessTime {
			existing.LastAccessTime = peer.LastAccessTime
		}
	}

	result := make([]models.PathMetricsJSON, 0, len(merged))
	for _, stat := range merged {
		FinalizeDerivedStats(stat)
		result = append(result, *stat)
	}
	return result
}

// FinalizeDerivedStats 由合并后的原始计数重算命中率与平均延迟
//
// 任何对 PathMetricsJSON 做过累加的地方都必须调用一次: 命中率和平均延迟是派生值,
// 直接相加或沿用其中一方的值都会得到错误结果
func FinalizeDerivedStats(stat *models.PathMetricsJSON) {
	if totalCache := stat.CacheHits + stat.CacheMisses; totalCache > 0 {
		stat.CacheHitRate = float64(stat.CacheHits) / float64(totalCache)
	} else {
		stat.CacheHitRate = 0
	}

	if stat.RequestCount > 0 {
		avgMs := float64(stat.TotalLatency) / float64(stat.RequestCount) / float64(time.Millisecond)
		stat.AvgLatency = fmt.Sprintf("%.2fms", avgMs)
	} else {
		stat.AvgLatency = "0.00ms"
	}
}

// MergeCounters 把本节点的计数 map 与其他节点的对应 map 相加 (状态码 / 延迟分布同构)
func mergeCounters(local, peer map[string]int64) map[string]int64 {
	merged := make(map[string]int64, len(local)+len(peer))
	for k, v := range local {
		merged[k] = v
	}
	for k, v := range peer {
		merged[k] += v
	}
	return merged
}

// MergeStatusCodes 合并状态码计数
func (s *PeerStats) MergeStatusCodes(local map[string]int64) map[string]int64 {
	return mergeCounters(local, s.statusCodes)
}

// TotalBytes 其他节点的总传输字节数
//
// 全局总流量没有独立的 D1 表, 由各节点的逐路径字节数求和得到; 与仪表盘按路径展示的合计同源
func (s *PeerStats) TotalBytes() int64 {
	var total int64
	for _, stat := range s.pathStats {
		total += stat.BytesTransferred
	}
	return total
}

// TotalLatencyNs 其他节点的延迟累计 (纳秒) 与对应请求数
//
// 与 TotalBytes 同源, 用于把平均响应时间也做成全集群口径:
// 直接平均各节点的平均值会丢掉请求数权重, 必须回到 总延迟/总请求数
func (s *PeerStats) TotalLatencyNs() (latencyNs int64, requests int64) {
	for _, stat := range s.pathStats {
		latencyNs += stat.TotalLatencyNs
		requests += stat.RequestCount
	}
	return latencyNs, requests
}

// MergeLatencyDistribution 合并延迟分布桶
func (s *PeerStats) MergeLatencyDistribution(local map[string]int64) map[string]int64 {
	return mergeCounters(local, s.latency)
}
