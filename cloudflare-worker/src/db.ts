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
	node_id: string;
	request_count: number;
	error_count: number;
	bytes_transferred: number;
	status_2xx: number;
	status_3xx: number;
	status_4xx: number;
	status_5xx: number;
	cache_hits: number;
	cache_misses: number;
	bytes_saved: number;
	total_latency_ns: number;
	last_access_time?: number;
	updated_at: number;
}

/**
 * 累计统计表的读取范围
 *
 * nodeId 与 excludeNode 互斥:
 *   - nodeId: 返回该节点的原始行, 不聚合 (节点启动时恢复自己的内存计数)
 *   - excludeNode: 聚合除该节点外的所有节点 (调用方再叠加自己的实时值)
 *   - 都不给: 聚合全部节点
 *
 * 节点集合由表里实际出现的 node_id 决定, 新节点上报后自动进入聚合, 无需注册。
 */
export interface MetricsScope {
	nodeId?: string;
	excludeNode?: string;
}

export async function getPathStats(
	db: D1Database,
	path?: string,
	scope: MetricsScope = {}
): Promise<PathStat[]> {
	const where: string[] = [];
	const params: any[] = [];

	if (path) {
		where.push('path = ?');
		params.push(path);
	}
	if (scope.nodeId) {
		where.push('node_id = ?');
		params.push(scope.nodeId);
	}
	if (scope.excludeNode) {
		where.push('node_id != ?');
		params.push(scope.excludeNode);
	}
	const whereSQL = where.length > 0 ? ` WHERE ${where.join(' AND ')}` : '';

	// 指定 nodeId 时取原始行; 其余情况按 path 跨节点求和。
	// last_access_time 取 MAX (最近一次被访问), updated_at 取 MAX (最近一次上报);
	// 派生值 (命中率 / 平均延迟) 不在这里算, 由读侧用 total_latency_ns 与 request_count 还原。
	const query = scope.nodeId
		? `SELECT * FROM path_stats${whereSQL} ORDER BY request_count DESC`
		: `SELECT
			path,
			'' AS node_id,
			SUM(request_count) AS request_count,
			SUM(error_count) AS error_count,
			SUM(bytes_transferred) AS bytes_transferred,
			SUM(status_2xx) AS status_2xx,
			SUM(status_3xx) AS status_3xx,
			SUM(status_4xx) AS status_4xx,
			SUM(status_5xx) AS status_5xx,
			SUM(cache_hits) AS cache_hits,
			SUM(cache_misses) AS cache_misses,
			SUM(bytes_saved) AS bytes_saved,
			SUM(total_latency_ns) AS total_latency_ns,
			MAX(last_access_time) AS last_access_time,
			MAX(updated_at) AS updated_at
		   FROM path_stats${whereSQL}
		   GROUP BY path
		   ORDER BY request_count DESC`;

	const result = await db.prepare(query).bind(...params).all<PathStat>();
	return result.results || [];
}

/**
 * 按 (path, node_id) 覆盖写本节点的累计快照
 *
 * 覆盖语义是刻意的: 上报的是节点自身的绝对累计值, 重复上报同一份不会双计。
 * 跨节点求和交给读侧的 GROUP BY, 不在写侧做累加。
 */
