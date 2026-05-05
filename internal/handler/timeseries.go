package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"proxy-go/pkg/sync"
)

// TimeseriesHandler 路径时间序列查询处理器
//
// 数据源为 D1 path_timeseries 表 (跨节点聚合后的小时桶), 由仪表盘趋势图使用
type TimeseriesHandler struct{}

// NewTimeseriesHandler 创建时间序列处理器
func NewTimeseriesHandler() *TimeseriesHandler {
	return &TimeseriesHandler{}
}

// hourPoint 单小时数据点 (序列化用)
type hourPoint struct {
	Hour     int   `json:"hour"`
	Requests int64 `json:"requests"`
	Bytes    int64 `json:"bytes"`
	Errors   int64 `json:"errors"`
}

// dayPoint 单日数据点 (序列化用)
type dayPoint struct {
	Date     string `json:"date"`
	Requests int64  `json:"requests"`
	Bytes    int64  `json:"bytes"`
	Errors   int64  `json:"errors"`
}

// pathSeriesView 单个路径的多粒度序列
type pathSeriesView struct {
	Path           string      `json:"path"`
	Today          []hourPoint `json:"today"`
	Yesterday      []hourPoint `json:"yesterday"`
	Daily          []dayPoint  `json:"daily"`
	TodayReq       int64       `json:"today_requests"`
	TodayBytes     int64       `json:"today_bytes"`
	YesterdayReq   int64       `json:"yesterday_requests"`
	YesterdayBytes int64       `json:"yesterday_bytes"`
	MonthReq       int64       `json:"month_requests"`
	MonthBytes     int64       `json:"month_bytes"`
}

// timeseriesResponse 接口响应体
type timeseriesResponse struct {
	GeneratedAt int64            `json:"generated_at"`
	Timezone    string           `json:"timezone"`
	Days        int              `json:"days"`
	Paths       []pathSeriesView `json:"paths"`
}

// GetTimeseries 返回近 31 天 (含今天) 各路径的小时 / 日级聚合时间序列
//
// 查询参数:
//
//	days: 日序列长度 (默认 31, 范围 1..31)
func (h *TimeseriesHandler) GetTimeseries(w http.ResponseWriter, r *http.Request) {
	days := 31
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 31 {
			days = n
		}
	}

	now := time.Now()
	loc := now.Location()

	// 计算查询的小时区间: 覆盖"近 days 天" + 当前小时
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).
		Add(-time.Duration(days-1) * 24 * time.Hour)
	minHour := startDay.Unix() / 3600
	maxHour := now.Unix() / 3600

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	points, err := sync.LoadPathTimeseries(ctx, minHour, maxHour)
	if err != nil {
		writeJSON(w, http.StatusOK, timeseriesResponse{
			GeneratedAt: now.Unix(),
			Timezone:    loc.String(),
			Days:        days,
			Paths:       []pathSeriesView{},
		})
		return
	}

	resp := buildTimeseriesView(points, now, days)
	writeJSON(w, http.StatusOK, resp)
}

// buildTimeseriesView 把 D1 返回的扁平桶组装成"今日 / 昨日 / 近 N 天"三段视图
func buildTimeseriesView(points []sync.AggregatedHourPoint, now time.Time, days int) timeseriesResponse {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterdayStart := todayStart.Add(-24 * time.Hour)
	currentHour := now.Hour()

	yestStartHour := yesterdayStart.Unix() / 3600
	todayStartHour := todayStart.Unix() / 3600

	type pathAgg struct {
		view *pathSeriesView
	}
	byPath := make(map[string]*pathAgg)

	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).
		Add(-time.Duration(days-1) * 24 * time.Hour)

	for _, p := range points {
		agg, ok := byPath[p.Path]
		if !ok {
			v := &pathSeriesView{
				Path:      p.Path,
				Today:     make([]hourPoint, currentHour+1),
				Yesterday: make([]hourPoint, 24),
				Daily:     make([]dayPoint, days),
			}
			for h := 0; h <= currentHour; h++ {
				v.Today[h] = hourPoint{Hour: h}
			}
			for h := 0; h < 24; h++ {
				v.Yesterday[h] = hourPoint{Hour: h}
			}
			for i := 0; i < days; i++ {
				d := startDay.Add(time.Duration(i) * 24 * time.Hour)
				v.Daily[i] = dayPoint{Date: d.Format("2006-01-02")}
			}
			agg = &pathAgg{view: v}
			byPath[p.Path] = agg
		}

		// 小时序列填充
		switch {
		case p.TsHour >= todayStartHour && p.TsHour <= todayStartHour+int64(currentHour):
			idx := int(p.TsHour - todayStartHour)
			agg.view.Today[idx].Requests += p.Requests
			agg.view.Today[idx].Bytes += p.Bytes
			agg.view.Today[idx].Errors += p.Errors
			agg.view.TodayReq += p.Requests
			agg.view.TodayBytes += p.Bytes
		case p.TsHour >= yestStartHour && p.TsHour < todayStartHour:
			idx := int(p.TsHour - yestStartHour)
			agg.view.Yesterday[idx].Requests += p.Requests
			agg.view.Yesterday[idx].Bytes += p.Bytes
			agg.view.Yesterday[idx].Errors += p.Errors
			agg.view.YesterdayReq += p.Requests
			agg.view.YesterdayBytes += p.Bytes
		}

		// 日序列填充: 把小时桶按本地日期归档
		bucketTime := time.Unix(p.TsHour*3600, 0).In(loc)
		bucketDay := time.Date(bucketTime.Year(), bucketTime.Month(), bucketTime.Day(), 0, 0, 0, 0, loc)
		dayDiff := int(bucketDay.Sub(startDay).Hours() / 24)
		if dayDiff >= 0 && dayDiff < days {
			agg.view.Daily[dayDiff].Requests += p.Requests
			agg.view.Daily[dayDiff].Bytes += p.Bytes
			agg.view.Daily[dayDiff].Errors += p.Errors
			agg.view.MonthReq += p.Requests
			agg.view.MonthBytes += p.Bytes
		}
	}

	out := make([]pathSeriesView, 0, len(byPath))
	for _, a := range byPath {
		out = append(out, *a.view)
	}
	// 按近 N 天总请求数降序; 同分按 path 字典序
	sort.Slice(out, func(i, j int) bool {
		if out[i].MonthReq != out[j].MonthReq {
			return out[i].MonthReq > out[j].MonthReq
		}
		return out[i].Path < out[j].Path
	})

	return timeseriesResponse{
		GeneratedAt: now.Unix(),
		Timezone:    loc.String(),
		Days:        days,
		Paths:       out,
	}
}

// writeJSON 写出 JSON 响应, 失败时静默 (与项目其他 handler 风格一致)
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
