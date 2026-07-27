package sync

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// 这里只覆盖纯逻辑与本地文件读写, 不触发对 ip.czl.net 的真实请求:
// 出口 IP 解析依赖运行环境的网络位置, 在开发机上跑不出有意义的结果

// TestSanitizeNodeID 归一化规则
func TestSanitizeNodeID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"常规小写", "de-82468f", "de-82468f"},
		{"大写转小写", "DE-82468F", "de-82468f"},
		{"去首尾空白", "  jp-56bf35\n", "jp-56bf35"},
		{"下划线与点转连字符", "node_1.a", "node-1-a"},
		{"收敛连续连字符", "de---82468f", "de-82468f"},
		{"去首尾连字符", "--de-82468f--", "de-82468f"},
		{"丢弃非 ASCII", "节点de", "de"},
		{"丢弃控制字符", "de\t\n82468f", "de82468f"},
		{"空串", "", ""},
		{"纯空白", "   \n\t ", ""},
		{"纯非法字符", "中文节点", ""},
		{"纯连字符", "---", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitizeNodeID(c.in); got != c.want {
				t.Errorf("sanitizeNodeID(%q) = %q, 期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestSanitizeNodeIDTruncatesLongInput 超长输入截断后不能留下尾部连字符
func TestSanitizeNodeIDLength(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	got := sanitizeNodeID(long)
	if len(got) != 64 {
		t.Errorf("超长输入应截断到 64 字符, 实际 %d", len(got))
	}
}

// TestPersistAndReadNodeID 固化后应能原样读回
func TestPersistAndReadNodeID(t *testing.T) {
	orig := nodeIDFile
	nodeIDFile = filepath.Join(t.TempDir(), "data", "node_id")
	t.Cleanup(func() { nodeIDFile = orig })

	if got := readPersistedNodeID(); got != "" {
		t.Errorf("文件不存在时应返回空串, 实际 %q", got)
	}

	persistNodeID("de-82468f")

	if got := readPersistedNodeID(); got != "de-82468f" {
		t.Errorf("读回的节点 ID 为 %q, 期望 de-82468f", got)
	}
}

// TestReadPersistedNodeIDRejectsGarbage 文件内容非法时按"未固化"处理, 让上层重新解析
func TestReadPersistedNodeIDRejectsGarbage(t *testing.T) {
	orig := nodeIDFile
	nodeIDFile = filepath.Join(t.TempDir(), "node_id")
	t.Cleanup(func() { nodeIDFile = orig })

	persistNodeID("   \n  ")

	if got := readPersistedNodeID(); got != "" {
		t.Errorf("空白内容应视为未固化, 实际 %q", got)
	}
}

// TestPersistedNodeIDTolerantOfTrailingNewline 手工编辑留下的换行不该导致 ID 漂移
func TestPersistedNodeIDTolerantOfTrailingNewline(t *testing.T) {
	orig := nodeIDFile
	nodeIDFile = filepath.Join(t.TempDir(), "node_id")
	t.Cleanup(func() { nodeIDFile = orig })

	persistNodeID("jp-56bf35\n")

	if got := readPersistedNodeID(); got != "jp-56bf35" {
		t.Errorf("尾部换行应被忽略, 实际 %q", got)
	}
}

// TestEgressIPResponseParsing 按线上文档的响应结构解析, 且新增字段不影响取值
//
// 样例取自 ip.czl.net /api/query 的真实返回 (2026-07-27 实测)
func TestEgressIPResponseParsing(t *testing.T) {
	raw := `{"code":200,"data":{"ip":"152.53.253.143","country":"德国","countryCode":"DE",
		"regions":["巴伐利亚州","纽伦堡"],"as":{"number":197540,"name":"netcup GmbH"},
		"registered_country":{"code":"AT","name":"奥地利"},"timezone":"Europe/Berlin",
		"accuracy":"medium","source":"MaxMind","ipVersion":"IPv4"},"msg":"success"}`

	var got egressIPResponse
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("解析真实响应失败: %v", err)
	}

	if got.Code != 200 {
		t.Errorf("code 应为 200, 实际 %d", got.Code)
	}
	if got.Data.IP != "152.53.253.143" {
		t.Errorf("ip 解析错误: %q", got.Data.IP)
	}
	if got.Data.CountryCode != "DE" {
		t.Errorf("countryCode 解析错误: %q", got.Data.CountryCode)
	}
}
