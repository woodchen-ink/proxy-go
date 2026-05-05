package metrics

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// 路径维度的小时级时间序列, 用于供本节点向 D1 周期性上报增量
//
// 设计要点:
//   - 内存环形缓冲, 每路径保留 48 个小时槽 (覆盖今日 + 昨日, 留足上传窗口)
//   - 槽位按"自纪元小时数"映射, 旧槽在落入新一小时时自动清零, 不需要显式滚动
//   - 仅记录本节点流量; 跨节点聚合由 D1 worker GROUP BY 完成
//   - 长期 (近 31 天) 数据由 D1 持久化, 内存只承担实时统计与上报缓冲

const hourSlotCount = 48

// hourBucket 单小时数据桶
type hourBucket struct {
	hour     int64 // 自纪元的小时数; 0 表示桶未占用
	requests atomic.Int64
	bytes    atomic.Int64
	errors   atomic.Int64
}

// pathTimeSeries 单个路径前缀的小时序列
type pathTimeSeries struct {
	mu    sync.Mutex // 仅在换槽时锁, 不参与读写计数
	hours [hourSlotCount]hourBucket
}

// PathTimeSeries 全部路径前缀的时间序列容器
type PathTimeSeries struct {
	mu     sync.RWMutex
	series map[string]*pathTimeSeries
}

// NewPathTimeSeries 构造空的时间序列容器
func NewPathTimeSeries() *PathTimeSeries {
	return &PathTimeSeries{series: make(map[string]*pathTimeSeries)}
}

// getOrCreate 获取或创建指定路径的序列
func (p *PathTimeSeries) getOrCreate(path string) *pathTimeSeries {
	p.mu.RLock()
	s, ok := p.series[path]
	p.mu.RUnlock()
	if ok {
		return s
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok = p.series[path]; ok {
		return s
	}
	s = &pathTimeSeries{}
	p.series[path] = s
	return s
}

// Record 累加一次请求到当前小时桶
func (p *PathTimeSeries) Record(path string, bytes int64, isError bool, now time.Time) {
	if path == "" {
		return
	}
	hour := now.Unix() / 3600
	idx := int(hour % hourSlotCount)

	s := p.getOrCreate(path)
	b := &s.hours[idx]
	if atomic.LoadInt64(&b.hour) != hour {
		s.mu.Lock()
		if atomic.LoadInt64(&b.hour) != hour {
			atomic.StoreInt64(&b.hour, hour)
			b.requests.Store(0)
			b.bytes.Store(0)
			b.errors.Store(0)
		}
		s.mu.Unlock()
	}
	b.requests.Add(1)
	b.bytes.Add(bytes)
	if isError {
		b.errors.Add(1)
	}
}

// HourlyBucket 单条上报点 (供 D1 同步使用)
type HourlyBucket struct {
	Path     string
	Hour     int64 // 自纪元小时数
	Requests int64
	Bytes    int64
	Errors   int64
}

// SnapshotForUpload 返回当前所有路径"近 hours 小时"的桶快照
//
// 用于 D1 周期性上报: 每次取近 N 小时的最新值, D1 端对 (path, hour, node_id) 做 UPSERT,
// 等价于本节点对自身贡献的 idempotent 覆盖
func (p *PathTimeSeries) SnapshotForUpload(now time.Time, hours int) []HourlyBucket {
	if hours <= 0 || hours > hourSlotCount {
		hours = hourSlotCount
	}
	curHour := now.Unix() / 3600
	minHour := curHour - int64(hours-1)

	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]HourlyBucket, 0, len(p.series)*hours)
	for path, s := range p.series {
		for i := 0; i < hourSlotCount; i++ {
			b := &s.hours[i]
			h := atomic.LoadInt64(&b.hour)
			if h < minHour || h > curHour {
				continue
			}
			req := b.requests.Load()
			by := b.bytes.Load()
			er := b.errors.Load()
			if req == 0 && by == 0 && er == 0 {
				continue
			}
			out = append(out, HourlyBucket{
				Path:     path,
				Hour:     h,
				Requests: req,
				Bytes:    by,
				Errors:   er,
			})
		}
	}
	// 按 (path, hour) 升序输出, 便于上报日志阅读
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Hour < out[j].Hour
	})
	return out
}
