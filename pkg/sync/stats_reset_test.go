package sync

import (
	"encoding/json"
	"strings"
	"testing"
)

// 重置代际语句构造是纯函数, 这里锁死两条关键语义:
//  1. marker 与删行在同一个 batch 里 (D1 batch 是事务的, 不能只成一半)
//  2. 前缀删除必须同时覆盖精确匹配与子路径, 且发起端删的是全节点的行 (不带 node_id 条件)

func TestBuildStatsResetStatementsAll(t *testing.T) {
	stmts := buildStatsResetStatements("", 1234)
	if len(stmts) != 2 {
		t.Fatalf("期望 2 条语句 (marker + 删行), 得到 %d", len(stmts))
	}
	if !strings.Contains(stmts[0].SQL, "INSERT INTO stats_resets") {
		t.Errorf("第一条应写入 marker: %s", stmts[0].SQL)
	}
	if got := stmts[0].Params[1]; got != int64(1234) {
		t.Errorf("marker reset_at 期望 1234, 得到 %v", got)
	}
	if stmts[1].SQL != "DELETE FROM path_stats" {
		t.Errorf("全量重置应删除全部行 (不带 WHERE): %s", stmts[1].SQL)
	}
	if strings.Contains(stmts[1].SQL, "node_id") {
		t.Errorf("发起端删的是全节点的行, 不应带 node_id 条件: %s", stmts[1].SQL)
	}
}

func TestBuildStatsResetStatementsPrefix(t *testing.T) {
	stmts := buildStatsResetStatements("/p1", 99)
	if len(stmts) != 2 {
		t.Fatalf("期望 2 条语句, 得到 %d", len(stmts))
	}
	if got := stmts[0].Params[0]; got != "/p1" {
		t.Errorf("marker path 期望 /p1, 得到 %v", got)
	}
	if !strings.Contains(stmts[1].SQL, "path = ? OR path LIKE ?") {
		t.Errorf("前缀删除应同时覆盖精确匹配与子路径: %s", stmts[1].SQL)
	}
	if got := stmts[1].Params[1]; got != "/p1/%" {
		t.Errorf("子路径通配期望 /p1/%%, 得到 %v", got)
	}
}

func TestRowInt64(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]any
		want int64
	}{
		{"float64", map[string]any{"v": float64(42)}, 42},
		{"json.Number", map[string]any{"v": json.Number("77")}, 77},
		{"null", map[string]any{"v": nil}, 0},
		{"缺失", map[string]any{}, 0},
		{"大时间戳不丢精度", map[string]any{"v": float64(1754000000000)}, 1754000000000},
	}
	for _, c := range cases {
		if got := rowInt64(c.row, "v"); got != c.want {
			t.Errorf("%s: 期望 %d, 得到 %d", c.name, c.want, got)
		}
	}
}

// __ 前缀 key 是同步层内部状态 (版本号等), 绝不能泄漏进业务配置,
// 否则会被当成配置字段写回内存甚至再上传
func TestRebuildConfigDataSkipsInternalKeys(t *testing.T) {
	maps := []ConfigMap{{Path: "/p1", DefaultTarget: "https://a.com", Enabled: 1}}
	others := []ConfigOther{
		{Key: configVersionKey, Value: "1700000000000"},
		{Key: "FaviconURL", Value: `"https://cdn.x/i.ico"`},
	}

	data := rebuildConfigData(maps, others)

	if _, leaked := data[configVersionKey]; leaked {
		t.Errorf("%s 不应出现在重建后的配置里", configVersionKey)
	}
	if got, ok := data["FaviconURL"].(string); !ok || got != "https://cdn.x/i.ico" {
		t.Errorf("业务 key 应正常重建, 得到 %v", data["FaviconURL"])
	}
	mapData, ok := data["MAP"].(map[string]any)
	if !ok || len(mapData) != 1 {
		t.Fatalf("MAP 重建异常: %v", data["MAP"])
	}
}

func TestExtractConfigVersion(t *testing.T) {
	if v := extractConfigVersion([]ConfigOther{{Key: configVersionKey, Value: "123"}}); v != 123 {
		t.Errorf("期望 123, 得到 %d", v)
	}
	if v := extractConfigVersion([]ConfigOther{{Key: "other", Value: "9"}}); v != 0 {
		t.Errorf("无版本行应返回 0, 得到 %d", v)
	}
	if v := extractConfigVersion([]ConfigOther{{Key: configVersionKey, Value: "not-a-number"}}); v != 0 {
		t.Errorf("损坏的版本值应返回 0, 得到 %d", v)
	}
}

func TestAdvanceConfigCursorMonotonic(t *testing.T) {
	configCursor.Store(0)
	t.Cleanup(func() { configCursor.Store(0) })

	AdvanceConfigCursor(100)
	if got := ConfigCursor(); got != 100 {
		t.Fatalf("期望 100, 得到 %d", got)
	}
	AdvanceConfigCursor(50) // 回退必须被忽略: 旧版本心跳不能把位点拉回去
	if got := ConfigCursor(); got != 100 {
		t.Errorf("位点不应回退, 得到 %d", got)
	}
}
