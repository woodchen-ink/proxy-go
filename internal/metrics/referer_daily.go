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
//   - 被引用路径只为最近 2 天 (与上报窗口 refererUploadDays 一致) 计数, 每天最多跟踪 refererPathCap 个,
//     超出后新路径不再计数 (distinct 封顶为 cap), 内存与 D1 列宽都有上界

const (
	refererDaySlotCount      = 40
	refererMinRequestsPerDay = 10
	refererPathSlots         = 2
	refererPathCap           = 200
	refererTopPaths          = 10
	refererPathMaxLen        = 200
)

// RefererAction 请求被 Referer 规则处理的方式
type RefererAction uint8

const (
	RefererPassed     RefererAction = iota // 正常放行
	RefererBlocked                         // 命中黑名单 / 不在白名单, 403
	RefererRedirected                      // 命中路径级 302 分流
)

// localDay 把 time.Time 切成本地时区下的"自纪元天数"
// 该口径必须与查询端 (cloudflare-worker admin/series.ts) 一致, 否则上报存的 ts_date
// 与查询的 minDate/maxDate 区间错位, 出现"今天数据不显示"或"30 天范围漏一天"
func localDay(t time.Time) int64 {
	_, offset := t.Zone()
	return (t.Unix() + int64(offset)) / 86400
}

// refererDayBucket 单天桶
type refererDayBucket struct {
	date       int64 // 自纪元天数; 0 表示未占用
	requests   atomic.Int64
	bytes      atomic.Int64
	errors     atomic.Int64
	blocked    atomic.Int64
	redirected atomic.Int64
}

// refererPathDay 单天的被引用路径计数, 由 refererHostSeries.mu 保护
type refererPathDay struct {
	date  int64
	paths map[string]int64
}

// refererHostSeries 单 host 的天序列
type refererHostSeries struct {
	mu    sync.Mutex
	days  [refererDaySlotCount]refererDayBucket
	paths [refererPathSlots]refererPathDay
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

// Record 累加一次请求到 host 当天桶; host 已由上游归一化 (小写 / 去端口), path 为请求路径 (不含 query)
// 被拦截的 403 只计 blocked 不计 errors, 免得"拉黑后错误数暴涨"误导成源站故障
func (r *RefererDailySeries) Record(host, path string, bytes int64, isError bool, action RefererAction, now time.Time) {
	if host == "" {
		return
	}
	day := localDay(now)
	idx := int(day % refererDaySlotCount)

	s := r.getOrCreate(host)
	b := &s.days[idx]
	s.mu.Lock()
	if atomic.LoadInt64(&b.date) != day {
		// 先清零再换日期: 快照不持锁读, 先换日期会把 40 天前的计数当成今天上报 (D1 侧 MAX 会永久保留)
		b.requests.Store(0)
		b.bytes.Store(0)
		b.errors.Store(0)
		b.blocked.Store(0)
		b.redirected.Store(0)
		atomic.StoreInt64(&b.date, day)
	}
	if len(path) > refererPathMaxLen {
		path = path[:refererPathMaxLen]
	}
	if path != "" {
		pd := &s.paths[int(day%refererPathSlots)]
		if pd.date != day || pd.paths == nil {
			pd.date = day
			pd.paths = make(map[string]int64)
		}
		if _, ok := pd.paths[path]; ok || len(pd.paths) < refererPathCap {
			pd.paths[path]++
		}
	}
	s.mu.Unlock()

	b.requests.Add(1)
	b.bytes.Add(bytes)
	switch {
	case action == RefererBlocked:
		b.blocked.Add(1)
	case action == RefererRedirected:
		b.redirected.Add(1)
	case isError:
		b.errors.Add(1)
	}
}

// PathCount 被引用路径及次数
type PathCount struct {
	Path     string `json:"p"`
	Requests int64  `json:"n"`
}

// DailyPoint 单条上报点
type DailyPoint struct {
	Host          string
	Date          int64
	Requests      int64
	Bytes         int64
	Errors        int64
	Blocked       int64
	Redirected    int64
	DistinctPaths int64       // 当天不同路径数, 封顶 refererPathCap; 超过窗口的老天为 0
	TopPaths      []PathCount // 当天被引用最多的路径, 最多 refererTopPaths 个
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
			p := DailyPoint{
				Host:       host,
				Date:       d,
				Requests:   req,
				Bytes:      b.bytes.Load(),
				Errors:     b.errors.Load(),
				Blocked:    b.blocked.Load(),
				Redirected: b.redirected.Load(),
			}
			p.DistinctPaths, p.TopPaths = s.topPaths(d)
			out = append(out, p)
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

// topPaths 取某天的不同路径数与前 refererTopPaths 个路径
func (s *refererHostSeries) topPaths(day int64) (int64, []PathCount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd := &s.paths[int(day%refererPathSlots)]
	if pd.date != day || len(pd.paths) == 0 {
		return 0, nil
	}
	list := make([]PathCount, 0, len(pd.paths))
	for p, n := range pd.paths {
		list = append(list, PathCount{Path: p, Requests: n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Requests != list[j].Requests {
			return list[i].Requests > list[j].Requests
		}
		return list[i].Path < list[j].Path
	})
	if len(list) > refererTopPaths {
		list = list[:refererTopPaths]
	}
	return int64(len(pd.paths)), list
}
