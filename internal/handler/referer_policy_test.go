package handler

import (
	"testing"

	"proxy-go/internal/config"
)

func TestBuildRefererPolicy(t *testing.T) {
	rb := &config.RefererBanConfig{
		Enabled: false, // 路径默认不检查
		NodeOverrides: []config.RefererNodeOverride{
			// 所有节点: /b2/random-video* 只允许 my.com 引用, 且拒绝空 Referer
			{Paths: []string{"/b2/random-video*"}, Enabled: true, Mode: config.RefererModeWhitelist, Hosts: []string{"my.com"}, BlockEmpty: true},
			// jp-1 上 /b2/public 下不检查 (排在后面, 不影响上一条)
			{Nodes: []string{"jp-1"}, Paths: []string{"/b2/public"}, Enabled: false},
		},
	}
	p := BuildRefererPolicy(rb, "us-1", true)
	cases := []struct {
		path, referer string
		blocked       bool
	}{
		{"/b2/random-video.mp4", "", true},
		{"/b2/random-video/1.mp4", "https://evil.com/x", true},
		{"/b2/random-video/1.mp4", "https://www.my.com/x", false},
		{"/b2/other.mp4", "", false}, // 默认规则关闭
	}
	for _, c := range cases {
		if got := p.IsBlocked(c.path, c.referer); got != c.blocked {
			t.Errorf("%s referer=%q: got %v want %v", c.path, c.referer, got, c.blocked)
		}
	}

	// 默认开启黑名单, 例外对某子路径关闭检查
	rb2 := &config.RefererBanConfig{
		Enabled: true, Hosts: []string{"bad.com"},
		NodeOverrides: []config.RefererNodeOverride{{Nodes: []string{"jp-1"}, Paths: []string{"/b2/public"}, Enabled: false}},
	}
	jp := BuildRefererPolicy(rb2, "jp-1", true)
	if jp.IsBlocked("/b2/public/a.jpg", "https://bad.com") {
		t.Error("jp-1 上 /b2/public 应放开")
	}
	if !jp.IsBlocked("/b2/publicx", "https://bad.com") || !jp.IsBlocked("/b2/a.jpg", "https://bad.com") {
		t.Error("例外范围之外仍走默认黑名单")
	}
	if us := BuildRefererPolicy(rb2, "us-1", true); !us.IsBlocked("/b2/public/a.jpg", "https://bad.com") {
		t.Error("其他节点不受 jp-1 的例外影响")
	}
	// 全局规则 (allowWhitelist=false) 把白名单按黑名单编译
	g := BuildRefererPolicy(&config.RefererBanConfig{Enabled: true, Mode: config.RefererModeWhitelist, Hosts: []string{"x.com"}}, "", false)
	if g.IsBlocked("/a", "https://y.com") || !g.IsBlocked("/a", "https://x.com") {
		t.Error("全局规则应恒为黑名单")
	}
	if BuildRefererPolicy(&config.RefererBanConfig{}, "", true) != nil {
		t.Error("无规则时应返回 nil")
	}
}
