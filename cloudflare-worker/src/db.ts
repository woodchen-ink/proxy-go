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
 * nodeId / excludeNode / byNode 互斥:
 *   - nodeId: 返回该节点的原始行, 不聚合 (节点启动时恢复自己的内存计数)
 *   - excludeNode: 聚合除该节点外的所有节点 (调用方再叠加自己的实时值)
 *   - byNode: 返回全部节点的原始行, 不聚合 (仪表盘展示各节点明细)
 *   - 都不给: 聚合全部节点
 *
 * 节点集合由表里实际出现的 node_id 决定, 新节点上报后自动进入聚合, 无需注册。
 */
export interface MetricsScope {
	nodeId?: string;
	excludeNode?: string;
	byNode?: boolean;
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

	// 取原始行 (不聚合) 的两种情形: 指定单节点, 或要各节点明细
	// last_access_time 取 MAX (最近一次被访问), updated_at 取 MAX (最近一次上报);
	// 派生值 (命中率 / 平均延迟) 不在这里算, 由读侧用 total_latency_ns 与 request_count 还原。
	const query = scope.nodeId || scope.byNode
		? `SELECT * FROM path_stats${whereSQL} ORDER BY node_id, request_count DESC`
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
 *
 * 各统计表的 UPSERT 都带 WHERE: 值没变的行不更新, 不计 D1 写入行数。节点每 5 分钟
 * 全量上报 (含近 6 小时时间序列窗口), 大部分行两次之间不变, 不跳过会让免费额度
 * (10 万行/天) 每天超标。代价是 updated_at 只在值变化时刷新, 读侧把它当"最后变化时间"。
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
        updated_at = excluded.updated_at
      WHERE request_count IS NOT excluded.request_count
        OR error_count IS NOT excluded.error_count
        OR bytes_transferred IS NOT excluded.bytes_transferred
        OR status_2xx IS NOT excluded.status_2xx
        OR status_3xx IS NOT excluded.status_3xx
        OR status_4xx IS NOT excluded.status_4xx
        OR status_5xx IS NOT excluded.status_5xx
        OR cache_hits IS NOT excluded.cache_hits
        OR cache_misses IS NOT excluded.cache_misses
        OR bytes_saved IS NOT excluded.bytes_saved
        OR total_latency_ns IS NOT excluded.total_latency_ns
        OR last_access_time IS NOT excluded.last_access_time`
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
         updated_at = excluded.updated_at
       WHERE count IS NOT excluded.count`
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
         updated_at = excluded.updated_at
       WHERE count IS NOT excluded.count`
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
         updated_at = excluded.updated_at
       WHERE excluded.requests > requests OR excluded.bytes > bytes OR excluded.errors > errors`
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

// RefererPathHits 某 host 当天被引用的一条路径 (p = 路径, n = 次数)
export interface RefererPathHits {
	p: string;
	n: number;
}

// RefererDailyPoint 单节点单天单 host 的桶记录; blocked 之后的字段由迁移 0009 引入, 旧节点不带
export interface RefererDailyPoint {
	host: string;
	ts_date: number;
	node_id?: string;
	requests: number;
	bytes: number;
	errors: number;
	updated_at: number;
	blocked?: number;
	redirected?: number;
	distinct_paths?: number;
	top_paths?: RefererPathHits[] | null;
}

// AggregatedRefererDayPoint 聚合后的单天单 host 记录 (跨 node_id 求和); paths 为各节点 top_paths JSON, 以 \x1e 分隔
export interface AggregatedRefererDayPoint {
	host: string;
	ts_date: number;
	requests: number;
	bytes: number;
	errors: number;
	blocked: number;
	redirected: number;
	distinct_paths: number;
	paths: string | null;
}

// batchUpsertRefererDaily 批量写入本节点上报的天桶
// 使用 MAX 语义, 与 path_timeseries 一致, 避免节点重启清零后踩回历史值;
// WHERE 只在计数变大时才写, 未变化的行不计 D1 写入。
// 带 top_paths 的上报来自已跑过迁移 0009 的新节点, 才写新列; 旧节点走原语句
export async function batchUpsertRefererDaily(db: D1Database, points: RefererDailyPoint[]): Promise<void> {
	if (points.length === 0) return;
	const detailed = points.some((p) => p.top_paths !== undefined);
	try {
		await upsertRefererDaily(db, points, detailed);
	} catch (e) {
		// 节点启动迁移失败时新列不存在: 退回旧语句, 不能让整个心跳 (含配置 / 重置下行) 失败
		if (!detailed) throw e;
		console.error('referer_daily detailed upsert failed, fallback:', e);
		await upsertRefererDaily(db, points, false);
	}
}

async function upsertRefererDaily(db: D1Database, points: RefererDailyPoint[], detailed: boolean): Promise<void> {
	const batch = points.map((p) =>
		detailed
			? db
					.prepare(
						`INSERT INTO referer_daily (host, ts_date, node_id, requests, bytes, errors, blocked, redirected, distinct_paths, top_paths, updated_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)
       ON CONFLICT(host, ts_date, node_id) DO UPDATE SET
         requests = MAX(excluded.requests, requests),
         bytes = MAX(excluded.bytes, bytes),
         errors = MAX(excluded.errors, errors),
         blocked = MAX(excluded.blocked, blocked),
         redirected = MAX(excluded.redirected, redirected),
         distinct_paths = MAX(excluded.distinct_paths, distinct_paths),
         top_paths = CASE WHEN excluded.requests >= requests AND excluded.top_paths IS NOT NULL THEN excluded.top_paths ELSE top_paths END,
         updated_at = excluded.updated_at
       WHERE excluded.requests > requests OR excluded.bytes > bytes OR excluded.errors > errors
          OR excluded.blocked > blocked OR excluded.redirected > redirected`
					)
					.bind(
						p.host,
						p.ts_date,
						p.node_id || 'default',
						p.requests,
						p.bytes,
						p.errors,
						p.blocked ?? 0,
						p.redirected ?? 0,
						p.distinct_paths ?? 0,
						p.top_paths?.length ? JSON.stringify(p.top_paths) : null,
						p.updated_at
					)
			: db
					.prepare(
						`INSERT INTO referer_daily (host, ts_date, node_id, requests, bytes, errors, updated_at)
       VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
       ON CONFLICT(host, ts_date, node_id) DO UPDATE SET
         requests = MAX(excluded.requests, requests),
         bytes = MAX(excluded.bytes, bytes),
         errors = MAX(excluded.errors, errors),
         updated_at = excluded.updated_at
       WHERE excluded.requests > requests OR excluded.bytes > bytes OR excluded.errors > errors`
					)
					.bind(p.host, p.ts_date, p.node_id || 'default', p.requests, p.bytes, p.errors, p.updated_at)
	);
	await db.batch(batch);
}

// getAggregatedRefererDaily 读取 [minDate, maxDate] 范围内的聚合天桶 (跨节点求和)
// 迁移 0009 之前的库没有新列, 退回只读基础列
export async function getAggregatedRefererDaily(
	db: D1Database,
	minDate: number,
	maxDate: number
): Promise<AggregatedRefererDayPoint[]> {
	try {
		const result = await db
			.prepare(
				`SELECT host, ts_date,
              SUM(requests) AS requests, SUM(bytes) AS bytes, SUM(errors) AS errors,
              SUM(blocked) AS blocked, SUM(redirected) AS redirected,
              MAX(distinct_paths) AS distinct_paths,
              GROUP_CONCAT(top_paths, char(30)) AS paths
         FROM referer_daily
        WHERE ts_date >= ?1 AND ts_date <= ?2
        GROUP BY host, ts_date`
			)
			.bind(minDate, maxDate)
			.all<AggregatedRefererDayPoint>();
		return result.results || [];
	} catch {
		const result = await db
			.prepare(
				`SELECT host, ts_date, SUM(requests) AS requests, SUM(bytes) AS bytes, SUM(errors) AS errors,
              0 AS blocked, 0 AS redirected, 0 AS distinct_paths, NULL AS paths
         FROM referer_daily
        WHERE ts_date >= ?1 AND ts_date <= ?2
        GROUP BY host, ts_date`
			)
			.bind(minDate, maxDate)
			.all<AggregatedRefererDayPoint>();
		return result.results || [];
	}
}

// RefererHostArchive 长期汇总: 已滚出 referer_daily 的天按 host 累加
export interface RefererHostArchive {
	host: string;
	first_date: number;
	last_date: number;
	requests: number;
	bytes: number;
	errors: number;
	blocked: number;
	redirected: number;
	peak_date: number;
	peak_requests: number;
	peak_paths: string | null;
}

const ARCHIVE_COLS = 'host, first_date, last_date, requests, bytes, errors, blocked, redirected, peak_date, peak_requests, peak_paths';

// getRefererHostArchive 读长期汇总: 指定 host (按主键) + 最近出现的 limit 个 (走 last_date 索引), 不全表扫;
// 表还没建 (节点未升级) 时返回空
export async function getRefererHostArchive(db: D1Database, hosts: string[], limit: number): Promise<RefererHostArchive[]> {
	try {
		const [byHost, recent] = await db.batch<RefererHostArchive>([
			db.prepare(`SELECT ${ARCHIVE_COLS} FROM referer_hosts WHERE host IN (SELECT value FROM json_each(?1))`).bind(JSON.stringify(hosts)),
			db.prepare(`SELECT ${ARCHIVE_COLS} FROM referer_hosts ORDER BY last_date DESC LIMIT ?1`).bind(limit),
		]);
		const out = new Map<string, RefererHostArchive>();
		for (const r of [...(byHost.results ?? []), ...(recent.results ?? [])]) out.set(r.host, r);
		return [...out.values()];
	} catch {
		return [];
	}
}

// pruneOldRefererDaily 把 ts_date < cutoff 的天累加进 referer_hosts 后删除, 同一 batch (事务) 内完成,
// 多个节点同时触发也只会有一个事务看到这些行, 不会重复累加。
// 汇总表还没建 (迁移 0009 未跑) 时不删, 留到下次, 免得 host 历史丢失
export async function pruneOldRefererDaily(db: D1Database, cutoffDate: number): Promise<number> {
	const now = Date.now();
	const rollup = db
		.prepare(
			`INSERT INTO referer_hosts (host, first_date, last_date, requests, bytes, errors, blocked, redirected, peak_date, peak_requests, peak_paths, updated_at)
       SELECT d.host, d.first_date, d.last_date, d.requests, d.bytes, d.errors, d.blocked, d.redirected,
              p.ts_date, p.requests, p.paths, ?2
         FROM (SELECT host, MIN(ts_date) AS first_date, MAX(ts_date) AS last_date,
                      SUM(requests) AS requests, SUM(bytes) AS bytes, SUM(errors) AS errors,
                      SUM(blocked) AS blocked, SUM(redirected) AS redirected
                 FROM referer_daily WHERE ts_date < ?1 GROUP BY host) d
         JOIN (SELECT host, ts_date, SUM(requests) AS requests, GROUP_CONCAT(top_paths, char(30)) AS paths,
                      ROW_NUMBER() OVER (PARTITION BY host ORDER BY SUM(requests) DESC, ts_date DESC) AS rn
                 FROM referer_daily WHERE ts_date < ?1 GROUP BY host, ts_date) p
           ON p.host = d.host AND p.rn = 1
        WHERE true
       ON CONFLICT(host) DO UPDATE SET
         first_date = MIN(first_date, excluded.first_date),
         last_date = MAX(last_date, excluded.last_date),
         requests = requests + excluded.requests,
         bytes = bytes + excluded.bytes,
         errors = errors + excluded.errors,
         blocked = blocked + excluded.blocked,
         redirected = redirected + excluded.redirected,
         peak_date = CASE WHEN excluded.peak_requests > peak_requests THEN excluded.peak_date ELSE peak_date END,
         peak_paths = CASE WHEN excluded.peak_requests > peak_requests AND excluded.peak_paths IS NOT NULL THEN excluded.peak_paths ELSE peak_paths END,
         peak_requests = MAX(peak_requests, excluded.peak_requests),
         updated_at = excluded.updated_at`
		)
		.bind(cutoffDate, now);
	const del = db.prepare(`DELETE FROM referer_daily WHERE ts_date < ?1`).bind(cutoffDate);
	try {
		const res = await db.batch([rollup, del]);
		return res[1].meta?.changes || 0;
	} catch (e) {
		console.error('referer rollup failed, keep rows:', e);
		return 0;
	}
}
