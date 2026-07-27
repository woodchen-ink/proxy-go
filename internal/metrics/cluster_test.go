package metrics

import (
	"proxy-go/internal/models"
	"proxy-go/pkg/sync"
	"testing"
	"time"
)

// ms 把毫秒数换成纳秒, 让用例里的延迟值可读
func ms(v int64) int64 { return v * int64(time.Millisecond) }

// TestMergePathStatsSumsOverlappingPaths 同一路径在两个节点上的计数应相加
func TestMergePathStatsSumsOverlappingPaths(t *testing.T) {
	peers := &PeerStats{
		nodeRows: []sync.PathStat{{
			NodeID:           "peer-a",
			Path:             "/img",
			RequestCount:     300,
			ErrorCount:       30,
			BytesTransferred: 3000,
			Status2xx:        270,
			Status4xx:        30,
			CacheHits:        150,
			CacheMisses:      150,
			BytesSaved:       1500,
			TotalLatencyNs:   ms(300 * 40), // 平均 40ms
			LastAccessTime:   500,
		}},
	}

	local := []models.PathMetricsJSON{{
		Path:             "/img",
		RequestCount:     100,
		ErrorCount:       10,
		BytesTransferred: 1000,
		Status2xx:        90,
		Status4xx:        10,
		CacheHits:        90,
		CacheMisses:      10,
		BytesSaved:       500,
		TotalLatency:     ms(100 * 20), // 平均 20ms
		LastAccessTime:   900,
	}}

	got := peers.MergePathStats(local)
	if len(got) != 1 {
		t.Fatalf("期望合并成 1 条路径, 实际 %d 条", len(got))
	}

	s := got[0]
	if s.RequestCount != 400 {
		t.Errorf("请求数应为 400, 实际 %d", s.RequestCount)
	}
	if s.ErrorCount != 40 {
		t.Errorf("错误数应为 40, 实际 %d", s.ErrorCount)
	}
	if s.BytesTransferred != 4000 {
		t.Errorf("流量应为 4000, 实际 %d", s.BytesTransferred)
	}
	if s.BytesSaved != 2000 {
		t.Errorf("节省流量应为 2000, 实际 %d", s.BytesSaved)
	}
	if s.Status2xx != 360 || s.Status4xx != 40 {
		t.Errorf("状态码未正确相加: 2xx=%d 4xx=%d", s.Status2xx, s.Status4xx)
	}
	// 最后访问时间取两边较新的
	if s.LastAccessTime != 900 {
		t.Errorf("最后访问时间应取较新的 900, 实际 %d", s.LastAccessTime)
	}
	// 命中率按合并后的 240/(240+160) 重算, 而不是两个节点命中率的平均
	if s.CacheHitRate != 0.6 {
		t.Errorf("缓存命中率应为 0.6, 实际 %v", s.CacheHitRate)
	}
	// 加权平均延迟 = (100*20 + 300*40) / 400 = 35ms, 不是 (20+40)/2 = 30ms
	if s.AvgLatency != "35.00ms" {
		t.Errorf("加权平均延迟应为 35.00ms, 实际 %s", s.AvgLatency)
	}
}

// TestMergePathStatsKeepsDisjointPaths 只在某一侧出现的路径都要保留
func TestMergePathStatsKeepsDisjointPaths(t *testing.T) {
	peers := &PeerStats{
		nodeRows: []sync.PathStat{{
			NodeID:           "peer-a",
			Path:           "/only-peer",
			RequestCount:   10,
			TotalLatencyNs: ms(10 * 5),
		}},
	}
	local := []models.PathMetricsJSON{{
		Path:         "/only-local",
		RequestCount: 20,
		TotalLatency: ms(20 * 5),
	}}

	got := peers.MergePathStats(local)
	if len(got) != 2 {
		t.Fatalf("期望保留 2 条路径, 实际 %d 条", len(got))
	}

	byPath := map[string]models.PathMetricsJSON{}
	for _, s := range got {
		byPath[s.Path] = s
	}
	if byPath["/only-peer"].RequestCount != 10 {
		t.Errorf("仅存在于其他节点的路径丢失或数值错误: %+v", byPath["/only-peer"])
	}
	if byPath["/only-local"].RequestCount != 20 {
		t.Errorf("仅存在于本节点的路径丢失或数值错误: %+v", byPath["/only-local"])
	}
}

