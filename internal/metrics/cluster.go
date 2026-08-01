package metrics

import (
	"context"
	"fmt"
	"log"
	"proxy-go/internal/models"
	"proxy-go/pkg/sync"
	"sort"
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
// 各节点每 30 分钟才上报一次, 所以 D1 里的数据在一个上报周期内根本不会变 ——
// 缓存时长必须按"数据多久可能变一次"来定, 而不是按仪表盘多久刷新一次。
// 取 5 分钟: 最坏让 peer 数据多滞后 5 分钟 (相对本身就有的 30 分钟滞后可忽略),
// 换来 D1 读取量下降一个数量级。Worker / D1 都是按请求与行数计费的, 不能被轮询打穿
const peerTTL = 5 * time.Minute

// ClusterInfo 描述本次聚合实际覆盖的范围, 随指标接口一起返回给前端
type ClusterInfo struct {
	NodeID     string   `json:"node_id"`    // 当前响应来自哪个节点
	Nodes      []string `json:"nodes"`      // 参与合计的全部节点 (含自己)
	Aggregated bool     `json:"aggregated"` // 是否真的做了跨节点合计
	Degraded   bool     `json:"degraded"`   // true 表示拉取其他节点失败, 当前只有本节点数据
	FetchedAt  int64    `json:"fetched_at"` // 其他节点数据的抓取时刻 (秒), 供前端显示数据新鲜度
}

// PeerStats 一次跨节点快照
//
// nodeRows 是 D1 里**全部节点**的原始行 (含本节点上一次上报的那份)。
// 只拉这一份原始数据, 在 Go 侧派生出三种视图, 避免为每种视图各打一次 D1:
//   - 其他节点合计 (peerPathStats): 过滤掉本节点后按 path 求和, 叠加本地实时值即为全局合计
//   - 各节点明细 (NodeSummaries): 按 node_id 分组, 本节点那份换成内存实时值
//   - 节点列表: 直接取 node_id 去重, 不必再查一次 DISTINCT
type PeerStats struct {
	nodeRows    []sync.PathStat
	statusCodes map[string]int64
	latency     map[string]int64
	degraded    bool
	fetchedAt   time.Time
}

// peerPathStats 折算出"除本节点外所有节点"按 path 的合计
func (s *PeerStats) peerPathStats() []sync.PathStat {
	self := sync.NodeID()
	merged := make(map[string]*sync.PathStat)
	for _, row := range s.nodeRows {
		if row.NodeID == self {
			continue
		}
		existing, ok := merged[row.Path]
		if !ok {
			cp := row
			cp.NodeID = ""
			merged[row.Path] = &cp
			continue
		}
		existing.RequestCount += row.RequestCount
		existing.ErrorCount += row.ErrorCount
		existing.BytesTransferred += row.BytesTransferred
		existing.Status2xx += row.Status2xx
		existing.Status3xx += row.Status3xx
		existing.Status4xx += row.Status4xx
		existing.Status5xx += row.Status5xx
		existing.CacheHits += row.CacheHits
		existing.CacheMisses += row.CacheMisses
		existing.BytesSaved += row.BytesSaved
		existing.TotalLatencyNs += row.TotalLatencyNs
		if row.LastAccessTime > existing.LastAccessTime {
			existing.LastAccessTime = row.LastAccessTime
		}
	}

	out := make([]sync.PathStat, 0, len(merged))
	for _, stat := range merged {
		out = append(out, *stat)
	}
	return out
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

// InvalidatePeerStats 丢弃 peer 缓存 (含最近成功快照), 下次读取强制重新拉 D1
//
// 重置统计后调用: 缓存里躺着其他节点的重置前旧值, 沿用会让仪表盘在 TTL 内
// 显示已被删除的数字。lastGood 一并丢弃 —— 它同样是重置前的数据, 不能再当兜底
func InvalidatePeerStats() {
	peerMu.Lock()
	peerCache = nil
	peerLastGood = nil
	peerMu.Unlock()
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
		fetchedAt:   time.Now(),
	}

	if !sync.IsEnabled() {
		// 单节点部署 (未接 D1) 不算降级: 本节点数据本来就是全部数据
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 一次请求拿回三类数据: 路径统计取各节点原始行 (仪表盘要明细),
	// 状态码与延迟分布取其他节点合计 (没有分节点展示需求)。
	// 其他节点合计 / 各节点明细 / 节点列表都从这一份派生, 不再各打一次
	bulk, err := sync.BulkLoadByNode(ctx)
	if err != nil {
		log.Printf("[Cluster] 拉取集群指标失败, 降级为仅本节点: %v", err)
		result.degraded = true
		return result
	}
	if bulk == nil {
		return result
	}

	result.nodeRows = bulk.PathStats
	for _, m := range bulk.StatusCodes {
		result.statusCodes[m.StatusCode] += m.Count
	}
	for _, m := range bulk.Latency {
		result.latency[m.Bucket] += m.Count
	}

	return result
}

// nodeIDs 从原始行里取出去重排序的节点列表, 本节点恒在内
//
// 节点集合数据驱动: D1 里出现过谁就有谁, 新节点首次上报后自动出现, 无需注册
func (s *PeerStats) nodeIDs() []string {
	self := sync.NodeID()
	seen := map[string]struct{}{self: {}}
	nodes := []string{self}
	for _, row := range s.nodeRows {
		if row.NodeID == "" {
			continue
		}
		if _, dup := seen[row.NodeID]; dup {
			continue
		}
		seen[row.NodeID] = struct{}{}
		nodes = append(nodes, row.NodeID)
	}
	sort.Strings(nodes)
	return nodes
}

// Info 汇报本次聚合覆盖范围
func (s *PeerStats) Info() ClusterInfo {
	nodes := s.nodeIDs()
	return ClusterInfo{
		NodeID:     sync.NodeID(),
		Nodes:      nodes,
		Aggregated: len(nodes) > 1,
		Degraded:   s.degraded,
		FetchedAt:  s.fetchedAt.Unix(),
	}
}

// MergePathStats 把本节点实时路径统计与其他节点的 D1 值按 path 相加
//
// 派生值必须重算而不是相加: 命中率按合并后的 hits/(hits+misses),
// 平均延迟按合并后的 总延迟/总请求数 (直接平均各节点的平均值会丢失请求数权重)
func (s *PeerStats) MergePathStats(local []models.PathMetricsJSON) []models.PathMetricsJSON {
	peerStats := s.peerPathStats()
	merged := make(map[string]*models.PathMetricsJSON, len(local)+len(peerStats))

	for i := range local {
		stat := local[i]
		merged[stat.Path] = &stat
	}

	for _, peer := range peerStats {
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
	self := sync.NodeID()
	var total int64
	for _, stat := range s.nodeRows {
		if stat.NodeID != self {
			total += stat.BytesTransferred
		}
	}
	return total
}

// TotalLatencyNs 其他节点的延迟累计 (纳秒) 与对应请求数
//
// 与 TotalBytes 同源, 用于把平均响应时间也做成全集群口径:
// 直接平均各节点的平均值会丢掉请求数权重, 必须回到 总延迟/总请求数
func (s *PeerStats) TotalLatencyNs() (latencyNs int64, requests int64) {
	self := sync.NodeID()
	for _, stat := range s.nodeRows {
		if stat.NodeID == self {
			continue
		}
		latencyNs += stat.TotalLatencyNs
		requests += stat.RequestCount
	}
	return latencyNs, requests
}

// MergeLatencyDistribution 合并延迟分布桶
func (s *PeerStats) MergeLatencyDistribution(local map[string]int64) map[string]int64 {
	return mergeCounters(local, s.latency)
}

// NodeSummary 单个节点的累计概览
type NodeSummary struct {
	NodeID           string  `json:"node_id"`
	IsCurrent        bool    `json:"is_current"` // 是否为响应本次请求的节点
	RequestCount     int64   `json:"request_count"`
	ErrorCount       int64   `json:"error_count"`
	ErrorRate        float64 `json:"error_rate"`
	BytesTransferred int64   `json:"bytes_transferred"`
	BytesSaved       int64   `json:"bytes_saved"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	AvgLatency       string  `json:"avg_latency"`
	// ReportedAt 该节点最后一次上报到 D1 的时刻 (秒); 本节点为 0, 因为用的是内存实时值
	ReportedAt int64 `json:"reported_at"`
}

// NodeSummaries 按节点汇总累计指标, 本节点用传入的内存实时值而非 D1 里的上一次上报
//
// 之所以要区分: 本节点最近一个上报周期内的流量还没进 D1, 直接用 D1 值会少一截,
// 且与页面上其他"本节点实时"口径对不上
func (s *PeerStats) NodeSummaries(local []models.PathMetricsJSON) []NodeSummary {
	self := sync.NodeID()

	// 派生值 (错误率 / 命中率 / 平均延迟) 一律先攒原始计数, 最后统一重算,
	// 中途累加派生值会得到错误结果
	type acc struct {
		summary     NodeSummary
		totalLatncy int64
		cacheHits   int64
		cacheMisses int64
		reportedAt  int64
	}
	nodes := make(map[string]*acc)

	ensure := func(nodeID string) *acc {
		if a, ok := nodes[nodeID]; ok {
			return a
		}
		a := &acc{summary: NodeSummary{NodeID: nodeID, IsCurrent: nodeID == self}}
		nodes[nodeID] = a
		return a
	}

	// 本节点恒存在, 即便还没有任何流量也要出现在列表里
	selfAcc := ensure(self)

	for _, row := range s.nodeRows {
		a := ensure(row.NodeID)
		if row.UpdatedAt > a.reportedAt {
			a.reportedAt = row.UpdatedAt
		}
		// 本节点跳过 D1 值, 下面改用内存实时值; 这里只借它的上报时间
		if row.NodeID == self {
			continue
		}
		a.summary.RequestCount += row.RequestCount
		a.summary.ErrorCount += row.ErrorCount
		a.summary.BytesTransferred += row.BytesTransferred
		a.summary.BytesSaved += row.BytesSaved
		a.totalLatncy += row.TotalLatencyNs
		a.cacheHits += row.CacheHits
		a.cacheMisses += row.CacheMisses
	}

	for _, stat := range local {
		selfAcc.summary.RequestCount += stat.RequestCount
		selfAcc.summary.ErrorCount += stat.ErrorCount
		selfAcc.summary.BytesTransferred += stat.BytesTransferred
		selfAcc.summary.BytesSaved += stat.BytesSaved
		selfAcc.totalLatncy += stat.TotalLatency
		selfAcc.cacheHits += stat.CacheHits
		selfAcc.cacheMisses += stat.CacheMisses
	}

	out := make([]NodeSummary, 0, len(nodes))
	for _, a := range nodes {
		n := a.summary
		if n.RequestCount > 0 {
			n.ErrorRate = float64(n.ErrorCount) / float64(n.RequestCount)
			avgMs := float64(a.totalLatncy) / float64(n.RequestCount) / float64(time.Millisecond)
			n.AvgLatency = fmt.Sprintf("%.2fms", avgMs)
		} else {
			n.AvgLatency = "0.00ms"
		}
		if total := a.cacheHits + a.cacheMisses; total > 0 {
			n.CacheHitRate = float64(a.cacheHits) / float64(total)
		}
		// 本节点是实时内存值, 不存在"上报时刻"这回事
		if !n.IsCurrent {
			n.ReportedAt = a.reportedAt / 1000 // D1 存的是毫秒
		}
		out = append(out, n)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

// AttachNodeBreakdown 给每条路径挂上该路径在各节点上的分布
//
// 回答"这个路径的流量到底是哪个节点扛的" —— 合计数字看不出偏斜,
// 而多节点部署最常见的问题恰恰是流量没按预期分流。
//
// resolvePath 把原始路径归并到与 aggregated 相同的键 (通常是配置的路径前缀),
// 两侧必须用同一套归并逻辑, 否则明细挂不到对应的合计行上
func (s *PeerStats) AttachNodeBreakdown(
	aggregated []models.PathMetricsJSON,
	local []models.PathMetricsJSON,
	resolvePath func(string) string,
) {
	self := sync.NodeID()
	if resolvePath == nil {
		resolvePath = func(p string) string { return p }
	}

	// path -> nodeID -> 该节点在此路径上的计数
	byPath := make(map[string]map[string]*models.PathNodeStat)
	ensure := func(path, nodeID string) *models.PathNodeStat {
		perNode, ok := byPath[path]
		if !ok {
			perNode = make(map[string]*models.PathNodeStat)
			byPath[path] = perNode
		}
		if s, ok := perNode[nodeID]; ok {
			return s
		}
		s := &models.PathNodeStat{NodeID: nodeID, IsCurrent: nodeID == self}
		perNode[nodeID] = s
		return s
	}

	for _, row := range s.nodeRows {
		if row.NodeID == self {
			continue // 本节点用内存实时值
		}
		n := ensure(resolvePath(row.Path), row.NodeID)
		n.RequestCount += row.RequestCount
		n.ErrorCount += row.ErrorCount
		n.BytesTransferred += row.BytesTransferred
		n.AddLatency(row.TotalLatencyNs)
		n.CacheHits += row.CacheHits
		n.CacheMisses += row.CacheMisses
	}

	for _, stat := range local {
		n := ensure(resolvePath(stat.Path), self)
		n.RequestCount += stat.RequestCount
		n.ErrorCount += stat.ErrorCount
		n.BytesTransferred += stat.BytesTransferred
		n.AddLatency(stat.TotalLatency)
		n.CacheHits += stat.CacheHits
		n.CacheMisses += stat.CacheMisses
	}

	for i := range aggregated {
		perNode := byPath[aggregated[i].Path]
		if len(perNode) == 0 {
			aggregated[i].Nodes = []models.PathNodeStat{}
			continue
		}
		list := make([]models.PathNodeStat, 0, len(perNode))
		for _, n := range perNode {
			n.Finalize()
			list = append(list, *n)
		}
		sort.Slice(list, func(a, b int) bool {
			if list[a].RequestCount != list[b].RequestCount {
				return list[a].RequestCount > list[b].RequestCount
			}
			return list[a].NodeID < list[b].NodeID
		})
		aggregated[i].Nodes = list
	}
}
