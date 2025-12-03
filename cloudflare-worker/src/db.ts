/**
 * 数据库操作辅助函数
 */

export interface Env {
	DB: D1Database;
	API_TOKEN?: string;
}

// ============================================
// Path Stats 操作
// ============================================

export interface PathStat {
	path: string;
	request_count: number;
	error_count: number;
	bytes_transferred: number;
	status_2xx: number;
	status_3xx: number;
	status_4xx: number;
	status_5xx: number;
	cache_hits: number;
	cache_misses: number;
	cache_hit_rate: number;
	bytes_saved: number;
	avg_latency?: string;
	last_access_time?: number;
	updated_at: number;
}

export async function getPathStats(db: D1Database, path?: string): Promise<PathStat[]> {
	let query = 'SELECT * FROM path_stats';
	const params: any[] = [];

	if (path) {
		query += ' WHERE path = ?';
		params.push(path);
	}

	query += ' ORDER BY request_count DESC';

	const result = await db.prepare(query).bind(...params).all<PathStat>();
	return result.results || [];
}

export async function upsertPathStat(db: D1Database, stat: PathStat): Promise<void> {
	await db
		.prepare(
			`INSERT INTO path_stats (
        path, request_count, error_count, bytes_transferred,
        status_2xx, status_3xx, status_4xx, status_5xx,
        cache_hits, cache_misses, cache_hit_rate, bytes_saved,
        avg_latency, last_access_time, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15)
      ON CONFLICT(path) DO UPDATE SET
        request_count = excluded.request_count,
        error_count = excluded.error_count,
        bytes_transferred = excluded.bytes_transferred,
        status_2xx = excluded.status_2xx,
        status_3xx = excluded.status_3xx,
        status_4xx = excluded.status_4xx,
        status_5xx = excluded.status_5xx,
        cache_hits = excluded.cache_hits,
        cache_misses = excluded.cache_misses,
        cache_hit_rate = excluded.cache_hit_rate,
        bytes_saved = excluded.bytes_saved,
        avg_latency = excluded.avg_latency,
        last_access_time = excluded.last_access_time,
        updated_at = excluded.updated_at`
		)
		.bind(
			stat.path,
			stat.request_count,
			stat.error_count,
			stat.bytes_transferred,
			stat.status_2xx,
			stat.status_3xx,
			stat.status_4xx,
			stat.status_5xx,
			stat.cache_hits,
			stat.cache_misses,
			stat.cache_hit_rate,
			stat.bytes_saved,
			stat.avg_latency || null,
			stat.last_access_time || null,
			stat.updated_at
		)
		.run();
}

export async function batchUpsertPathStats(db: D1Database, stats: PathStat[]): Promise<void> {
	const batch = stats.map((stat) =>
		db.prepare(
			`INSERT INTO path_stats (
        path, request_count, error_count, bytes_transferred,
        status_2xx, status_3xx, status_4xx, status_5xx,
        cache_hits, cache_misses, cache_hit_rate, bytes_saved,
        avg_latency, last_access_time, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15)
      ON CONFLICT(path) DO UPDATE SET
        request_count = excluded.request_count,
        error_count = excluded.error_count,
        bytes_transferred = excluded.bytes_transferred,
        status_2xx = excluded.status_2xx,
        status_3xx = excluded.status_3xx,
        status_4xx = excluded.status_4xx,
        status_5xx = excluded.status_5xx,
        cache_hits = excluded.cache_hits,
        cache_misses = excluded.cache_misses,
        cache_hit_rate = excluded.cache_hit_rate,
        bytes_saved = excluded.bytes_saved,
        avg_latency = excluded.avg_latency,
        last_access_time = excluded.last_access_time,
        updated_at = excluded.updated_at`
		).bind(
			stat.path,
			stat.request_count,
			stat.error_count,
			stat.bytes_transferred,
			stat.status_2xx,
			stat.status_3xx,
			stat.status_4xx,
			stat.status_5xx,
			stat.cache_hits,
			stat.cache_misses,
			stat.cache_hit_rate,
			stat.bytes_saved,
			stat.avg_latency || null,
			stat.last_access_time || null,
			stat.updated_at
		)
	);

	await db.batch(batch);
}

// ============================================
// Banned IPs 操作
// ============================================

