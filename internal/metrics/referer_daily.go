package metrics

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Referer host 维度的天级时间序列
//
// 设计要点:
//   - 内存按 host 分桶, 每 host 保留 40 天槽 (覆盖近 30 天 + 上传冗余), 自纪元天数 % 槽数 映射
//   - 仅记录"当天累计 requests >= refererMinRequestsPerDay"的 host 上报到 D1, 控制 cardinality
//   - 槽位旧值在落入新一天时自动清零, 不需要显式滚动
//   - 跨节点聚合在 D1 worker 端通过 GROUP BY 完成
//   - 天数按本地时区切分 (容器 TZ=Asia/Shanghai), 与查询端口径一致, 避免 UTC+8 下"今天上半天看不见数据"

const (
	refererDaySlotCount      = 40
	refererMinRequestsPerDay = 10
)

// localDay 把 time.Time 切成本地时区下的"自纪元天数"
// 该口径必须与查询端 (internal/handler/referer_daily.go) 一致, 否则上报存的 ts_date
// 与查询的 minDate/maxDate 区间错位, 出现"今天数据不显示"或"30 天范围漏一天"
func localDay(t time.Time) int64 {
	_, offset := t.Zone()
	return (t.Unix() + int64(offset)) / 86400
}

// refererDayBucket 单天桶
type refererDayBucket struct {
	date     int64 // 自纪元天数; 0 表示未占用
	requests atomic.Int64
	bytes    atomic.Int64
	errors   atomic.Int64
}

// refererHostSeries 单 host 的天序列
type refererHostSeries struct {
	mu   sync.Mutex
	days [refererDaySlotCount]refererDayBucket
}

// RefererDailySeries 全部 host 的天级序列容器
type RefererDailySeries struct {
	mu     sync.RWMutex
	series map[string]*refererHostSeries
}

// NewRefererDailySeries 构造空容器
func NewRefererDailySeries() *RefererDailySeries {
	return &RefererDailySeries{series: make(map[string]*refererHostSeries)}
}

func (r *RefererDailySeries) getOrCreate(host string) *refererHostSeries {
	r.mu.RLock()
	s, ok := r.series[host]
	r.mu.RUnlock()
	if ok {
		return s
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok = r.series[host]; ok {
		return s
	}
	s = &refererHostSeries{}
	r.series[host] = s
	return s
}

// Record 累加一次请求到 host 当天桶; host 已由上游归一化 (小写 / 去端口)
func (r *RefererDailySeries) Record(host string, bytes int64, isError bool, now time.Time) {
	if host == "" {
		return
	}
	day := localDay(now)
	idx := int(day % refererDaySlotCount)

	s := r.getOrCreate(host)
	b := &s.days[idx]
	if atomic.LoadInt64(&b.date) != day {
		s.mu.Lock()
		if atomic.LoadInt64(&b.date) != day {
			atomic.StoreInt64(&b.date, day)
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

// DailyPoint 单条上报点
type DailyPoint struct {
	Host     string
	Date     int64
	Requests int64
	Bytes    int64
	Errors   int64
}

// SnapshotForUpload 返回近 days 天的桶快照, 仅包含 requests >= refererMinRequestsPerDay 的条目
// 用于 D1 周期性上报: worker 端对 (host, date, node_id) 做 UPSERT MAX
func (r *RefererDailySeries) SnapshotForUpload(now time.Time, days int) []DailyPoint {
	if days <= 0 || days > refererDaySlotCount {
		days = refererDaySlotCount
	}
	curDay := localDay(now)
	minDay := curDay - int64(days-1)

	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]DailyPoint, 0, len(r.series))
	for host, s := range r.series {
		for i := 0; i < refererDaySlotCount; i++ {
			b := &s.days[i]
			d := atomic.LoadInt64(&b.date)
			if d < minDay || d > curDay {
				continue
			}
			req := b.requests.Load()
			if req < refererMinRequestsPerDay {
				continue
			}
			by := b.bytes.Load()
			er := b.errors.Load()
			out = append(out, DailyPoint{
				Host:     host,
				Date:     d,
				Requests: req,
				Bytes:    by,
				Errors:   er,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Host < out[j].Host
	})
	return out
}
