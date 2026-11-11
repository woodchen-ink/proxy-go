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
	// 节点程序版本 (CI 每个 main 提交登记一次), assets = {arch: {url, sha256, size}}
	`CREATE TABLE IF NOT EXISTS releases (
		version TEXT PRIMARY KEY,
		build_time INTEGER NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		assets TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	// 滚动发布: kind = binary (容器内换程序) | container (Dokploy 重新部署); steps 为逐节点进度
	`CREATE TABLE IF NOT EXISTS rollouts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		version TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		steps TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT '',
		message TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
	// Dokploy 实例 (一个实例可管多个节点), API Key 以 CRED_KEY 加密
	`CREATE TABLE IF NOT EXISTS dokploy_servers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		secret TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`,
];

// 追加列: 只增不改, 已存在则跳过
const COLUMNS: [table: string, column: string, ddl: string][] = [
	['nodes', 'hello_at', 'INTEGER NOT NULL DEFAULT 0'], // 最近一次 hello (重连) 时间, 判断容器是否已重建
	['nodes', 'dokploy_server_id', 'INTEGER NOT NULL DEFAULT 0'],
	['nodes', 'dokploy_app_id', "TEXT NOT NULL DEFAULT ''"],
	['nodes', 'dokploy_app_type', "TEXT NOT NULL DEFAULT 'application'"], // application | compose
];

function addColumns(sql: SqlStorage): void {
	for (const [table, column, ddl] of COLUMNS) {
		const exists = sql.exec(`SELECT 1 FROM pragma_table_info('${table}') WHERE name = ?`, column).toArray().length > 0;
		if (!exists) sql.exec(`ALTER TABLE ${table} ADD COLUMN ${column} ${ddl}`);
	}
}

export function migrate(sql: SqlStorage): void {
	for (const stmt of STATEMENTS) sql.exec(stmt);
	rebuildBindings(sql);
	addColumns(sql);
}

// rebuildBindings 把旧表重建为 v3。SQLite 不能改约束, 只能重建; 按建表语句判断版本:
// v1 hostname 列级 UNIQUE, 无 kind 列; v2 增加 kind, 唯一键 (hostname, kind);
// v3 唯一键 (hostname, kind, record_line), DNSPod 纯 IP 绑定可以按线路各一条 (境内 / 境外)
function rebuildBindings(sql: SqlStorage): void {
	const row = sql.exec("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'dns_bindings'").toArray()[0];
	if (!row) return;
	const ddl = String(row.sql);
	const v1 = ddl.includes('hostname TEXT NOT NULL UNIQUE');
	if (!v1 && !ddl.includes('UNIQUE (hostname, kind)')) return;
	sql.exec('DROP TABLE IF EXISTS dns_bindings_v3');
	sql.exec(`CREATE TABLE dns_bindings_v3 (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL DEFAULT 'cf_dnspod',
		hostname TEXT NOT NULL,
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
		updated_at INTEGER NOT NULL,
		UNIQUE (hostname, kind, record_line)
	)`);
	sql.exec(`INSERT INTO dns_bindings_v3 (id, kind, hostname, enabled, cf_credential_id, saas_zone_id, saas_zone_name,
		dnspod_credential_id, dnspod_domain, record_name, record_line, pool, active_node, auto_failover, auto_failback,
		last_switch_at, last_check, note, updated_at)
		SELECT id, ${v1 ? "'cf_dnspod'" : 'kind'}, hostname, enabled, cf_credential_id, saas_zone_id, saas_zone_name,
		dnspod_credential_id, dnspod_domain, record_name, record_line, pool, active_node, auto_failover, auto_failback,
		last_switch_at, last_check, note, updated_at FROM dns_bindings`);
	sql.exec('DROP TABLE dns_bindings');
	sql.exec('ALTER TABLE dns_bindings_v3 RENAME TO dns_bindings');
}
