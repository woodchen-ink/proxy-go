package metrics

import (
	"os"
	"path/filepath"
	"proxy-go/internal/models"
	"testing"
)

// newTestCollector 构造只带路径统计的最小 Collector, 并塞入指定路径的非零计数
func newTestCollector(paths ...string) *Collector {
	c := &Collector{pathStats: NewRefererStats()}
	for _, p := range paths {
		m := &models.PathMetrics{Path: p}
		m.RequestCount.Store(10)
		m.BytesTransferred.Store(1024)
		m.CacheHits.Store(3)
		c.pathStats.Store(p, m)
	}
	return c
}

// requestCount 读取指定路径当前内存计数, 不存在返回 -1
func requestCount(c *Collector, path string) int64 {
	m, ok := c.pathStats.Load(path)
	if !ok {
		return -1
	}
	return m.RequestCount.Load()
}

// 前缀清零必须是"完整路径段"语义: /p1 覆盖 /p1 与 /p1/sub, 不误伤 /p1x
func TestZeroPathStatsPrefix(t *testing.T) {
	c := newTestCollector("/p1", "/p1/sub", "/p1x", "/p2")

	if n := c.zeroPathStats("/p1"); n != 2 {
		t.Fatalf("期望清零 2 条, 得到 %d", n)
	}
	if got := requestCount(c, "/p1"); got != 0 {
		t.Errorf("/p1 应被清零, 得到 %d", got)
	}
	if got := requestCount(c, "/p1/sub"); got != 0 {
		t.Errorf("/p1/sub 应被清零, 得到 %d", got)
	}
	if got := requestCount(c, "/p1x"); got != 10 {
		t.Errorf("/p1x 不应被误伤, 得到 %d", got)
	}
	if got := requestCount(c, "/p2"); got != 10 {
		t.Errorf("/p2 不应被清零, 得到 %d", got)
	}
}

func TestZeroPathStatsAll(t *testing.T) {
	c := newTestCollector("/p1", "/p2")

	if n := c.zeroPathStats(""); n != 2 {
		t.Fatalf("期望清零全部 2 条, 得到 %d", n)
	}
	if got := requestCount(c, "/p1"); got != 0 {
		t.Errorf("/p1 应被清零, 得到 %d", got)
	}
	if got := requestCount(c, "/p2"); got != 0 {
		t.Errorf("/p2 应被清零, 得到 %d", got)
	}
}

// 位点只进不退, 且每次推进都落盘 —— 文件是跨重启的权威副本,
// 启动补应用堵"上报旧值后崩溃"的缝全靠它
func TestMarkResetAppliedMonotonicAndPersisted(t *testing.T) {
	origFile := resetSeenFile
	resetSeenFile = filepath.Join(t.TempDir(), "reset_seen")
	origSeen := resetSeenMs.Load()
	t.Cleanup(func() {
		resetSeenFile = origFile
		resetSeenMs.Store(origSeen)
	})
	resetSeenMs.Store(0)

	markResetApplied(1000)
	if got := ResetSeen(); got != 1000 {
		t.Fatalf("期望位点 1000, 得到 %d", got)
	}

	markResetApplied(500) // 旧代际不应把位点拉回去
	if got := ResetSeen(); got != 1000 {
		t.Errorf("位点不应回退, 得到 %d", got)
	}

	data, err := os.ReadFile(resetSeenFile)
	if err != nil {
		t.Fatalf("位点文件应已写入: %v", err)
	}
	if string(data) != "1000" {
		t.Errorf("文件内容期望 1000, 得到 %q", string(data))
	}
}