// TestMergePathStatsDoesNotMutateLocal 合并不能改动传入的本地切片
func TestMergePathStatsDoesNotMutateLocal(t *testing.T) {
	peers := &PeerStats{
		nodeRows: []sync.PathStat{{NodeID: "peer-a", Path: "/img", RequestCount: 300}},
	}
	local := []models.PathMetricsJSON{{Path: "/img", RequestCount: 100}}

	peers.MergePathStats(local)

	if local[0].RequestCount != 100 {
		t.Errorf("本地统计被合并逻辑改写了: %d", local[0].RequestCount)
	}
}

// TestMergePathStatsWithNoPeers 单节点场景下结果等价于本地值
func TestMergePathStatsWithNoPeers(t *testing.T) {
	peers := &PeerStats{}
	local := []models.PathMetricsJSON{{
		Path:         "/img",
		RequestCount: 100,
		CacheHits:    75,
		CacheMisses:  25,
		TotalLatency: ms(100 * 12),
	}}

	got := peers.MergePathStats(local)
	if len(got) != 1 {
		t.Fatalf("期望 1 条, 实际 %d 条", len(got))
	}
	if got[0].RequestCount != 100 {
		t.Errorf("请求数应保持 100, 实际 %d", got[0].RequestCount)
	}
	if got[0].CacheHitRate != 0.75 {
		t.Errorf("命中率应为 0.75, 实际 %v", got[0].CacheHitRate)
	}
	if got[0].AvgLatency != "12.00ms" {
		t.Errorf("平均延迟应为 12.00ms, 实际 %s", got[0].AvgLatency)
	}
}

// TestFinalizeDerivedStatsZeroRequests 零请求时派生值不能除零
func TestFinalizeDerivedStatsZeroRequests(t *testing.T) {
	stat := &models.PathMetricsJSON{Path: "/idle"}
	FinalizeDerivedStats(stat)

	if stat.AvgLatency != "0.00ms" {
		t.Errorf("零请求平均延迟应为 0.00ms, 实际 %s", stat.AvgLatency)
	}
	if stat.CacheHitRate != 0 {
		t.Errorf("零缓存请求命中率应为 0, 实际 %v", stat.CacheHitRate)
	}
}

// TestMergeCounters 状态码 / 延迟桶的合并按 key 相加, 单边 key 保留
func TestMergeCounters(t *testing.T) {
	peers := &PeerStats{
		statusCodes: map[string]int64{"200": 500, "404": 20, "500": 3},
		latency:     map[string]int64{"lt10ms": 100, "gt1s": 2},
	}

	codes := peers.MergeStatusCodes(map[string]int64{"200": 100, "404": 5})
	if codes["200"] != 600 {
		t.Errorf("200 应为 600, 实际 %d", codes["200"])
	}
	if codes["404"] != 25 {
		t.Errorf("404 应为 25, 实际 %d", codes["404"])
	}
	if codes["500"] != 3 {
		t.Errorf("仅其他节点有的 500 应保留为 3, 实际 %d", codes["500"])
	}

	buckets := peers.MergeLatencyDistribution(map[string]int64{"lt10ms": 50, "50-200ms": 7})
	if buckets["lt10ms"] != 150 {
		t.Errorf("lt10ms 应为 150, 实际 %d", buckets["lt10ms"])
	}
	if buckets["50-200ms"] != 7 {
		t.Errorf("仅本节点有的桶应保留为 7, 实际 %d", buckets["50-200ms"])
	}
	if buckets["gt1s"] != 2 {
		t.Errorf("仅其他节点有的桶应保留为 2, 实际 %d", buckets["gt1s"])
	}
}

