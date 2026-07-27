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
		pathStats: []sync.PathStat{{
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
		pathStats: []sync.PathStat{{
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
		pathStats: []sync.PathStat{{Path: "/img", RequestCount: 300}},
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
		pathStats: []sync.PathStat{
			{Path: "/a", BytesTransferred: 1000, RequestCount: 10, TotalLatencyNs: ms(10 * 30)},
			{Path: "/b", BytesTransferred: 2500, RequestCount: 40, TotalLatencyNs: ms(40 * 5)},
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
	fresh := &PeerStats{pathStats: []sync.PathStat{{Path: "/img", RequestCount: 10}}}
	lastGood := &PeerStats{pathStats: []sync.PathStat{{Path: "/img", RequestCount: 999}}}

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
		pathStats:   []sync.PathStat{{Path: "/img", RequestCount: 300}},
		statusCodes: map[string]int64{"200": 300},
		nodes:       []string{"de", "jp"},
	}

	// 模拟 D1 连续三轮拉取失败: 每轮 fresh 都是空的降级快照, lastGood 始终不变
	for round := 1; round <= 3; round++ {
		fresh := &PeerStats{degraded: true, statusCodes: map[string]int64{}, latency: map[string]int64{}}

		got := resolvePeerSnapshot(fresh, lastGood)

		if !got.degraded {
			t.Fatalf("第 %d 轮失败后应标记为降级", round)
		}
		if len(got.pathStats) != 1 || got.pathStats[0].RequestCount != 300 {
			t.Fatalf("第 %d 轮失败后丢失了最近一次成功的数据: %+v", round, got.pathStats)
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