export interface BannedIP {
	ip: string;
	ban_time: number;
	ban_end_time: number;
	reason?: string;
	error_count: number;
	is_active: boolean;
	unban_time?: number;
	unban_reason?: string;
	updated_at: number;
}

export interface BannedIPHistory {
	id?: number;
	ip: string;
	ban_time: number;
	ban_end_time: number;
	reason?: string;
	error_count: number;
	unban_time?: number;
	unban_reason?: string;
	created_at: number;
}

export async function getBannedIPs(db: D1Database, activeOnly: boolean = false): Promise<BannedIP[]> {
	let query = 'SELECT * FROM banned_ips';

	if (activeOnly) {
		query += ' WHERE is_active = 1';
	}

	query += ' ORDER BY ban_time DESC';

	const result = await db.prepare(query).all<BannedIP>();
	return result.results || [];
}

export async function upsertBannedIP(db: D1Database, ban: BannedIP): Promise<void> {
	await db
		.prepare(
			`INSERT INTO banned_ips (
        ip, ban_time, ban_end_time, reason, error_count,
        is_active, unban_time, unban_reason, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
      ON CONFLICT(ip) DO UPDATE SET
        ban_time = excluded.ban_time,
        ban_end_time = excluded.ban_end_time,
        reason = excluded.reason,
        error_count = excluded.error_count,
        is_active = excluded.is_active,
        unban_time = excluded.unban_time,
        unban_reason = excluded.unban_reason,
        updated_at = excluded.updated_at`
		)
		.bind(
			ban.ip,
			ban.ban_time,
			ban.ban_end_time,
			ban.reason || null,
			ban.error_count,
			ban.is_active ? 1 : 0,
			ban.unban_time || null,
			ban.unban_reason || null,
			ban.updated_at
		)
		.run();
}

export async function batchUpsertBannedIPs(db: D1Database, bans: BannedIP[]): Promise<void> {
	const batch = bans.map((ban) =>
		db.prepare(
			`INSERT INTO banned_ips (
        ip, ban_time, ban_end_time, reason, error_count,
        is_active, unban_time, unban_reason, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
      ON CONFLICT(ip) DO UPDATE SET
        ban_time = excluded.ban_time,
        ban_end_time = excluded.ban_end_time,
        reason = excluded.reason,
        error_count = excluded.error_count,
        is_active = excluded.is_active,
        unban_time = excluded.unban_time,
        unban_reason = excluded.unban_reason,
        updated_at = excluded.updated_at`
		).bind(
			ban.ip,
			ban.ban_time,
			ban.ban_end_time,
			ban.reason || null,
			ban.error_count,
			ban.is_active ? 1 : 0,
			ban.unban_time || null,
			ban.unban_reason || null,
			ban.updated_at
		)
	);

	await db.batch(batch);
}

export async function getBannedIPHistory(db: D1Database, ip?: string, limit: number = 100): Promise<BannedIPHistory[]> {
	let query = 'SELECT * FROM banned_ips_history';
	const params: any[] = [];

	if (ip) {
		query += ' WHERE ip = ?';
		params.push(ip);
	}

	query += ' ORDER BY created_at DESC LIMIT ?';
	params.push(limit);

	const result = await db.prepare(query).bind(...params).all<BannedIPHistory>();
	return result.results || [];
}

export async function addBannedIPHistory(db: D1Database, history: BannedIPHistory): Promise<void> {
	await db
		.prepare(
			`INSERT INTO banned_ips_history (
        ip, ban_time, ban_end_time, reason, error_count,
        unban_time, unban_reason, created_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`
		)
		.bind(
			history.ip,
			history.ban_time,
			history.ban_end_time,
			history.reason || null,
			history.error_count,
			history.unban_time || null,
			history.unban_reason || null,
			history.created_at
		)
		.run();
}

// ============================================
// Config Maps 操作
// ============================================

export interface ConfigMap {
	path: string;
	default_target: string;
	enabled: boolean;
	extension_rules?: string; // JSON
	cache_config?: string; // JSON
	created_at: number;
	updated_at: number;
}