export async function batchUpsertPathStats(db: D1Database, stats: PathStat[]): Promise<void> {
	if (stats.length === 0) return;

	const batch = stats.map((stat) =>
		db.prepare(
			`INSERT INTO path_stats (
        path, node_id, request_count, error_count, bytes_transferred,
        status_2xx, status_3xx, status_4xx, status_5xx,
        cache_hits, cache_misses, bytes_saved,
        total_latency_ns, last_access_time, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15)
      ON CONFLICT(path, node_id) DO UPDATE SET
        request_count = excluded.request_count,
        error_count = excluded.error_count,
        bytes_transferred = excluded.bytes_transferred,
        status_2xx = excluded.status_2xx,
        status_3xx = excluded.status_3xx,
        status_4xx = excluded.status_4xx,
        status_5xx = excluded.status_5xx,
        cache_hits = excluded.cache_hits,
        cache_misses = excluded.cache_misses,
        bytes_saved = excluded.bytes_saved,
        total_latency_ns = excluded.total_latency_ns,
        last_access_time = excluded.last_access_time,
        updated_at = excluded.updated_at`
		).bind(
			stat.path,
			stat.node_id || 'default',
			stat.request_count,
			stat.error_count,
			stat.bytes_transferred,
			stat.status_2xx,
			stat.status_3xx,
			stat.status_4xx,
			stat.status_5xx,
			stat.cache_hits,
			stat.cache_misses,
			stat.bytes_saved,
			stat.total_latency_ns || 0,
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
	extra_config?: string; // JSON: 无专属列的 PathConfig 字段 (DefaultTargets / RefererBan / CFImageOpt / RedirectMode)
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
        path, default_target, enabled, extension_rules, cache_config, extra_config,
        created_at, updated_at
      ) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
      ON CONFLICT(path) DO UPDATE SET
        default_target = excluded.default_target,
        enabled = excluded.enabled,
        extension_rules = excluded.extension_rules,
        cache_config = excluded.cache_config,
        extra_config = excluded.extra_config,
        updated_at = excluded.updated_at`
		)
		.bind(
			map.path,
			map.default_target,
			map.enabled ? 1 : 0,
			map.extension_rules || null,
			map.cache_config || null,
			map.extra_config || null,
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
	node_id: string;
	count: number;
	updated_at: number;
}

export interface LatencyMetric {
	bucket: string;
	node_id: string;
	count: number;
	updated_at: number;
}

/**
 * 拼出"按 key 跨节点求和"或"取单节点原始行"的查询, 供两张单键计数表复用
 */
function buildCounterQuery(table: string, keyColumn: string, scope: MetricsScope): { sql: string; params: any[] } {
	const params: any[] = [];
	let whereSQL = '';
	if (scope.nodeId) {
		whereSQL = ' WHERE node_id = ?';
		params.push(scope.nodeId);
	} else if (scope.excludeNode) {
		whereSQL = ' WHERE node_id != ?';
		params.push(scope.excludeNode);
	}

	const sql = scope.nodeId
		? `SELECT * FROM ${table}${whereSQL} ORDER BY ${keyColumn}`
		: `SELECT ${keyColumn}, '' AS node_id, SUM(count) AS count, MAX(updated_at) AS updated_at
		   FROM ${table}${whereSQL} GROUP BY ${keyColumn} ORDER BY ${keyColumn}`;

	return { sql, params };
}

export async function getStatusCodes(db: D1Database, scope: MetricsScope = {}): Promise<StatusCodeMetric[]> {
	const { sql, params } = buildCounterQuery('status_codes', 'status_code', scope);
	const result = await db.prepare(sql).bind(...params).all<StatusCodeMetric>();
	return result.results || [];
}

export async function batchUpsertStatusCodes(db: D1Database, metrics: StatusCodeMetric[]): Promise<void> {
	if (metrics.length === 0) return;

	const batch = metrics.map((m) =>
		db.prepare(
			`INSERT INTO status_codes (status_code, node_id, count, updated_at)
       VALUES (?1, ?2, ?3, ?4)
       ON CONFLICT(status_code, node_id) DO UPDATE SET
         count = excluded.count,
         updated_at = excluded.updated_at`
		).bind(m.status_code, m.node_id || 'default', m.count, m.updated_at)
	);

	await db.batch(batch);
}

export async function getLatencyDistribution(db: D1Database, scope: MetricsScope = {}): Promise<LatencyMetric[]> {
	const { sql, params } = buildCounterQuery('latency_distribution', 'bucket', scope);
	const result = await db.prepare(sql).bind(...params).all<LatencyMetric>();
	return result.results || [];
}

export async function batchUpsertLatencyDistribution(db: D1Database, metrics: LatencyMetric[]): Promise<void> {
	if (metrics.length === 0) return;

	const batch = metrics.map((m) =>
		db.prepare(
			`INSERT INTO latency_distribution (bucket, node_id, count, updated_at)
       VALUES (?1, ?2, ?3, ?4)
       ON CONFLICT(bucket, node_id) DO UPDATE SET
         count = excluded.count,
         updated_at = excluded.updated_at`
		).bind(m.bucket, m.node_id || 'default', m.count, m.updated_at)
	);

	await db.batch(batch);
}

// ============================================
// Path Time Series 操作
// ============================================

// PathTimeseriesPoint 单节点单小时的桶记录
export interface PathTimeseriesPoint {
	path: string;
	ts_hour: number;
	node_id?: string;
	requests: number;
	bytes: number;
	errors: number;
	updated_at: number;
}

// AggregatedHourPoint 聚合后的单小时记录 (跨 node_id 求和)
export interface AggregatedHourPoint {
	path: string;
	ts_hour: number;
	requests: number;
	bytes: number;
	errors: number;
}

// batchUpsertPathTimeseries 批量写入本节点上报的桶, 同 (path, ts_hour, node_id) 主键覆盖
export async function batchUpsertPathTimeseries(
	db: D1Database,
	points: PathTimeseriesPoint[]
): Promise<void> {
	if (points.length === 0) return;

	// 用 MAX 而非直接覆盖, 避免节点重启后内存桶清零导致上报值小于已存值, 反而把历史值踩回去
	const batch = points.map((p) =>
		db.prepare(
			`INSERT INTO path_timeseries (path, ts_hour, node_id, requests, bytes, errors, updated_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
       ON CONFLICT(path, ts_hour, node_id) DO UPDATE SET
         requests = MAX(excluded.requests, requests),
         bytes = MAX(excluded.bytes, bytes),
         errors = MAX(excluded.errors, errors),
         updated_at = excluded.updated_at`
		).bind(
			p.path,
			p.ts_hour,
			p.node_id || 'default',
			p.requests,
			p.bytes,
			p.errors,
			p.updated_at
		)
	);

	await db.batch(batch);
}

// getAggregatedTimeseries 读取 [minHour, maxHour] 范围内全部路径的聚合数据 (跨节点求和)
export async function getAggregatedTimeseries(
	db: D1Database,
	minHour: number,
	maxHour: number
): Promise<AggregatedHourPoint[]> {
	const result = await db
		.prepare(
			`SELECT path, ts_hour,
              SUM(requests) AS requests,
              SUM(bytes) AS bytes,
              SUM(errors) AS errors
         FROM path_timeseries
        WHERE ts_hour >= ?1 AND ts_hour <= ?2
        GROUP BY path, ts_hour
        ORDER BY path, ts_hour`
		)
		.bind(minHour, maxHour)
		.all<AggregatedHourPoint>();
	return result.results || [];
}

// pruneOldTimeseries 删除 ts_hour < cutoff 的旧数据, 防止表无限增长
export async function pruneOldTimeseries(db: D1Database, cutoffHour: number): Promise<number> {
	const result = await db
		.prepare(`DELETE FROM path_timeseries WHERE ts_hour < ?1`)
		.bind(cutoffHour)
		.run();
	return result.meta?.changes || 0;
}

// ============================================
// Referer 天级时间序列 (referer_daily 表)
// ============================================

// RefererDailyPoint 单节点单天单 host 的桶记录
export interface RefererDailyPoint {
	host: string;
	ts_date: number;
	node_id?: string;
	requests: number;
	bytes: number;
	errors: number;
	updated_at: number;
}

// AggregatedRefererDayPoint 聚合后的单天单 host 记录 (跨 node_id 求和)
export interface AggregatedRefererDayPoint {
	host: string;
	ts_date: number;
	requests: number;
	bytes: number;
	errors: number;
}

// batchUpsertRefererDaily 批量写入本节点上报的天桶
// 使用 MAX 语义, 与 path_timeseries 一致, 避免节点重启清零后踩回历史值
export async function batchUpsertRefererDaily(
	db: D1Database,
	points: RefererDailyPoint[]
): Promise<void> {
	if (points.length === 0) return;

	const batch = points.map((p) =>
		db.prepare(
			`INSERT INTO referer_daily (host, ts_date, node_id, requests, bytes, errors, updated_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
       ON CONFLICT(host, ts_date, node_id) DO UPDATE SET
         requests = MAX(excluded.requests, requests),
         bytes = MAX(excluded.bytes, bytes),
         errors = MAX(excluded.errors, errors),
         updated_at = excluded.updated_at`
		).bind(
			p.host,
			p.ts_date,
			p.node_id || 'default',
			p.requests,
			p.bytes,
			p.errors,
			p.updated_at
		)
	);

	await db.batch(batch);
}

// getAggregatedRefererDaily 读取 [minDate, maxDate] 范围内的聚合天桶 (跨节点求和)
export async function getAggregatedRefererDaily(
	db: D1Database,
	minDate: number,
	maxDate: number
): Promise<AggregatedRefererDayPoint[]> {
	const result = await db
		.prepare(
			`SELECT host, ts_date,
              SUM(requests) AS requests,
              SUM(bytes) AS bytes,
              SUM(errors) AS errors
         FROM referer_daily
        WHERE ts_date >= ?1 AND ts_date <= ?2
        GROUP BY host, ts_date
        ORDER BY ts_date, host`
		)
		.bind(minDate, maxDate)
		.all<AggregatedRefererDayPoint>();
	return result.results || [];
}

// pruneOldRefererDaily 删除 ts_date < cutoff 的旧数据
export async function pruneOldRefererDaily(db: D1Database, cutoffDate: number): Promise<number> {
	const result = await db
		.prepare(`DELETE FROM referer_daily WHERE ts_date < ?1`)
		.bind(cutoffDate)
		.run();
	return result.meta?.changes || 0;
}
