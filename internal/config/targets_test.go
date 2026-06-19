package config

import (
	"reflect"
	"testing"
)

// TestGetTargets 覆盖单源 / 多源 / 空白剔除 / 全空
func TestGetTargets(t *testing.T) {
	cases := []struct {
		name string
		pc   PathConfig
		want []string
	}{
		{"single", PathConfig{DefaultTarget: "https://a.com"}, []string{"https://a.com"}},
		{"multi", PathConfig{DefaultTargets: []string{"https://a.com", "https://b.com"}}, []string{"https://a.com", "https://b.com"}},
		{"multi trims blanks", PathConfig{DefaultTargets: []string{" https://a.com ", "", "  "}}, []string{"https://a.com"}},
		{"multi empty falls back to single", PathConfig{DefaultTarget: "https://a.com", DefaultTargets: []string{"", "  "}}, []string{"https://a.com"}},
		{"all empty", PathConfig{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.pc.GetTargets()
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("GetTargets() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestNormalizeTargets 验证归一: 回填 DefaultTarget / 单源退化清空列表 / 多源保留
func TestNormalizeTargets(t *testing.T) {
	// 多源: DefaultTarget 为空时用第一个回填, 列表保留
	pc := PathConfig{DefaultTargets: []string{"https://a.com", "https://b.com"}}
	normalizeTargets(&pc)
	if pc.DefaultTarget != "https://a.com" {
		t.Fatalf("expected DefaultTarget backfilled to first, got %q", pc.DefaultTarget)
	}
	if len(pc.DefaultTargets) != 2 {
		t.Fatalf("expected 2 targets retained, got %d", len(pc.DefaultTargets))
	}

	// 单源 (列表只 1 项): 退化为 DefaultTarget, 清空列表
	pc2 := PathConfig{DefaultTargets: []string{"https://only.com"}}
	normalizeTargets(&pc2)
	if pc2.DefaultTarget != "https://only.com" {
		t.Fatalf("expected DefaultTarget=%q, got %q", "https://only.com", pc2.DefaultTarget)
	}
	if pc2.DefaultTargets != nil {
		t.Fatalf("expected single-source to clear DefaultTargets, got %v", pc2.DefaultTargets)
	}

	// 空白项剔除后为空: 不动 DefaultTarget
	pc3 := PathConfig{DefaultTarget: "https://keep.com", DefaultTargets: []string{"", "  "}}
	normalizeTargets(&pc3)
	if pc3.DefaultTarget != "https://keep.com" || len(pc3.DefaultTargets) != 0 {
		t.Fatalf("unexpected normalize result: target=%q list=%v", pc3.DefaultTarget, pc3.DefaultTargets)
	}
}