export async function getConfigMaps(db: D1Database, enabledOnly: boolean = false): Promise<ConfigMap[]> {
	let query = 'SELECT * FROM config_maps';

	if (enabledOnly) {
		query += ' WHERE enabled = 1';
	}

	query += ' ORDER BY path';

	const result = await db.prepare(query).all<ConfigMap>();
	return result.results || [];
}

export async function getConfigMap(db: D1Database, path: string): Promise<ConfigMap | null> {
	const result = await db.prepare('SELECT * FROM config_maps WHERE path = ?').bind(path).first<ConfigMap>();
	return result || null;
}

export async function upsertConfigMap(db: D1Database, map: ConfigMap): Promise<void> {
	const now = Date.now();
	await db
		.prepare(
			`INSERT INTO config_maps (
        path, default_target, enabled, extension_rules, cache_config,
        created_at, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
      ON CONFLICT(path) DO UPDATE SET
        default_target = excluded.default_target,
        enabled = excluded.enabled,
        extension_rules = excluded.extension_rules,
        cache_config = excluded.cache_config,
        updated_at = excluded.updated_at`
		)
		.bind(
			map.path,
			map.default_target,
			map.enabled ? 1 : 0,
			map.extension_rules || null,
			map.cache_config || null,
			map.created_at || now,
			map.updated_at || now
		)
		.run();
}

export async function deleteConfigMap(db: D1Database, path: string): Promise<void> {
	await db.prepare('DELETE FROM config_maps WHERE path = ?').bind(path).run();
}

// ============================================
// Config Other 操作
// ============================================

export interface ConfigOther {
	key: string;
	value: string; // JSON
	description?: string;
	updated_at: number;
}

export async function getConfigOther(db: D1Database, key?: string): Promise<ConfigOther[]> {
	let query = 'SELECT * FROM config_other';
	const params: any[] = [];

	if (key) {
		query += ' WHERE key = ?';
		params.push(key);
	}

	const result = await db.prepare(query).bind(...params).all<ConfigOther>();
	return result.results || [];
}

export async function upsertConfigOther(db: D1Database, config: ConfigOther): Promise<void> {
	await db
		.prepare(
			`INSERT INTO config_other (key, value, description, updated_at)
       VALUES (?1, ?2, ?3, ?4)
       ON CONFLICT(key) DO UPDATE SET
         value = excluded.value,
         description = excluded.description,
         updated_at = excluded.updated_at`
		)
		.bind(config.key, config.value, config.description || null, config.updated_at)
		.run();
}

// ============================================
// Metrics 操作 (status_codes, latency_distribution)
// ============================================

export interface StatusCodeMetric {
	status_code: string;
	count: number;
	updated_at: number;
}

export interface LatencyMetric {
	bucket: string;
	count: number;
	updated_at: number;
}

export async function getStatusCodes(db: D1Database): Promise<StatusCodeMetric[]> {
	const result = await db.prepare('SELECT * FROM status_codes ORDER BY status_code').all<StatusCodeMetric>();
	return result.results || [];
}

export async function batchUpsertStatusCodes(db: D1Database, metrics: StatusCodeMetric[]): Promise<void> {
	if (metrics.length === 0) return;

	const batch = metrics.map((m) =>
		db.prepare(
			`INSERT INTO status_codes (status_code, count, updated_at)
       VALUES (?1, ?2, ?3)
       ON CONFLICT(status_code) DO UPDATE SET
         count = excluded.count,
         updated_at = excluded.updated_at`
		).bind(m.status_code, m.count, m.updated_at)
	);

	await db.batch(batch);
}

export async function getLatencyDistribution(db: D1Database): Promise<LatencyMetric[]> {
	const result = await db.prepare('SELECT * FROM latency_distribution ORDER BY bucket').all<LatencyMetric>();
	return result.results || [];
}

export async function batchUpsertLatencyDistribution(db: D1Database, metrics: LatencyMetric[]): Promise<void> {
	if (metrics.length === 0) return;

	const batch = metrics.map((m) =>
		db.prepare(
			`INSERT INTO latency_distribution (bucket, count, updated_at)
       VALUES (?1, ?2, ?3)
       ON CONFLICT(bucket) DO UPDATE SET
         count = excluded.count,
         updated_at = excluded.updated_at`
		).bind(m.bucket, m.count, m.updated_at)
	);

	await db.batch(batch);
}
