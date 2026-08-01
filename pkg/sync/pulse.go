package sync

import (
	"log"
	"proxy-go/internal/config"
	"sync/atomic"
)

// 心跳通道 (pulse)
//
// 节点间零互连, 所有跨节点状态流动收敛到每 5 分钟一次的 bulk 上报里:
// 上行捎游标 (本节点已应用到哪), 下行捎增量 (比游标新的配置 / 重置代际)。
// 请求数不因此增加 —— 全部搭在既有上报请求上。
//
// 下行槽位:
//   - config:  __config_version 比游标新时, 响应带回完整配置, 节点热应用 (不重启生效)
//   - resets:  重置代际 marker, 见 stats_reset.go
//   - bans:    协议已预留 (worker 侧按 bans_seen 游标查询), Go 侧封禁同步子系统落地后启用

// PulseCursors 上行游标: 声明本节点已应用的状态位点, worker 只回传比它新的部分
type PulseCursors struct {
	ConfigVersion int64 `json:"config_version"`
	ResetSeen     int64 `json:"reset_seen"`
}

// PulseState 下行状态: 各槽位为空表示没有比游标新的内容
type PulseState struct {
	ConfigVersion int64         `json:"config_version"`
	ConfigMaps    []ConfigMap   `json:"config_maps"`
	ConfigOther   []ConfigOther `json:"config_other"`
	Resets        []StatsReset  `json:"resets"`
}

// configCursor 本节点已应用的配置版本 (ms 时间戳)
//
// 三处推进: 启动下载成功 (取远端 __config_version)、本地修改配置 (置为当前时刻,
// 挡住上传完成前的旧版本心跳回灌)、心跳热应用成功 (取应用的版本)
var configCursor atomic.Int64

// ConfigCursor 返回本节点当前配置版本位点
func ConfigCursor() int64 {
	return configCursor.Load()
}

// AdvanceConfigCursor 单调推进配置位点 (只进不退)
func AdvanceConfigCursor(v int64) {
	for {
		cur := configCursor.Load()
		if v <= cur || configCursor.CompareAndSwap(cur, v) {
			return
		}
	}
}

// ApplyPulseConfig 把心跳带回的远端配置热应用到本节点
//
// 触发条件: 远端版本比本节点位点新且带了配置数据。应用走 config.ApplyRemoteConfig
// (只触发本地回调, 不触发回写 D1 的 sync 回调), 防止 A 改 → B 收 → B 又传的回声循环。
// 应用成功即视为与 D1 对齐, 顺带解除"配置只读"模式 (启动时拉取失败的节点由此自愈)
func ApplyPulseConfig(state *PulseState) {
	if state == nil || state.ConfigVersion <= ConfigCursor() {
		return
	}
	if len(state.ConfigMaps) == 0 && len(state.ConfigOther) == 0 {
		return
	}

	data := rebuildConfigData(state.ConfigMaps, state.ConfigOther)
	if err := config.ApplyRemoteConfig(data); err != nil {
		log.Printf("[Pulse] 远端配置热应用失败 (version=%d): %v", state.ConfigVersion, err)
		return
	}

	AdvanceConfigCursor(state.ConfigVersion)
	markConfigTrusted()
	log.Printf("[Pulse] 已热应用远端配置: %d 个路径映射, version=%d", len(state.ConfigMaps), state.ConfigVersion)
}

// markConfigTrusted 心跳拿到权威配置后解除只读模式
func markConfigTrusted() {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()
	if globalSyncService != nil && globalSyncService.manager != nil {
		globalSyncService.manager.MarkConfigTrusted()
	}
}
