package config

import "testing"

func TestRefererRulesForNode(t *testing.T) {
	rb := &RefererBanConfig{
		Enabled: true, Hosts: []string{"bad.com"},
		NodeOverrides: []RefererNodeOverride{
			{Paths: []string{"/b2/random-video*"}, Enabled: true, Mode: RefererModeWhitelist, Hosts: []string{"my.com"}},
			{Nodes: []string{"jp-1", " jp-2 "}, Enabled: false},
			{Nodes: []string{}, Paths: []string{" "}}, // 没有范围, 忽略
		},
	}
	if got := rb.RulesForNode("us-1"); len(got) != 1 || got[0].Paths[0] != "/b2/random-video*" {
		t.Fatalf("us-1 只应命中按路径的例外: %+v", got)
	}
	if got := rb.RulesForNode("jp-2"); len(got) != 2 {
		t.Fatalf("jp-2 应命中路径例外 + 节点例外 (Nodes 两侧空白忽略): %+v", got)
	}
	if got := rb.RulesForNode(""); len(got) != 1 {
		t.Fatalf("节点 ID 未知时只有不限节点的例外生效: %+v", got)
	}
	var nilCfg *RefererBanConfig
	if nilCfg.RulesForNode("x") != nil {
		t.Fatal("nil 配置应返回 nil")
	}
}
