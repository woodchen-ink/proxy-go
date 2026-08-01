package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// 重置代际 (stats reset generation)
//
// "重置统计"要跨节点生效, 但节点间没有直连通道, 所以走 D1 的 stats_resets 表下发:
//
//	发起节点: 清零本地内存 → 一个事务里 [写 marker + 删全节点 path_stats 行] → 推进本地位点
//	其他节点: 心跳响应里收到比位点新的 marker → 清零本地内存 → 删掉自己刚写回的行 → 推进位点
//
// 发起节点删的是**全节点**的行 (仪表盘立即归零); 其他节点在下个心跳前可能又把内存值
// 写回 D1, 但它们应用 marker 时会删掉自己的行, 最多一个心跳周期内收敛。
// 位点持久化与应用逻辑在 internal/metrics/reset_sync.go, 本文件只负责 D1 侧读写。

// StatsReset 单条重置 marker; Path 为空串表示全部路径
type StatsReset struct {
	ID      int64  `json:"id"`
	Path    string `json:"path"`
	ResetAt int64  `json:"reset_at"` // ms, 同时是代际位点
}

// buildStatsResetStatements 构造"写 marker + 删全节点行"的事务语句 (纯函数, 供测试)
//
// prefix 非空时按前缀语义删行 (path 精确匹配或 path/ 子路径), 与 DeleteOwnPathStats 一致
func buildStatsResetStatements(prefix string, resetAt int64) []SQLStatement {
	stmts := []SQLStatement{{
		SQL:    "INSERT INTO stats_resets (path, reset_at) VALUES (?, ?)",
		Params: []any{prefix, resetAt},
	}}
	if prefix == "" {
		stmts = append(stmts, SQLStatement{SQL: "DELETE FROM path_stats"})
	} else {
		stmts = append(stmts, SQLStatement{
			SQL:    "DELETE FROM path_stats WHERE path = ? OR path LIKE ?",
			Params: []any{prefix, prefix + "/%"},
		})
	}
	return stmts
}

// PublishStatsReset 发布一次跨节点重置, 返回 marker 的代际时间戳 (ms)
//
// marker 与全节点行删除在同一个 D1 batch 里原子提交; 失败时调用方应降级为只删本节点行
func PublishStatsReset(ctx context.Context, prefix string) (int64, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return 0, fmt.Errorf("sync service not available")
	}

	resetAt := time.Now().UnixMilli()
	_, err := globalSyncService.manager.storage.ExecuteSQLBatch(ctx, buildStatsResetStatements(prefix, resetAt))
	if err != nil {
		return 0, err
	}
	return resetAt, nil
}

// FetchStatsResets 拉取比 since 新的 marker, 按代际升序; 启动补应用用
func FetchStatsResets(ctx context.Context, sinceMs int64) ([]StatsReset, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil, nil
	}

	rows, err := globalSyncService.manager.storage.QuerySQL(ctx,
		"SELECT id, path, reset_at FROM stats_resets WHERE reset_at > ? ORDER BY reset_at ASC LIMIT 50",
		[]any{sinceMs})
	if err != nil {
		return nil, err
	}

	out := make([]StatsReset, 0, len(rows))
	for _, row := range rows {
		path, _ := row["path"].(string)
		out = append(out, StatsReset{
			ID:      rowInt64(row, "id"),
			Path:    path,
			ResetAt: rowInt64(row, "reset_at"),
		})
	}
	return out, nil
}

// LatestStatsResetAt 返回最新 marker 的代际时间戳, 没有 marker 时返回 0
//
// 位点文件缺失时用它快进: 无法区分"新节点"与"老节点丢文件", 重放历史 marker
// 可能误清合法数据, 所以只对齐位点、不应用
func LatestStatsResetAt(ctx context.Context) (int64, error) {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return 0, nil
	}

	rows, err := globalSyncService.manager.storage.QuerySQL(ctx,
		"SELECT MAX(reset_at) AS latest FROM stats_resets", nil)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rowInt64(rows[0], "latest"), nil
}

// PruneStatsResets 清理 cutoffMs 之前的旧 marker (低频维护, 随时间序列清理触发)
func PruneStatsResets(ctx context.Context, cutoffMs int64) error {
	globalSyncMutex.RLock()
	defer globalSyncMutex.RUnlock()

	if globalSyncService == nil || !globalSyncService.isEnabled {
		return nil
	}

	_, err := globalSyncService.manager.storage.ExecuteSQLBatch(ctx, []SQLStatement{{
		SQL:    "DELETE FROM stats_resets WHERE reset_at < ?",
		Params: []any{cutoffMs},
	}})
	return err
}

// rowInt64 从通用 SQL 查询行里取整数; D1 经 JSON 解码后数字是 float64, NULL 是 nil
func rowInt64(row map[string]any, key string) int64 {
	switch v := row[key].(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case int64:
		return v
	default:
		return 0
	}
}
