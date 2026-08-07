package config

import (
	"encoding/json"
	"os"
	"testing"
)

// 兜底文件缺失时生成的默认配置必须是空白的:
// 历史上这里会伪造 /test → localhost:8080 样例, 与生产兜底身份重叠,
// D1 拉取失败时节点会顶着假配置对外服务 —— 不允许回退到那个行为
func TestCreateDefaultConfigIsBlank(t *testing.T) {
	path := t.TempDir() + "/config.json"

	cm, err := NewConfigManager(path)
	if err != nil {
		t.Fatalf("首次运行应能生成默认配置: %v", err)
	}

	cfg := cm.GetConfig()
	if _, hasTest := cfg.MAP["/test"]; hasTest {
		t.Errorf("默认配置不应再包含 /test 样例路径")
	}
	for p, pc := range cfg.MAP {
		// ensureMirrorPath 会补系统路径 /mirror, 除此之外不允许出现任何路径
		if pc.DefaultTarget != MirrorTargetMarker {
			t.Errorf("默认配置出现了非系统路径 %s → %s", p, pc.DefaultTarget)
		}
	}
}

// 快照写盘后必须能被 NewConfigManager 原样加载 —— 它就是 D1 不可达时的兜底来源
func TestSaveSnapshotRoundTrip(t *testing.T) {
	path := t.TempDir() + "/config.json"

	cm, err := NewConfigManagerFromData(path, map[string]any{
		"MAP": map[string]any{
			"/p1": map[string]any{"DefaultTarget": "https://a.com", "Enabled": true},
		},
	})
	if err != nil {
		t.Fatalf("从数据创建配置管理器失败: %v", err)
	}

	cm.SaveSnapshot()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("快照文件应已写入: %v", err)
	}
	var snap Config
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("快照应是合法 JSON 配置: %v", err)
	}
	if pc, ok := snap.MAP["/p1"]; !ok || pc.DefaultTarget != "https://a.com" {
		t.Errorf("快照应包含 /p1 → https://a.com, 得到 %+v", snap.MAP)
	}

	// 快照能作为兜底被重新加载
	reloaded, err := NewConfigManager(path)
	if err != nil {
		t.Fatalf("快照应能被重新加载: %v", err)
	}
	if pc, ok := reloaded.GetConfig().MAP["/p1"]; !ok || pc.DefaultTarget != "https://a.com" {
		t.Errorf("重载后应保留 /p1, 得到 %+v", reloaded.GetConfig().MAP)
	}
}
