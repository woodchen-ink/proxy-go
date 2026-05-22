/**
 * 内置 migration 列表
 *
 * 与 cloudflare-worker/migrations/*.sql 对齐, 但全部使用 CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS,
 * 保证在已部署的 D1 上重复执行不会破坏数据 (不会 DROP). worker 自己维护 app_migrations 跟踪表,
 * 已应用过的 id 不再重跑; 不与 wrangler 的 d1_migrations 表混用, 互不影响
 */

export interface Migration {
	id: string;
	statements: string[];
}

// MIGRATIONS 按 id 字典序应用; 新增 schema 变更追加到末尾, 不要修改既有项
export const MIGRATIONS: Migration[] = [
	{
		id: '0001_initial_schema',
		statements: [
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
		],
	},
	{
		id: '0002_add_metrics_tables',
		statements: [
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
		],
	},
	{
		id: '0003_add_path_timeseries',
		statements: [
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
		],
	},
	{
		id: '0004_add_referer_daily',
		statements: [
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
		],
	},
];

// MigrationResult 单次 apply 调用的返回结果
export interface MigrationResult {
	applied: string[];
	skipped: string[];
}

// applyPendingMigrations 执行所有未应用的 migration, 通过 app_migrations 表去重
//
// 失败时立即抛错, 已成功的 migration 已经写入 app_migrations, 下次调用不再重跑;
// 失败的 migration 不会写入 app_migrations, 下次调用会重试
export async function applyPendingMigrations(db: D1Database): Promise<MigrationResult> {
	// 跟踪表自身用 IF NOT EXISTS 创建, 不进入 migration 列表 (避免循环)
	await db
		.prepare(
			`CREATE TABLE IF NOT EXISTS app_migrations (
				id TEXT PRIMARY KEY,
				applied_at INTEGER NOT NULL
			)`
		)
		.run();

	const applied = new Set<string>();
	const rows = await db.prepare(`SELECT id FROM app_migrations`).all<{ id: string }>();
	for (const r of rows.results || []) {
		applied.add(r.id);
	}

	const result: MigrationResult = { applied: [], skipped: [] };
	const now = Date.now();

	for (const m of MIGRATIONS) {
		if (applied.has(m.id)) {
			result.skipped.push(m.id);
			continue;
		}
		// 逐条执行 statement; D1 不支持单 prepare 中多语句, 只能拆开
		for (const stmt of m.statements) {
			await db.prepare(stmt).run();
		}
		await db
			.prepare(`INSERT INTO app_migrations (id, applied_at) VALUES (?1, ?2)`)
			.bind(m.id, now)
			.run();
		result.applied.push(m.id);
	}

	return result;
}