// TestMergeCountersDoesNotMutateInput 合并不能改写传入的本地 map
func TestMergeCountersDoesNotMutateInput(t *testing.T) {
	peers := &PeerStats{statusCodes: map[string]int64{"200": 500}}
	local := map[string]int64{"200": 100}

	peers.MergeStatusCodes(local)

	if local["200"] != 100 {
		t.Errorf("本地状态码 map 被改写: %d", local["200"])
	}
}

// TestPeerTotals 其他节点的总流量与加权延迟分母由逐路径累加得到
func TestPeerTotals(t *testing.T) {
	peers := &PeerStats{
		nodeRows: []sync.PathStat{
			{NodeID: "peer-a", Path: "/a", BytesTransferred: 1000, RequestCount: 10, TotalLatencyNs: ms(10 * 30)},
			{NodeID: "peer-a", Path: "/b", BytesTransferred: 2500, RequestCount: 40, TotalLatencyNs: ms(40 * 5)},
		},
	}

	if got := peers.TotalBytes(); got != 3500 {
		t.Errorf("总流量应为 3500, 实际 %d", got)
	}

	latency, requests := peers.TotalLatencyNs()
	if requests != 50 {
		t.Errorf("总请求数应为 50, 实际 %d", requests)
	}
	if want := ms(10*30) + ms(40*5); latency != want {
		t.Errorf("延迟累计应为 %d, 实际 %d", want, latency)
	}
}

// TestResolvePeerSnapshotUsesFreshWhenHealthy 拉取成功时直接用新结果
func TestResolvePeerSnapshotUsesFreshWhenHealthy(t *testing.T) {
	fresh := &PeerStats{nodeRows: []sync.PathStat{{NodeID: "peer-a", Path: "/img", RequestCount: 10}}}
	lastGood := &PeerStats{nodeRows: []sync.PathStat{{NodeID: "peer-a", Path: "/img", RequestCount: 999}}}

	got := resolvePeerSnapshot(fresh, lastGood)

	if got != fresh {
		t.Error("健康时应直接返回新快照")
	}
	if got.degraded {
		t.Error("健康快照不该被标记为降级")
	}
}

// TestResolvePeerSnapshotKeepsLastGoodAcrossRepeatedFailures 连续拉取失败时应持续沿用
// 最近一次成功的数据并标记降级, 而不是第二次失败就把数字清零
func TestResolvePeerSnapshotKeepsLastGoodAcrossRepeatedFailures(t *testing.T) {
	lastGood := &PeerStats{
		nodeRows:    []sync.PathStat{{NodeID: "peer-a", Path: "/img", RequestCount: 300}},
		statusCodes: map[string]int64{"200": 300},
	}

	// 模拟 D1 连续三轮拉取失败: 每轮 fresh 都是空的降级快照, lastGood 始终不变
	for round := 1; round <= 3; round++ {
		fresh := &PeerStats{degraded: true, statusCodes: map[string]int64{}, latency: map[string]int64{}}

		got := resolvePeerSnapshot(fresh, lastGood)

		if !got.degraded {
			t.Fatalf("第 %d 轮失败后应标记为降级", round)
		}
		if len(got.nodeRows) != 1 || got.nodeRows[0].RequestCount != 300 {
			t.Fatalf("第 %d 轮失败后丢失了最近一次成功的数据: %+v", round, got.nodeRows)
		}
		if got.MergeStatusCodes(map[string]int64{})["200"] != 300 {
			t.Fatalf("第 %d 轮失败后状态码数据丢失", round)
		}
		// 兜底不能污染 lastGood 本身, 否则下一轮就退化成"已降级"而被丢弃
		if lastGood.degraded {
			t.Fatalf("第 %d 轮兜底就地改写了 lastGood", round)
		}
	}
}

