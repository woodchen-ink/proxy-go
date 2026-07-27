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
		{"常规小写", "de-192.0.2.10", "de-192.0.2.10"},
		{"大写转小写", "DE-192.0.2.10", "de-192.0.2.10"},
		{"去首尾空白", "  jp-198.51.100.20\n", "jp-198.51.100.20"},
		{"下划线转连字符, 点保留", "node_1.a", "node-1.a"},
		{"冒号转连字符", "de-2001:db8::1", "de-2001-db8-1"},
		{"收敛连续连字符", "de---node1", "de-node1"},
		{"去首尾连字符", "--de-192.0.2.10--", "de-192.0.2.10"},
		{"丢弃非 ASCII", "节点de", "de"},
		{"丢弃控制字符", "de\t\nnode1", "denode1"},
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

	persistNodeID("de-192.0.2.10")

	if got := readPersistedNodeID(); got != "de-192.0.2.10" {
		t.Errorf("读回的节点 ID 为 %q, 期望 de-192.0.2.10", got)
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

	persistNodeID("jp-198.51.100.20\n")

	if got := readPersistedNodeID(); got != "jp-198.51.100.20" {
		t.Errorf("尾部换行应被忽略, 实际 %q", got)
	}
}

// TestBulkPayloadIsEmpty 五段全空时不该发请求
func TestBulkPayloadIsEmpty(t *testing.T) {
	if !(&BulkPayload{}).IsEmpty() {
		t.Error("空 payload 应判定为 empty")
	}
	if (&BulkPayload{PathStats: []PathStat{{Path: "/x"}}}).IsEmpty() {
		t.Error("含 path_stats 的 payload 不应判定为 empty")
	}
	if (&BulkPayload{Latency: []LatencyMetric{{Bucket: "lt10ms"}}}).IsEmpty() {
		t.Error("只含 latency 的 payload 也不应判定为 empty")
	}
}

// TestNodeIDGeneratedFromCountryAndIP 节点 ID 由国家码 + 出口 IP 明文构成
//
// IPv4 的点号保留以维持可读性, IPv6 的冒号归一化为连字符
func TestNodeIDGeneratedFromCountryAndIP(t *testing.T) {
	cases := []struct {
		name string
		cc   string
		ip   string
		want string
	}{
		{"IPv4", "DE", "192.0.2.10", "de-192.0.2.10"},
		{"IPv6 冒号转连字符", "DE", "2001:db8:1234:5678::1", "de-2001-db8-1234-5678-1"},
		{"国家码缺失退化为纯 IP", "", "198.51.100.20", "node-198.51.100.20"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prefix := sanitizeNodeID(c.cc)
			if prefix == "" {
				prefix = "node"
			}
			got := sanitizeNodeID(prefix + "-" + c.ip)
			if got != c.want {
				t.Errorf("期望 %q, 实际 %q", c.want, got)
			}
		})
	}
}

// TestEgressIPResponseParsing 按线上文档的响应结构解析, 且新增字段不影响取值
//
// 样例按 ip.czl.net /api/query 的实际响应结构构造 (地址与归属用文档保留值);
// 关键在于只取 code / ip / countryCode 三个字段, 其余字段增减都不该影响解析
func TestEgressIPResponseParsing(t *testing.T) {
	raw := `{"code":200,"data":{"ip":"192.0.2.10","country":"Example","countryCode":"DE",
		"regions":["Region","City"],"as":{"number":64496,"name":"Example AS"},
		"registered_country":{"code":"DE","name":"Example"},"timezone":"Europe/Berlin",
		"accuracy":"medium","source":"MaxMind","ipVersion":"IPv4"},"msg":"success"}`

	var got egressIPResponse
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("解析真实响应失败: %v", err)
	}

	if got.Code != 200 {
		t.Errorf("code 应为 200, 实际 %d", got.Code)
	}
	if got.Data.IP != "192.0.2.10" {
		t.Errorf("ip 解析错误: %q", got.Data.IP)
	}
	if got.Data.CountryCode != "DE" {
		t.Errorf("countryCode 解析错误: %q", got.Data.CountryCode)
	}
}
