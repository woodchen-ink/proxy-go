/**
 * 节点 REST 协议 (旧通道, Bearer API_TOKEN)
 *
 * 节点的 D1 读写 (指标上报 / 心跳下行 / 配置拉取 / 通用 SQL 迁移) 仍走这里;
 * 实时指令与快照改走 WebSocket (见 hub/), 两条通道并存, 老版本节点不受影响。
 * 鉴权由 index.ts 在分发前完成。
 */

import * as db from './db';
import { checkDrop, checkQuery, checkWrite } from './sql_guard';
import { batchUpsertNodeTimeseries, pruneNodeTimeseries, type NodeTimeseriesUpload } from './node_timeseries';
import type { Env } from './env';

export async function handleLegacy(request: Request, env: Env): Promise<Response> {
	{
		const corsHeaders = {
			'Access-Control-Allow-Origin': '*',
			'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
			'Access-Control-Allow-Headers': 'Content-Type, Authorization',
		};

		if (request.method === 'OPTIONS') {
			return new Response(null, { headers: corsHeaders });
		}

		const url = new URL(request.url);
		const path = url.pathname;

		try {
			// ============================================
			// Path Stats API
			// ============================================
			if (path === '/path-stats' && request.method === 'GET') {
				const pathParam = url.searchParams.get('path');
				const stats = await db.getPathStats(env.DB, pathParam || undefined, readScope(url));
				return jsonResponse({ success: true, data: stats }, 200, corsHeaders);
			}

			// ============================================
			// Config Maps API
			// ============================================
			if (path === '/config-maps' && request.method === 'GET') {
				const enabledOnly = url.searchParams.get('enabled') === 'true';
				const maps = await db.getConfigMaps(env.DB, enabledOnly);
				return jsonResponse({ success: true, data: maps }, 200, corsHeaders);
			}

			if (path.startsWith('/config-maps/') && request.method === 'GET') {
				const mapPath = decodeURIComponent(path.substring('/config-maps/'.length));
				const map = await db.getConfigMap(env.DB, mapPath);
				if (!map) {
					return jsonResponse({ error: 'Config map not found' }, 404, corsHeaders);
				}
				return jsonResponse({ success: true, data: map }, 200, corsHeaders);
			}

			if (path === '/config-maps' && request.method === 'POST') {
				const body = await request.json<{ maps: db.ConfigMap[] }>();
				if (!body.maps || !Array.isArray(body.maps)) {
					return jsonResponse({ error: 'Invalid request: maps array required' }, 400, corsHeaders);
				}
				for (const map of body.maps) {
					await db.upsertConfigMap(env.DB, map);
				}
				return jsonResponse({ success: true, updated: body.maps.length }, 200, corsHeaders);
			}

			if (path.startsWith('/config-maps/') && request.method === 'DELETE') {
				const mapPath = decodeURIComponent(path.substring('/config-maps/'.length));
				await db.deleteConfigMap(env.DB, mapPath);
				return jsonResponse({ success: true, message: 'Config map deleted' }, 200, corsHeaders);
			}

			// ============================================
			// Config Other API
			// ============================================
			if (path === '/config-other' && request.method === 'GET') {
				const key = url.searchParams.get('key');
				const configs = await db.getConfigOther(env.DB, key || undefined);
				return jsonResponse({ success: true, data: configs }, 200, corsHeaders);
			}

			if (path === '/config-other' && request.method === 'POST') {
				const body = await request.json<{ configs: db.ConfigOther[] }>();
				if (!body.configs || !Array.isArray(body.configs)) {
					return jsonResponse({ error: 'Invalid request: configs array required' }, 400, corsHeaders);
				}
				for (const config of body.configs) {
					await db.upsertConfigOther(env.DB, config);
				}
				return jsonResponse({ success: true, updated: body.configs.length }, 200, corsHeaders);
			}

			// ============================================
			// 合并读写 endpoint
			//
			// 一次同步原本要打 5 个 POST, 一次仪表盘刷新要打 3 个 GET。
			// 每个请求都单独计费, 且各自一次网络往返 —— 合并成单请求后, 上报 5→1、
			// 读取 3→1, 请求数降到约五分之一, 也让上报间隔有余量往下调。
			// 各表的批量写仍走原来的 db.batchUpsert*, 语义完全不变。
			// ============================================
			if (path === '/metrics/bulk' && request.method === 'POST') {
				const body = await request.json<{
					path_stats?: db.PathStat[];
					status_codes?: db.StatusCodeMetric[];
					latency?: db.LatencyMetric[];
					timeseries?: db.PathTimeseriesPoint[];
					referer_daily?: db.RefererDailyPoint[];
					node_timeseries?: NodeTimeseriesUpload[];
					cursors?: {
						config_version?: number;
						reset_seen?: number;
					};
				}>();

				const written: Record<string, number> = {};
				// 逐段独立执行: 某一段为空就跳过, 不因为一段缺失而整体失败
				if (body.path_stats?.length) {
					await db.batchUpsertPathStats(env.DB, body.path_stats);
					written.path_stats = body.path_stats.length;
				}
				if (body.status_codes?.length) {
					await db.batchUpsertStatusCodes(env.DB, body.status_codes);
					written.status_codes = body.status_codes.length;
				}
				if (body.latency?.length) {
					await db.batchUpsertLatencyDistribution(env.DB, body.latency);
					written.latency = body.latency.length;
				}
				if (body.timeseries?.length) {
					await db.batchUpsertPathTimeseries(env.DB, body.timeseries);
					written.timeseries = body.timeseries.length;
				}
				if (body.referer_daily?.length) {
					await db.batchUpsertRefererDaily(env.DB, body.referer_daily);
					written.referer_daily = body.referer_daily.length;
				}
				// 新表由节点迁移 0008 建立; 写失败只记日志, 不拖垮其余统计的上报
				if (body.node_timeseries?.length) {
					try {
						await batchUpsertNodeTimeseries(env.DB, body.node_timeseries);
						written.node_timeseries = body.node_timeseries.length;
					} catch (e) {
						console.error('node_timeseries upsert failed:', e);
					}
				}

				// 心跳下行: 节点上行捎游标, 这里只回传比游标新的状态。
				// 每个槽位独立 try/catch —— 下行状态是增值服务, 任何一段失败
				// (表还没建好 / 查询异常) 都不能拖垮指标上报本身
				const state: Record<string, unknown> = {};
				const cursors = body.cursors;
				if (cursors && typeof cursors === 'object') {
					// 槽位 1: 配置版本 + 增量配置 (版本比游标新才带数据)
					try {
						const row = await env.DB.prepare(
							"SELECT value FROM config_other WHERE key = '__config_version'"
						).first<{ value: string }>();
						const version = row ? parseInt(row.value, 10) || 0 : 0;
						state.config_version = version;
						if (version > (cursors.config_version ?? 0)) {
							state.config_maps = await db.getConfigMaps(env.DB, false);
							state.config_other = await db.getConfigOther(env.DB);
						}
					} catch (e) {
						console.error('pulse config slot failed:', e);
					}

					// 槽位 2: 重置代际 marker
					try {
						if (typeof cursors.reset_seen === 'number') {
							const rows = await env.DB.prepare(
								'SELECT id, path, reset_at FROM stats_resets WHERE reset_at > ? ORDER BY reset_at ASC LIMIT 50'
							)
								.bind(cursors.reset_seen)
								.all();
							if (rows.results?.length) {
								state.resets = rows.results;
							}
						}
					} catch (e) {
						console.error('pulse resets slot failed:', e);
					}
				}

				return jsonResponse({ success: true, written, state }, 200, corsHeaders);
			}

			if (path === '/metrics/bulk' && request.method === 'GET') {
				const scope = readScope(url);
				const [pathStats, statusCodes, latency] = await Promise.all([
					db.getPathStats(env.DB, undefined, scope),
					db.getStatusCodes(env.DB, scope),
					db.getLatencyDistribution(env.DB, scope),
				]);
				return jsonResponse(
					{ success: true, data: { path_stats: pathStats, status_codes: statusCodes, latency } },
					200,
					corsHeaders
				);
			}

			// ============================================
			// 通用 SQL 执行 endpoint
			//
			// 由 Go 端持有所有 schema / migration 定义, worker 只做 D1 的薄壳代理:
			//   POST /admin/sql/batch   { statements: [{sql, params?}] }  -> 事务批量执行 (D1 batch 是事务的)
			//   POST /admin/sql/query   { sql, params? }                  -> 查询返回 rows
			//
			// 安全模型: 除 API_TOKEN 鉴权外, 语句按白名单校验 (sql_guard.ts), 只放行迁移与节点维护
			// 实际用到的形态; 每个节点都持有 API_TOKEN, 不能让它等同于 D1 的任意读写权限
			// ============================================
			if (path === '/admin/sql/batch' && request.method === 'POST') {
				const body = await request.json<{
					statements: Array<{ sql: string; params?: unknown[] }>;
				}>();
				if (!body.statements || !Array.isArray(body.statements) || body.statements.length === 0) {
					return jsonResponse(
						{ error: 'Invalid request: non-empty statements array required' },
						400,
						corsHeaders
					);
				}
				for (const s of body.statements) {
					let verdict = checkWrite(s?.sql);
					if (verdict.ok && verdict.dropTable) verdict = await checkDrop(env.DB, verdict.dropTable);
					if (!verdict.ok) {
						console.warn('admin/sql/batch rejected:', verdict.reason, String(s?.sql).slice(0, 200));
						return jsonResponse({ error: `SQL rejected: ${verdict.reason}` }, 403, corsHeaders);
					}
				}
				const prepared = body.statements.map((s) => {
					const stmt = env.DB.prepare(s.sql);
					return s.params && s.params.length > 0 ? stmt.bind(...s.params) : stmt;
				});
				const results = await env.DB.batch(prepared);
				return jsonResponse(
					{
						success: true,
						results: results.map((r) => ({
							changes: r.meta?.changes ?? 0,
							last_row_id: r.meta?.last_row_id ?? 0,
						})),
					},
					200,
					corsHeaders
				);
			}

			if (path === '/admin/sql/query' && request.method === 'POST') {
				const body = await request.json<{ sql: string; params?: unknown[] }>();
				if (!body.sql || typeof body.sql !== 'string') {
					return jsonResponse(
						{ error: 'Invalid request: sql string required' },
						400,
						corsHeaders
					);
				}
				const verdict = checkQuery(body.sql);
				if (!verdict.ok) {
					console.warn('admin/sql/query rejected:', verdict.reason, body.sql.slice(0, 200));
					return jsonResponse({ error: `SQL rejected: ${verdict.reason}` }, 403, corsHeaders);
				}
				const stmt = env.DB.prepare(body.sql);
				const bound = body.params && body.params.length > 0 ? stmt.bind(...body.params) : stmt;
				const result = await bound.all();
				return jsonResponse(
					{ success: true, rows: result.results ?? [] },
					200,
					corsHeaders
				);
			}

			// ============================================
			// Path Time Series API
			// ============================================
			if (path === '/metrics/path-timeseries' && request.method === 'DELETE') {
				const cutoff = parseInt(url.searchParams.get('cutoff_hour') || '0');
				if (!cutoff) {
					return jsonResponse(
						{ error: 'cutoff_hour required' },
						400,
						corsHeaders
					);
				}
				const deleted = (await db.pruneOldTimeseries(env.DB, cutoff)) + (await pruneNodeTimeseries(env.DB, cutoff));
				return jsonResponse({ success: true, deleted }, 200, corsHeaders);
			}

			// ============================================
			// Referer Daily API (host 天级时间序列)
			// ============================================
			if (path === '/metrics/referer-daily' && request.method === 'DELETE') {
				const cutoff = parseInt(url.searchParams.get('cutoff_date') || '0');
				if (!cutoff) {
					return jsonResponse(
						{ error: 'cutoff_date required' },
						400,
						corsHeaders
					);
				}
				const deleted = await db.pruneOldRefererDaily(env.DB, cutoff);
				return jsonResponse({ success: true, deleted }, 200, corsHeaders);
			}

			// ============================================
			// 根路径 - API 信息
			// ============================================
			if (path === '/' || path === '') {
				return jsonResponse(
					{
						name: 'proxy-go-sync API v2',
						version: '3.0.0',
						endpoints: {
							'POST /metrics/bulk': 'Node heartbeat: upload stats, receive config / reset state',
							'GET /metrics/bulk?node_id=': 'Node startup: read back own cumulative stats',
							'GET /path-stats': 'Path statistics',
							'GET|POST /config-maps, DELETE /config-maps/{path}': 'Path configs (seed only; hub is the writer)',
							'GET|POST /config-other': 'Other configs',
							'POST /admin/sql/batch|query': 'Node-owned schema migrations',
							'DELETE /metrics/path-timeseries?cutoff_hour': 'Prune hourly buckets',
							'DELETE /metrics/referer-daily?cutoff_date': 'Prune referer daily buckets',
						},
					},
					200,
					corsHeaders
				);
			}

			return jsonResponse({ error: 'Not found' }, 404, corsHeaders);
		} catch (error) {
			console.error('Error handling request:', error);
			return jsonResponse(
				{
					error: 'Internal server error',
					message: error instanceof Error ? error.message : 'Unknown error',
				},
				500,
				corsHeaders
			);
		}
	}
}

/**
 * 从 query string 解析累计统计的读取范围
 *
 * node_id=X       只返回节点 X 的原始行 (节点恢复自身内存计数)
 * exclude_node=X  返回除 X 外所有节点的求和 (调用方叠加自己的实时值)
 * by_node=1       返回全部节点的原始行 (仪表盘展示各节点明细)
 * 都不带          返回全节点求和
 */
function readScope(url: URL): db.MetricsScope {
	return {
		nodeId: url.searchParams.get('node_id') || undefined,
		excludeNode: url.searchParams.get('exclude_node') || undefined,
		byNode: url.searchParams.get('by_node') === '1',
	};
}

function jsonResponse(data: any, status: number, headers: Record<string, string> = {}): Response {
	return new Response(JSON.stringify(data), {
		status,
		headers: {
			'Content-Type': 'application/json',
			...headers,
		},
	});
}