// TestResolvePeerSnapshotWithoutLastGood 从未成功过时只能返回降级空快照
func TestResolvePeerSnapshotWithoutLastGood(t *testing.T) {
	fresh := &PeerStats{degraded: true}

	got := resolvePeerSnapshot(fresh, nil)

	if got != fresh {
		t.Error("没有可用兜底时应返回新快照本身")
	}
	if !got.degraded {
		t.Error("应保持降级标记")
	}
}

// TestNodeSummariesSplitsPerNode 各节点概览应分别统计, 本节点用内存实时值
func TestNodeSummariesSplitsPerNode(t *testing.T) {
	self := sync.NodeID()
	peers := &PeerStats{
		nodeRows: []sync.PathStat{
			// 本节点在 D1 里的旧值必须被内存实时值取代, 不能重复计入
			{NodeID: self, Path: "/img", RequestCount: 999999, BytesTransferred: 999999},
			{NodeID: "peer-a", Path: "/img", RequestCount: 300, ErrorCount: 30,
				BytesTransferred: 3000, CacheHits: 150, CacheMisses: 50,
				TotalLatencyNs: ms(300 * 40), UpdatedAt: 1700000000000},
			{NodeID: "peer-a", Path: "/js", RequestCount: 100, ErrorCount: 0,
				BytesTransferred: 1000, CacheHits: 50, CacheMisses: 50,
				TotalLatencyNs: ms(100 * 10), UpdatedAt: 1700000000000},
		},
	}

	local := []models.PathMetricsJSON{{
		Path: "/img", RequestCount: 200, ErrorCount: 4, BytesTransferred: 2000,
		CacheHits: 180, CacheMisses: 20, TotalLatency: ms(200 * 15),
	}}

	got := peers.NodeSummaries(local)
	if len(got) != 2 {
		t.Fatalf("期望 2 个节点, 实际 %d: %+v", len(got), got)
	}

	byID := map[string]NodeSummary{}
	for _, n := range got {
		byID[n.NodeID] = n
	}

	selfSummary := byID[self]
	if !selfSummary.IsCurrent {
		t.Error("本节点应标记 IsCurrent")
	}
	// 用内存的 200, 而不是 D1 里那份 999999
	if selfSummary.RequestCount != 200 {
		t.Errorf("本节点应取内存实时值 200, 实际 %d", selfSummary.RequestCount)
	}
	if selfSummary.ReportedAt != 0 {
		t.Errorf("本节点是实时值, 不应有上报时刻, 实际 %d", selfSummary.ReportedAt)
	}
	if selfSummary.AvgLatency != "15.00ms" {
		t.Errorf("本节点平均延迟应为 15.00ms, 实际 %s", selfSummary.AvgLatency)
	}
	if selfSummary.CacheHitRate != 0.9 {
		t.Errorf("本节点命中率应为 0.9, 实际 %v", selfSummary.CacheHitRate)
	}

	peer := byID["peer-a"]
	if peer.IsCurrent {
		t.Error("对端节点不该标记 IsCurrent")
	}
	// 跨该节点的两条路径合计
	if peer.RequestCount != 400 {
		t.Errorf("对端节点请求数应为 400, 实际 %d", peer.RequestCount)
	}
	if peer.ErrorRate != 0.075 {
		t.Errorf("对端错误率应为 30/400=0.075, 实际 %v", peer.ErrorRate)
	}
	// 加权平均: (300*40 + 100*10) / 400 = 32.5ms
	if peer.AvgLatency != "32.50ms" {
		t.Errorf("对端加权平均延迟应为 32.50ms, 实际 %s", peer.AvgLatency)
	}
	if peer.ReportedAt != 1700000000 {
		t.Errorf("对端上报时刻应换算成秒 1700000000, 实际 %d", peer.ReportedAt)
	}
}

