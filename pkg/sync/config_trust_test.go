package sync

import (
	"context"
	"testing"
)

// 配置信任闸门的回归测试
//
// 背景: UploadConfig 会删除 D1 中本地不存在的路径。启动时若拉取 D1 失败, 进程会退回
// 本地兜底配置 (可能只是仓库自带的样例), 此时一旦触发上传就会抹掉整个线上配置。
// 线上曾因 worker 瞬时 502 走到过这一步, 只是碰巧被另一个 bug 挡住才没酿成事故。

// TestSyncConfigToD1BlockedWhenUntrusted 未确认远端状态时必须完全不碰上传路径
//
// 用 nil storage / nil configLoader 构造 manager: 只要守卫失效往下走一步就会 panic,
// 因此本用例能真正证明"一行都没往下执行", 而不只是返回值对
func TestSyncConfigToD1BlockedWhenUntrusted(t *testing.T) {
	m := &D1Manager{} // configTrusted 零值即 false
	m.status.Store(SyncStatus{})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("不可信时不该触碰 storage / configLoader, 却 panic 了: %v", r)
		}
	}()

	if err := m.syncConfigToD1(context.Background()); err != nil {
		t.Errorf("跳过上传应返回 nil (非错误), 实际 %v", err)
	}
}

// TestConfigTrustDefaultsToFalse 默认不可信: 没确认过远端就不许写
func TestConfigTrustDefaultsToFalse(t *testing.T) {
	m := &D1Manager{}
	if m.configTrusted.Load() {
		t.Error("configTrusted 默认必须为 false, 否则拉取失败的进程也能覆盖线上配置")
	}
}

// TestMarkConfigTrustedRecovers 拉取成功后放开上传, 让只读节点无需重启即可恢复
func TestMarkConfigTrustedRecovers(t *testing.T) {
	m := &D1Manager{}

	m.MarkConfigTrusted()

	if !m.configTrusted.Load() {
		t.Error("MarkConfigTrusted 后应变为可信")
	}

	// 重复调用需幂等, 不应重复打日志或翻转状态
	m.MarkConfigTrusted()
	if !m.configTrusted.Load() {
		t.Error("重复调用不应把状态翻回去")
	}
}

// TestToGenericMap 本地配置是结构体指针, 必须能转成通用 map
//
// 旧代码在这里直接做 map[string]any 断言, 对 *config.Config 恒失败,
// 导致"远端为空时播种本地配置"这条路从未真正生效
func TestToGenericMap(t *testing.T) {
	type pathCfg struct {
		DefaultTarget string `json:"DefaultTarget"`
	}
	type cfg struct {
		MAP map[string]pathCfg `json:"MAP"`
	}

	got, err := toGenericMap(&cfg{MAP: map[string]pathCfg{"/a": {DefaultTarget: "https://example.com"}}})
	if err != nil {
		t.Fatalf("结构体指针转换失败: %v", err)
	}

	m, ok := got["MAP"].(map[string]any)
	if !ok {
		t.Fatalf("MAP 字段类型不对: %T", got["MAP"])
	}
	entry, ok := m["/a"].(map[string]any)
	if !ok {
		t.Fatalf("路径项类型不对: %T", m["/a"])
	}
	if entry["DefaultTarget"] != "https://example.com" {
		t.Errorf("DefaultTarget 丢失或错误: %v", entry["DefaultTarget"])
	}
}

// TestToGenericMapPassesThroughMap 已经是 map 的直接返回, 不做多余序列化
func TestToGenericMapPassesThroughMap(t *testing.T) {
	src := map[string]any{"MAP": map[string]any{}}

	got, err := toGenericMap(src)
	if err != nil {
		t.Fatalf("map 透传失败: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("期望原样返回, 实际 %+v", got)
	}
}
