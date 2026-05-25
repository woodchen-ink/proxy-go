package handler

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"proxy-go/pkg/sync"
)

// RefererDailyHandler 引用来源 host 天级时间序列查询处理器
// 数据源为 D1 referer_daily 表 (跨节点聚合), 仪表盘用于排查异常引用
type RefererDailyHandler struct{}

// localDay 把 time.Time 切成本地时区下的"自纪元天数"
// 该口径必须与上报端 (internal/metrics/referer_daily.go) 一致, 否则查询区间与存储的 ts_date 错位
func localDay(t time.Time) int64 {
	_, offset := t.Zone()
	return (t.Unix() + int64(offset)) / 86400
}

// NewRefererDailyHandler 创建 referer 天级序列处理器
func NewRefererDailyHandler() *RefererDailyHandler {
	return &RefererDailyHandler{}
}

// refererDayPoint 单天数据点
type refererDayPoint struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
	Errors   int64  `json:"errors"`
}

// refererHostView 单 host 的近 N 天序列
type refererHostView struct {
	Host          string            `json:"host"`
	Daily         []refererDayPoint `json:"daily"`
	TotalReq      int64             `json:"total_requests"`
	TotalBytes    int64             `json:"total_bytes"`
	TotalErrors   int64             `json:"total_errors"`
	FirstSeenDate string            `json:"first_seen_date"`
	LastSeenDate  string            `json:"last_seen_date"`
}

type refererDailyResponse struct {
	GeneratedAt int64             `json:"generated_at"`
	Timezone    string            `json:"timezone"`
	Days        int               `json:"days"`
	Hosts       []refererHostView `json:"hosts"`
}

// GetRefererDaily 返回近 days 天 (1..30, 默认 30) 每 host 的天级序列
// 查询参数:
//   days: 序列长度
//   top:  返回的 host 数量上限 (按近 days 天总请求降序, 默认 50, 上限 200)
func (h *RefererDailyHandler) GetRefererDaily(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 30 {
			days = n
		}
	}
	top := 50
	if v := r.URL.Query().Get("top"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			top = n
		}
	}

	now := time.Now()
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	startDay := todayStart.Add(-time.Duration(days-1) * 24 * time.Hour)

	maxDate := localDay(now)
	minDate := maxDate - int64(days-1)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	points, err := sync.LoadRefererDaily(ctx, minDate, maxDate)
	if err != nil {
		writeJSON(w, http.StatusOK, refererDailyResponse{
			GeneratedAt: now.Unix(),
			Timezone:    loc.String(),
			Days:        days,
			Hosts:       []refererHostView{},
		})
		return
	}

	writeJSON(w, http.StatusOK, buildRefererDailyView(points, now, days, top, startDay))
}

// buildRefererDailyView 把扁平 (host, ts_date) 桶组装为每 host 的天序列, 并按总请求 Top N 截断
func buildRefererDailyView(
	points []sync.AggregatedRefererDayPoint,
	now time.Time,
	days int,
	top int,
	startDay time.Time,
) refererDailyResponse {
	loc := now.Location()
	byHost := make(map[string]*refererHostView, 64)

	// startDay 是本地时区 00:00, 其本地天数即 ts_date 序列的起点;
	// p.TsDate 已经是本地时区天数 (由上报端 localDay 写入), 直接相减即可
	startLocalDay := localDay(startDay)

	for _, p := range points {
		v, ok := byHost[p.Host]
		if !ok {
			v = &refererHostView{
				Host:  p.Host,
				Daily: make([]refererDayPoint, days),
			}
			for i := 0; i < days; i++ {
				d := startDay.Add(time.Duration(i) * 24 * time.Hour)
				v.Daily[i] = refererDayPoint{Date: d.Format("2006-01-02")}
			}
			byHost[p.Host] = v
		}

		idx := int(p.TsDate - startLocalDay)
		if idx < 0 || idx >= days {
			continue
		}
		v.Daily[idx].Requests += p.Requests
		v.Daily[idx].Bytes += p.Bytes
		v.Daily[idx].Errors += p.Errors
		v.TotalReq += p.Requests
		v.TotalBytes += p.Bytes
		v.TotalErrors += p.Errors

		dateStr := v.Daily[idx].Date
		if v.FirstSeenDate == "" || dateStr < v.FirstSeenDate {
			v.FirstSeenDate = dateStr
		}
		if dateStr > v.LastSeenDate {
			v.LastSeenDate = dateStr
		}
	}

	out := make([]refererHostView, 0, len(byHost))
	for _, v := range byHost {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalReq != out[j].TotalReq {
			return out[i].TotalReq > out[j].TotalReq
		}
		return out[i].Host < out[j].Host
	})
	if len(out) > top {
		out = out[:top]
	}

	return refererDailyResponse{
		GeneratedAt: now.Unix(),
		Timezone:    loc.String(),
		Days:        days,
		Hosts:       out,
	}
}
