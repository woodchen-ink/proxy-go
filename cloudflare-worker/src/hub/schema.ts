/**
 * 中控 DO 的 SQLite schema (控制面状态)
 *
 * D1 仍存节点上报的统计与路径配置 (老协议共用); 只属于中控的状态放 DO 内置 SQLite:
 * 强一致、与 WS 连接同进程, 不需要跨服务事务。语句全部 idempotent, 每次 DO 冷启动执行一遍。
 * 加列只能追加 ALTER (用 addColumn 判重), 不改既有语句。
 */

const STATEMENTS = [
	`CREATE TABLE IF NOT EXISTS nodes (
		node_id TEXT PRIMARY KEY,
		ipv4 TEXT NOT NULL DEFAULT '',
		ipv6 TEXT NOT NULL DEFAULT '',
		version TEXT NOT NULL DEFAULT '',
		hostname TEXT NOT NULL DEFAULT '',
		origin_host TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		first_seen INTEGER NOT NULL,
		last_seen INTEGER NOT NULL,
		state TEXT NOT NULL DEFAULT 'up',
		state_since INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS commands (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		payload TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS command_acks (
		seq INTEGER NOT NULL,
		node_id TEXT NOT NULL,
		ok INTEGER NOT NULL,
		message TEXT NOT NULL DEFAULT '',
		result TEXT NOT NULL DEFAULT '',
		acked_at INTEGER NOT NULL,
		PRIMARY KEY (seq, node_id)
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		at INTEGER NOT NULL,
		level TEXT NOT NULL,
		kind TEXT NOT NULL,
		subject TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_events_at ON events(at)`,
	`CREATE TABLE IF NOT EXISTS dns_credentials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		secret TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS dns_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		hostname TEXT NOT NULL UNIQUE,
		enabled INTEGER NOT NULL DEFAULT 1,
		cf_credential_id INTEGER NOT NULL DEFAULT 0,
		saas_zone_id TEXT NOT NULL DEFAULT '',
		saas_zone_name TEXT NOT NULL DEFAULT '',
		dnspod_credential_id INTEGER NOT NULL DEFAULT 0,
		dnspod_domain TEXT NOT NULL DEFAULT '',
		record_name TEXT NOT NULL DEFAULT '@',
		record_line TEXT NOT NULL DEFAULT '默认',
		pool TEXT NOT NULL DEFAULT '[]',
		active_node TEXT NOT NULL DEFAULT '',
		auto_failover INTEGER NOT NULL DEFAULT 1,
		auto_failback INTEGER NOT NULL DEFAULT 1,
		last_switch_at INTEGER NOT NULL DEFAULT 0,
		last_check TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		updated_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS probes (
		node_id TEXT PRIMARY KEY,
		ok INTEGER NOT NULL,
		fails INTEGER NOT NULL DEFAULT 0,
		checked_at INTEGER NOT NULL,
		ok_since INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT ''
	)`,
	// 健康巡检的跨唤醒状态 (DO 被驱逐后内存清空, 去重键与节拍时间必须落盘)
	`CREATE TABLE IF NOT EXISTS kv (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
];

export function migrate(sql: SqlStorage): void {
	for (const stmt of STATEMENTS) sql.exec(stmt);
}
