package metrics

import (
	"testing"
	"time"
)

func TestNodeSeriesAccounting(t *testing.T) {
	ns := NewNodeSeries()
	now := time.Unix(1_800_000_000, 0)

	// 命中的小文件: 不计回源, 不进速度统计
	ns.record(now, nodeSample{bytes: 1000, latencyNs: int64(time.Millisecond), cacheHit: true})
	// 未命中的 10 MB, 耗时 1s → 10 MB/s, 落 5-20 档
	ns.record(now, nodeSample{bytes: 10 << 20, latencyNs: int64(time.Second)})
	// 未命中的 404
	ns.record(now, nodeSample{bytes: 0, latencyNs: int64(time.Millisecond), isError: true})

	mins := ns.Minutes(now)
	if len(mins) != 1 {
		t.Fatalf("minute points = %d, want 1", len(mins))
	}
	p := mins[0]
	if p.Requests != 3 || p.CacheHits != 1 || p.CacheMisses != 2 || p.Errors != 1 {
		t.Fatalf("counts = %+v", p)
	}
	if p.OriginBytes != 10<<20 || p.Bytes != 10<<20+1000 {
		t.Fatalf("bytes = %d origin = %d", p.Bytes, p.OriginBytes)
	}
	if p.BigCount != 1 || p.BigBytes != 10<<20 || p.Speed[2] != 1 {
		t.Fatalf("speed = %+v", p)
	}

	// 跨到 61 分钟后, 旧分钟槽不再出现在窗口里, 小时窗口仍保留
	later := now.Add(61 * time.Minute)
	ns.record(later, nodeSample{bytes: 1})
	if got := ns.Minutes(later); len(got) != 1 || got[0].Requests != 1 {
		t.Fatalf("minutes after roll = %+v", got)
	}
	if got := ns.Hours(later, 6); len(got) != 2 {
		t.Fatalf("hours = %d, want 2", len(got))
	}
}
