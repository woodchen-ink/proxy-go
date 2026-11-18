package metrics

import (
	"fmt"
	"testing"
	"time"
)

func TestRefererDailyDetails(t *testing.T) {
	r := NewRefererDailySeries()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	for i := 0; i < 8; i++ {
		r.Record("a.com", "/img/1.png", 100, false, RefererPassed, now)
	}
	r.Record("a.com", "/img/2.png", 100, true, RefererPassed, now)
	r.Record("a.com", "/img/2.png", 0, true, RefererBlocked, now)
	r.Record("a.com", "/img/3.png", 0, false, RefererRedirected, now)
	r.Record("b.com", "/x", 1, false, RefererPassed, now) // 低于上报阈值

	pts := r.SnapshotForUpload(now, 2)
	if len(pts) != 1 {
		t.Fatalf("want 1 point, got %d", len(pts))
	}
	p := pts[0]
	if p.Requests != 11 || p.Errors != 1 || p.Blocked != 1 || p.Redirected != 1 {
		t.Fatalf("counts wrong: %+v", p)
	}
	if p.DistinctPaths != 3 || len(p.TopPaths) != 3 || p.TopPaths[0] != (PathCount{"/img/1.png", 8}) {
		t.Fatalf("paths wrong: %+v", p)
	}
}

func TestRefererDailyPathCapAndRollover(t *testing.T) {
	r := NewRefererDailySeries()
	day1 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	for i := 0; i < refererPathCap+50; i++ {
		r.Record("m.com", fmt.Sprintf("/p/%d", i), 1, false, RefererPassed, day1)
	}
	pts := r.SnapshotForUpload(day1, 2)
	if pts[0].DistinctPaths != refererPathCap || len(pts[0].TopPaths) != refererTopPaths {
		t.Fatalf("cap not applied: distinct=%d top=%d", pts[0].DistinctPaths, len(pts[0].TopPaths))
	}

	// 两天后同一路径槽被新的一天复用, 旧路径清空
	day3 := day1.Add(48 * time.Hour)
	for i := 0; i < refererMinRequestsPerDay; i++ {
		r.Record("m.com", "/new", 1, false, RefererPassed, day3)
	}
	for _, p := range r.SnapshotForUpload(day3, 3) {
		if p.Date == localDay(day3) && (p.DistinctPaths != 1 || p.TopPaths[0].Path != "/new") {
			t.Fatalf("rollover wrong: %+v", p)
		}
	}
}
