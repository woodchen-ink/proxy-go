package sync

import (
	"context"
	"fmt"
	"log"
	"time"
)

// schema 演进定义
//
// 设计要点:
//   - 所有 schema 变更由 Go 端持有, worker 只做"哑 SQL 执行器"
//   - 加新表 / 索引 → 追加一个 Migration 项即可, Docker 镜像发版自动生效
//   - 每个 Migration 是 idempotent 的 (全部使用 IF NOT EXISTS), 重跑不破坏数据
//   - app_migrations 跟踪表去重, 同一 id 不会再次执行
//   - 不要修改既有 Migration 项, 只能追加; 修改既有项不会重新执行 (已记录到 app_migrations),
//     等于改了寂寞; 真要改 schema 就追加新 Migration
//   - 不与 wrangler 的 d1_migrations 跟踪表混用, 互不干扰
//
// 与历史数据兼容:
//   - 旧 worker (v2) 在 app_migrations 里写过 0001..0004, 新 Go 端用相同 id 全部 skip 不重跑

// Migration 单次 schema 变更
type Migration struct {
	ID         string   // 唯一 id (用文件名风格, 字典序即应用顺序)
	Statements []string // 必须全部 idempotent
}

// migrations 按 id 字典序应用; 新增追加到末尾, 不修改既有项
var migrations = []Migration{
	{
		ID: "0001_initial_schema",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS path_stats (
				path TEXT PRIMARY KEY,
				request_count INTEGER DEFAULT 0,
				error_count INTEGER DEFAULT 0,
				bytes_transferred INTEGER DEFAULT 0,
				status_2xx INTEGER DEFAULT 0,
				status_3xx INTEGER DEFAULT 0,
				status_4xx INTEGER DEFAULT 0,
				status_5xx INTEGER DEFAULT 0,
				cache_hits INTEGER DEFAULT 0,
				cache_misses INTEGER DEFAULT 0,
				cache_hit_rate REAL DEFAULT 0.0,
				bytes_saved INTEGER DEFAULT 0,
				avg_latency TEXT,
				last_access_time INTEGER,
				updated_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_path_stats_request_count ON path_stats(request_count DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_path_stats_updated_at ON path_stats(updated_at)`,
			`CREATE INDEX IF NOT EXISTS idx_path_stats_last_access ON path_stats(last_access_time)`,

			`CREATE TABLE IF NOT EXISTS banned_ips (
				ip TEXT PRIMARY KEY,
				ban_time INTEGER NOT NULL,
				ban_end_time INTEGER NOT NULL,
				reason TEXT,
				error_count INTEGER DEFAULT 0,
				is_active BOOLEAN DEFAULT 1,
				unban_time INTEGER,
				unban_reason TEXT,
				updated_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_banned_ips_ban_end_time ON banned_ips(ban_end_time)`,
			`CREATE INDEX IF NOT EXISTS idx_banned_ips_is_active ON banned_ips(is_active)`,
			`CREATE INDEX IF NOT EXISTS idx_banned_ips_updated_at ON banned_ips(updated_at)`,

			`CREATE TABLE IF NOT EXISTS banned_ips_history (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				ip TEXT NOT NULL,
				ban_time INTEGER NOT NULL,
				ban_end_time INTEGER NOT NULL,
				reason TEXT,
				error_count INTEGER DEFAULT 0,
				unban_time INTEGER,
				unban_reason TEXT,
				created_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_banned_ips_history_ip ON banned_ips_history(ip)`,
			`CREATE INDEX IF NOT EXISTS idx_banned_ips_history_created_at ON banned_ips_history(created_at DESC)`,

			`CREATE TABLE IF NOT EXISTS config_maps (
				path TEXT PRIMARY KEY,
				default_target TEXT NOT NULL,
				enabled BOOLEAN DEFAULT 1,
				extension_rules TEXT,
				cache_config TEXT,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_config_maps_enabled ON config_maps(enabled)`,
			`CREATE INDEX IF NOT EXISTS idx_config_maps_updated_at ON config_maps(updated_at)`,

			`CREATE TABLE IF NOT EXISTS config_other (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL,
				description TEXT,
				updated_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_config_other_updated_at ON config_other(updated_at)`,
		},
	},
	{
		ID: "0002_add_metrics_tables",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS status_codes (
				status_code TEXT PRIMARY KEY,
				count INTEGER DEFAULT 0,
				updated_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_status_codes_updated_at ON status_codes(updated_at)`,
			`CREATE INDEX IF NOT EXISTS idx_status_codes_count ON status_codes(count DESC)`,

			`CREATE TABLE IF NOT EXISTS latency_distribution (
				bucket TEXT PRIMARY KEY,
				count INTEGER DEFAULT 0,
				updated_at INTEGER NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_latency_distribution_updated_at ON latency_distribution(updated_at)`,
			`CREATE INDEX IF NOT EXISTS idx_latency_distribution_count ON latency_distribution(count DESC)`,

			`INSERT OR IGNORE INTO latency_distribution (bucket, count, updated_at) VALUES ('lt10ms', 0, 0)`,
			`INSERT OR IGNORE INTO latency_distribution (bucket, count, updated_at) VALUES ('10-50ms', 0, 0)`,
			`INSERT OR IGNORE INTO latency_distribution (bucket, count, updated_at) VALUES ('50-200ms', 0, 0)`,
			`INSERT OR IGNORE INTO latency_distribution (bucket, count, updated_at) VALUES ('200-1000ms', 0, 0)`,
			`INSERT OR IGNORE INTO latency_distribution (bucket, count, updated_at) VALUES ('gt1s', 0, 0)`,
		},
	},
	{
		ID: "0003_add_path_timeseries",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS path_timeseries (
				path TEXT NOT NULL,
				ts_hour INTEGER NOT NULL,
				node_id TEXT NOT NULL DEFAULT 'default',
				requests INTEGER DEFAULT 0,
				bytes INTEGER DEFAULT 0,
				errors INTEGER DEFAULT 0,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (path, ts_hour, node_id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_path_timeseries_ts_hour ON path_timeseries(ts_hour DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_path_timeseries_path ON path_timeseries(path)`,
			`CREATE INDEX IF NOT EXISTS idx_path_timeseries_updated_at ON path_timeseries(updated_at)`,
		},
	},
	{
		ID: "0004_add_referer_daily",
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS referer_daily (
				host TEXT NOT NULL,
				ts_date INTEGER NOT NULL,
				node_id TEXT NOT NULL DEFAULT 'default',
				requests INTEGER DEFAULT 0,
				bytes INTEGER DEFAULT 0,
				errors INTEGER DEFAULT 0,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (host, ts_date, node_id)
			)`,
			`CREATE INDEX IF NOT EXISTS idx_referer_daily_ts_date ON referer_daily(ts_date DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_referer_daily_host ON referer_daily(host)`,
			`CREATE INDEX IF NOT EXISTS idx_referer_daily_updated_at ON referer_daily(updated_at)`,
		},
	},
}

// MigrationResult 启动期迁移汇总, 仅用于日志
type MigrationResult struct {
	Applied []string // 本次新执行的 id
	Skipped []string // 已在 app_migrations 中跳过的 id
}

// runMigrations 把未应用的 Migration 推到 D1
//
// 行为:
//  1. 先用 batch 语义建 app_migrations 跟踪表 (idempotent), 与第一次 query 解耦
//  2. 查现有 app_migrations, 已应用 id 跳过
//  3. 未应用的逐个推: 每个 Migration 单独发一个 batch (含全部 SQL + 末尾 INSERT INTO app_migrations)
//     - 单 Migration 原子提交 (D1 batch 是事务的), 部分失败时不会污染 app_migrations
//     - 不一次性把所有未应用的塞进一个 batch, 是为了让某个迁移失败不阻断后续迁移记录
//
// 失败语义: 第一个失败的 Migration 终止, 已成功的保留, 下次启动从失败处续跑
func runMigrations(ctx context.Context, c *D1Client) (*MigrationResult, error) {
	// Step 1: 跟踪表 (idempotent)
	if _, err := c.ExecuteSQLBatch(ctx, []SQLStatement{
		{SQL: `CREATE TABLE IF NOT EXISTS app_migrations (
			id TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`},
	}); err != nil {
		return nil, fmt.Errorf("ensure app_migrations table: %w", err)
	}

	// Step 2: 读已应用 id
	rows, err := c.QuerySQL(ctx, "SELECT id FROM app_migrations", nil)
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	applied := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if id, ok := row["id"].(string); ok {
			applied[id] = struct{}{}
		}
	}

	// Step 3: 按字典序推未应用的, 单 Migration 单 batch
	result := &MigrationResult{}
	now := time.Now().UnixMilli()
	for _, m := range migrations {
		if _, ok := applied[m.ID]; ok {
			result.Skipped = append(result.Skipped, m.ID)
			continue
		}
		stmts := make([]SQLStatement, 0, len(m.Statements)+1)
		for _, sql := range m.Statements {
			stmts = append(stmts, SQLStatement{SQL: sql})
		}
		stmts = append(stmts, SQLStatement{
			SQL:    `INSERT INTO app_migrations (id, applied_at) VALUES (?, ?)`,
			Params: []any{m.ID, now},
		})
		if _, err := c.ExecuteSQLBatch(ctx, stmts); err != nil {
			return result, fmt.Errorf("apply migration %s: %w", m.ID, err)
		}
		result.Applied = append(result.Applied, m.ID)
		log.Printf("[Sync] Migration applied: %s", m.ID)
	}

	return result, nil
}
