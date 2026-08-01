package metrics

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	gosync "sync"
	"sync/atomic"
	"time"

	d1sync "proxy-go/pkg/sync"
)

// 重置代际的节点侧状态
//
// resetSeen 记录本节点已应用到的代际位点 (marker 的 reset_at, ms), 持久化在
// data/reset_seen (与 node_id 同一持久卷)。两条应用路径:
//
//	运行中: 每 5 分钟心跳响应带回比位点新的 marker → applyStatsResets
//	启动时: 恢复完内存计数后按文件位点补应用 → catchUpStatsResets
//
// 启动补应用堵的是这条缝: 节点在"上报了重置前的旧值"与"收到 marker"之间崩溃,
// 重启会从 D1 把旧值恢复回来 —— 文件里的位点比 marker 老, 补应用会把它清掉。

// resetSeenFile 位点文件路径
var resetSeenFile = filepath.Join("data", "reset_seen")

// resetSeenMs 已应用的代际位点 (进程内镜像, 文件是权威副本)
var resetSeenMs atomic.Int64

// resetSeenFileMu 串行化"推进位点 + 落盘": 发起端 (HTTP goroutine) 与心跳应用端
// (保存 goroutine) 可能并发推进, 只有推进与写文件在同一临界区里, 文件才不会被
// 旧值反超覆盖 (文件是跨重启的权威副本, 回退等于丢失代际)
var resetSeenFileMu gosync.Mutex

// ResetSeen 返回当前代际位点, 供心跳上行游标使用
func ResetSeen() int64 {
	return resetSeenMs.Load()
}

// markResetApplied 推进代际位点并落盘 (只进不退)
func markResetApplied(resetAt int64) {
	resetSeenFileMu.Lock()
	defer resetSeenFileMu.Unlock()

	if resetAt <= resetSeenMs.Load() {
		return
	}
	resetSeenMs.Store(resetAt)

	if err := os.MkdirAll(filepath.Dir(resetSeenFile), 0755); err != nil {
		log.Printf("[Metrics] 创建重置位点目录失败: %v", err)
		return
	}
	if err := os.WriteFile(resetSeenFile, []byte(strconv.FormatInt(resetAt, 10)), 0644); err != nil {
		log.Printf("[Metrics] 写入重置位点文件失败: %v", err)
	}
}

// catchUpStatsResets 启动时对齐重置代际, 在本节点内存计数恢复完成后调用
//
// 文件存在: 位点之后的 marker 逐个补应用 (清掉恢复回来的重置前旧值)。
// 文件缺失: 无法区分"新节点"与"老节点丢文件", 重放历史 marker 可能误清合法数据,
// 只把位点快进到最新 marker、不应用
func catchUpStatsResets(ctx context.Context) {
	if !d1sync.IsEnabled() {
		return
	}

	data, err := os.ReadFile(resetSeenFile)
	if err != nil {
		latest, qErr := d1sync.LatestStatsResetAt(ctx)
		if qErr != nil {
			log.Printf("[Metrics] 查询最新重置代际失败, 位点从当前时刻起算: %v", qErr)
			latest = time.Now().UnixMilli()
		}
		if latest > 0 {
			markResetApplied(latest)
		}
		return
	}

	seen, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if parseErr != nil {
		log.Printf("[Metrics] 重置位点文件损坏 (%v), 按缺失处理", parseErr)
		if latest, qErr := d1sync.LatestStatsResetAt(ctx); qErr == nil && latest > 0 {
			markResetApplied(latest)
		}
		return
	}
	resetSeenMs.Store(seen)

	resets, fetchErr := d1sync.FetchStatsResets(ctx, seen)
	if fetchErr != nil {
		log.Printf("[Metrics] 启动补应用重置代际失败 (下个心跳重试): %v", fetchErr)
		return
	}
	if n := applyStatsResets(ctx, resets); n > 0 {
		log.Printf("[Metrics] 启动补应用了 %d 条重置代际", n)
	}
}

// applyStatsResets 按代际升序应用 marker: 清零本地内存 + 删掉本节点在 D1 的行
//
// 删行失败时不推进位点: 下个心跳会重新带回同一 marker 再试, 代价是重复清零
// (丢几分钟增量), 换来"最终一定收敛到零"。返回实际应用的条数
func applyStatsResets(ctx context.Context, resets []d1sync.StatsReset) int {
	applied := 0
	for _, r := range resets {
		if r.ResetAt <= resetSeenMs.Load() {
			continue
		}

		n := GetCollector().zeroPathStats(r.Path)
		if err := d1sync.DeleteOwnPathStats(ctx, r.Path); err != nil {
			log.Printf("[Metrics] 应用重置代际时删除 D1 行失败 (path=%q, 下个心跳重试): %v", r.Path, err)
			break
		}
		markResetApplied(r.ResetAt)

		scope := r.Path
		if scope == "" {
			scope = "全部路径"
		}
		log.Printf("[Metrics] 已应用重置代际: %s (本地清零 %d 条, reset_at=%d)", scope, n, r.ResetAt)
		applied++
	}

	if applied > 0 {
		// 其他节点的旧值可能还躺在 peer 缓存里, 强制下次仪表盘读取重新拉 D1
		InvalidatePeerStats()
	}
	return applied
}
