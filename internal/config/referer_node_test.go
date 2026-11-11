package config

import "testing"

func TestRefererBanForNode(t *testing.T) {
	base := &RefererBanConfig{
		Enabled: true, Mode: RefererModeBlacklist, Hosts: []string{"bad.com"},
		NodeOverrides: []RefererNodeOverride{
			{Nodes: []string{"CN-1.1.1.1", " HK-2.2.2.2 "}, Enabled: true, Mode: RefererModeWhitelist, Hosts: []string{"my.com"}, BlockEmpty: true},
			{Nodes: []string{"HK-2.2.2.2"}, Enabled: false},
		},
	}

	if got := base.ForNode("US-3.3.3.3"); got != base {
		t.Fatalf("未命中覆盖应返回默认规则")
	}
	if got := base.ForNode(""); got != base {
		t.Fatalf("节点 ID 未知时应走默认规则")
	}
	got := base.ForNode("HK-2.2.2.2") // 两条都含 HK, 先命中先用; Nodes 两侧空白忽略
	if !got.IsWhitelist() || !got.BlockEmpty || got.Hosts[0] != "my.com" || len(got.NodeOverrides) != 0 {
		t.Fatalf("应命中第一条覆盖: %+v", got)
	}
	var nilCfg *RefererBanConfig
	if nilCfg.ForNode("CN-1.1.1.1") != nil {
		t.Fatalf("nil 配置应返回 nil")
	}
}