// TestNodeSummariesAlwaysIncludesSelf 本节点即使毫无流量也要出现在列表里
func TestNodeSummariesAlwaysIncludesSelf(t *testing.T) {
	got := (&PeerStats{}).NodeSummaries(nil)

	if len(got) != 1 {
		t.Fatalf("期望只有本节点 1 条, 实际 %d", len(got))
	}
	if !got[0].IsCurrent || got[0].NodeID != sync.NodeID() {
		t.Errorf("唯一一条应为本节点, 实际 %+v", got[0])
	}
	if got[0].AvgLatency != "0.00ms" {
		t.Errorf("零流量平均延迟应为 0.00ms, 实际 %s", got[0].AvgLatency)
	}
}

// TestAttachNodeBreakdown 每条路径应挂上各节点分布, 且按前缀归并对齐合计行
func TestAttachNodeBreakdown(t *testing.T) {
	self := sync.NodeID()
	peers := &PeerStats{
		nodeRows: []sync.PathStat{
			// 子路径要能归并到 /img 前缀下
			{NodeID: "peer-a", Path: "/img/sub", RequestCount: 300, ErrorCount: 3,
				BytesTransferred: 3000, CacheHits: 150, CacheMisses: 150, TotalLatencyNs: ms(300 * 40)},
			{NodeID: self, Path: "/img", RequestCount: 999999}, // 本节点走内存, 这行应被忽略
		},
	}
	local := []models.PathMetricsJSON{{
		Path: "/img", RequestCount: 100, BytesTransferred: 1000, TotalLatency: ms(100 * 20),
	}}

	aggregated := []models.PathMetricsJSON{{Path: "/img", RequestCount: 400}}
	resolve := func(p string) string {
		if len(p) >= 4 && p[:4] == "/img" {
			return "/img"
		}
		return p
	}

	peers.AttachNodeBreakdown(aggregated, local, resolve)

	nodes := aggregated[0].Nodes
	if len(nodes) != 2 {
		t.Fatalf("期望 2 个节点的分布, 实际 %d: %+v", len(nodes), nodes)
	}
	// 按请求数降序
	if nodes[0].NodeID != "peer-a" || nodes[0].RequestCount != 300 {
		t.Errorf("首位应为请求数最多的 peer-a(300), 实际 %+v", nodes[0])
	}
	if nodes[1].NodeID != self || nodes[1].RequestCount != 100 {
		t.Errorf("次位应为本节点内存值 100 (而非 D1 的 999999), 实际 %+v", nodes[1])
	}
	if !nodes[1].IsCurrent {
		t.Error("本节点应标记 IsCurrent")
	}
	if nodes[0].AvgLatency != "40.00ms" {
		t.Errorf("peer-a 平均延迟应为 40.00ms, 实际 %s", nodes[0].AvgLatency)
	}
	if nodes[0].CacheHitRate != 0.5 {
		t.Errorf("peer-a 命中率应为 0.5, 实际 %v", nodes[0].CacheHitRate)
	}
}

// TestAttachNodeBreakdownEmptyIsNotNull 无分布时返回空数组而非 null (后端契约)
func TestAttachNodeBreakdownEmptyIsNotNull(t *testing.T) {
	aggregated := []models.PathMetricsJSON{{Path: "/nobody", RequestCount: 0}}

	(&PeerStats{}).AttachNodeBreakdown(aggregated, nil, nil)

	if aggregated[0].Nodes == nil {
		t.Error("无节点分布时应为空数组, 不能是 null")
	}
}

// TestInfoFallsBackToSelfNode 拿不到节点列表时至少要报出自己
func TestInfoFallsBackToSelfNode(t *testing.T) {
	info := (&PeerStats{}).Info()

	if len(info.Nodes) == 0 {
		t.Fatal("节点列表不能为空, 至少包含本节点")
	}
	if info.NodeID == "" {
		t.Error("NodeID 不能为空")
	}
	if info.Nodes[0] != info.NodeID {
		t.Errorf("兜底节点列表应为本节点, 实际 %v", info.Nodes)
	}
}
