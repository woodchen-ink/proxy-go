package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// 节点维度的流量序列: 本节点整体 (不分路径) 的请求 / 下发 / 回源 / 命中 / 下载速度
//
// 两个环形缓冲共用一种桶:
//   - 分钟级 60 个槽: 随 15s 实时快照推给中控, 仪表盘"最近 1 小时"曲线不经 D1
//   - 小时级 48 个槽: 随 5 分钟心跳上报 D1 node_timeseries, 支撑 24 小时 / 30 天视图
//
// 下载速度只统计 ≥ bigTransferBytes 的响应: 小文件的耗时主要是握手与首包, 算出来是延迟不是带宽。
// 节点记录的耗时含响应体写完, 所以 字节 / 耗时 就是节点到外层 CDN 边缘这一段的实际下发速度。
// 只存原始量 (字节和、耗时和、次数), 均速由读侧按合并后的 Σbytes / Σns 重算, 不对派生值求和。

const (
	bigTransferBytes = 256 << 10
	minuteSlotCount  = 60
)

// speedBucketBounds 下载速度分档上界 (字节/秒): <1 / 1-5 / 5-20 / 20-50 / ≥50 MB/s
var speedBucketBounds = [...]float64{1 << 20, 5 << 20, 20 << 20, 50 << 20}

const speedBucketCount = len(speedBucketBounds) + 1

// nodeSample 单个请求对节点序列的贡献
type nodeSample struct {
	bytes     int64
	latencyNs int64
	isError   bool
	cacheHit  bool
}

type nodeBucket struct {
	slot        int64 // 自纪元的分钟 / 小时序号; 0 表示未占用
	requests    atomic.Int64
	bytes       atomic.Int64
	originBytes atomic.Int64
	errors      atomic.Int64
	hits        atomic.Int64
	misses      atomic.Int64
	latencyNs   atomic.Int64
	bigBytes    atomic.Int64
	bigNs       atomic.Int64
	bigCount    atomic.Int64
	speed       [speedBucketCount]atomic.Int64
}

func (b *nodeBucket) reset(slot int64) {
	atomic.StoreInt64(&b.slot, slot)
	for _, v := range []*atomic.Int64{&b.requests, &b.bytes, &b.originBytes, &b.errors, &b.hits, &b.misses, &b.latencyNs, &b.bigBytes, &b.bigNs, &b.bigCount} {
		v.Store(0)
	}
	for i := range b.speed {
		b.speed[i].Store(0)
	}
}

func (b *nodeBucket) add(s nodeSample) {
	b.requests.Add(1)
	b.bytes.Add(s.bytes)
	b.latencyNs.Add(s.latencyNs)
	if s.isError {
		b.errors.Add(1)
	}
	if s.cacheHit {
		b.hits.Add(1)
	} else {
		b.misses.Add(1)
		b.originBytes.Add(s.bytes) // 未命中的响应体都是从源站读来的
	}
	if s.bytes >= bigTransferBytes && s.latencyNs > 0 {
		b.bigBytes.Add(s.bytes)
		b.bigNs.Add(s.latencyNs)
		b.bigCount.Add(1)
		bps := float64(s.bytes) / (float64(s.latencyNs) / 1e9)
		idx := len(speedBucketBounds)
		for i, bound := range speedBucketBounds {
			if bps < bound {
				idx = i
				break
			}
		}
		b.speed[idx].Add(1)
	}
}

// NodeSeriesPoint 一个桶的原始量 (实时快照与 D1 上报共用)
type NodeSeriesPoint struct {
	Ts          int64   `json:"ts"` // 桶起点 unix 秒
	Requests    int64   `json:"requests"`
	Bytes       int64   `json:"bytes"`
	OriginBytes int64   `json:"origin_bytes"`
	Errors      int64   `json:"errors"`
	CacheHits   int64   `json:"cache_hits"`
	CacheMisses int64   `json:"cache_misses"`
	LatencyNs   int64   `json:"latency_ns"`
	BigBytes    int64   `json:"big_bytes"`
	BigNs       int64   `json:"big_ns"`
	BigCount    int64   `json:"big_count"`
	Speed       []int64 `json:"speed"` // 各速度档请求数, 档位见 speedBucketBounds
}

// nodeRing 固定槽数的环形缓冲, 槽位按"自纪元的 unit 序号"取模, 落到新序号时清零重用
type nodeRing struct {
	mu    sync.Mutex // 只在换槽时加锁
	unit  int64      // 每槽秒数
	slots []nodeBucket
}

func newNodeRing(unit time.Duration, n int) *nodeRing {
	return &nodeRing{unit: int64(unit / time.Second), slots: make([]nodeBucket, n)}
}

func (r *nodeRing) record(now time.Time, s nodeSample) {
	slot := now.Unix() / r.unit
	b := &r.slots[int(slot%int64(len(r.slots)))]
	if atomic.LoadInt64(&b.slot) != slot {
		r.mu.Lock()
		if atomic.LoadInt64(&b.slot) != slot {
			b.reset(slot)
		}
		r.mu.Unlock()
	}
	b.add(s)
}

// points 返回近 n 个槽 (含当前) 中有数据的桶, 按时间升序
func (r *nodeRing) points(now time.Time, n int) []NodeSeriesPoint {
	if n <= 0 || n > len(r.slots) {
		n = len(r.slots)
	}
	cur := now.Unix() / r.unit
	out := make([]NodeSeriesPoint, 0, n)
	for slot := cur - int64(n-1); slot <= cur; slot++ {
		b := &r.slots[int(slot%int64(len(r.slots)))]
		if atomic.LoadInt64(&b.slot) != slot || b.requests.Load() == 0 {
			continue
		}
		speed := make([]int64, speedBucketCount)
		for i := range b.speed {
			speed[i] = b.speed[i].Load()
		}
		out = append(out, NodeSeriesPoint{
			Ts:          slot * r.unit,
			Requests:    b.requests.Load(),
			Bytes:       b.bytes.Load(),
			OriginBytes: b.originBytes.Load(),
			Errors:      b.errors.Load(),
			CacheHits:   b.hits.Load(),
			CacheMisses: b.misses.Load(),
			LatencyNs:   b.latencyNs.Load(),
			BigBytes:    b.bigBytes.Load(),
			BigNs:       b.bigNs.Load(),
			BigCount:    b.bigCount.Load(),
			Speed:       speed,
		})
	}
	return out
}

// NodeSeries 本节点的分钟级与小时级序列
type NodeSeries struct {
	minutes *nodeRing
	hours   *nodeRing
}

// NewNodeSeries 构造空序列: 60 个分钟槽 + 48 个小时槽
func NewNodeSeries() *NodeSeries {
	return &NodeSeries{
		minutes: newNodeRing(time.Minute, minuteSlotCount),
		hours:   newNodeRing(time.Hour, hourSlotCount),
	}
}

func (n *NodeSeries) record(now time.Time, s nodeSample) {
	n.minutes.record(now, s)
	n.hours.record(now, s)
}

// Minutes 最近 60 分钟 (含当前分钟) 的分钟桶
func (n *NodeSeries) Minutes(now time.Time) []NodeSeriesPoint {
	return n.minutes.points(now, minuteSlotCount)
}

// Hours 最近 hours 小时 (含当前小时) 的小时桶
func (n *NodeSeries) Hours(now time.Time, hours int) []NodeSeriesPoint {
	return n.hours.points(now, hours)
}
