package service

import (
	"proxy-go/internal/config"
	"testing"
)

// 回归测试: /mirror 系统路径的 DefaultTarget 是 ensureMirrorPath 自动补的哨兵值
// ("mirror"), 不是真实 URL。validateConfig 加严 URL 校验 (要求 http/https scheme)
// 时曾把它当非法值拒绝, 导致后台任何一次保存都必现失败——前端把 GET
// /admin/api/config/get 返回的整份配置原样回传, 必然带着这个自动注入的条目。
func TestValidateConfigAllowsMirrorSentinel(t *testing.T) {
	s := &ConfigService{}
	cfg := &config.Config{
		MAP: map[string]config.PathConfig{
			"/mirror": {
				DefaultTarget: config.MirrorTargetMarker,
				Enabled:       true,
			},
			"/p1": {
				DefaultTarget: "https://example.com",
				Enabled:       true,
			},
		},
	}

	if err := s.validateConfig(cfg); err != nil {
		t.Fatalf("含 /mirror 系统路径的合法配置不应被拒绝: %v", err)
	}
}

// 确认放行 mirror 哨兵没有连带放宽真实路径的 URL 校验
func TestValidateConfigRejectsNonURLTarget(t *testing.T) {
	s := &ConfigService{}
	cfg := &config.Config{
		MAP: map[string]config.PathConfig{
			"/p1": {
				DefaultTarget: "not-a-url",
				Enabled:       true,
			},
		},
	}

	if err := s.validateConfig(cfg); err == nil {
		t.Fatal("非 http/https 的目标应被拒绝")
	}
}
