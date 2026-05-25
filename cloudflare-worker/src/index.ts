/**
 * Cloudflare Worker for proxy-go data synchronization (V2 - Column-based)
 * 使用列式存储替代 JSON 存储
 */

import * as db from './db';

export interface Env {
	DB: D1Database;
	API_TOKEN?: string;
}

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
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

		// API Token 认证
		if (env.API_TOKEN) {
			const authHeader = request.headers.get('Authorization');
			if (!authHeader || authHeader !== `Bearer ${env.API_TOKEN}`) {
				return jsonResponse({ error: 'Unauthorized' }, 401, corsHeaders);
			}
		}

		try {
			// ============================================
			// Path Stats API
			// ============================================
			if (path === '/path-stats' && request.method === 'GET') {
				const pathParam = url.searchParams.get('path');
				const stats = await db.getPathStats(env.DB, pathParam || undefined);
				return jsonResponse({ success: true, data: stats }, 200, corsHeaders);
			}

			if (path === '/path-stats' && request.method === 'POST') {
				const body = await request.json<{ stats: db.PathStat[] }>();
				if (!body.stats || !Array.isArray(body.stats)) {
					return jsonResponse({ error: 'Invalid request: stats array required' }, 400, corsHeaders);
				}
				await db.batchUpsertPathStats(env.DB, body.stats);
				return jsonResponse({ success: true, updated: body.stats.length }, 200, corsHeaders);
			}

			// ============================================
			// Banned IPs API
			// ============================================
			if (path === '/banned-ips' && request.method === 'GET') {
				const activeOnly = url.searchParams.get('active') === 'true';
				const bans = await db.getBannedIPs(env.DB, activeOnly);
				return jsonResponse({ success: true, data: bans }, 200, corsHeaders);
			}

			if (path === '/banned-ips' && request.method === 'POST') {
				const body = await request.json<{ bans: db.BannedIP[] }>();
				if (!body.bans || !Array.isArray(body.bans)) {
					return jsonResponse({ error: 'Invalid request: bans array required' }, 400, corsHeaders);
				}
				await db.batchUpsertBannedIPs(env.DB, body.bans);
				return jsonResponse({ success: true, updated: body.bans.length }, 200, corsHeaders);
			}

			if (path === '/banned-ips/history' && request.method === 'GET') {
				const ip = url.searchParams.get('ip');
				const limit = parseInt(url.searchParams.get('limit') || '100');
				const history = await db.getBannedIPHistory(env.DB, ip || undefined, limit);
				return jsonResponse({ success: true, data: history }, 200, corsHeaders);
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
			// Metrics API (status_codes, latency_distribution)
			// ============================================
			if (path === '/metrics/status-codes' && request.method === 'GET') {
				const metrics = await db.getStatusCodes(env.DB);
				return jsonResponse({ success: true, data: metrics }, 200, corsHeaders);
			}

			if (path === '/metrics/status-codes' && request.method === 'POST') {
				const body = await request.json<{ metrics: db.StatusCodeMetric[] }>();
				if (!body.metrics || !Array.isArray(body.metrics)) {
					return jsonResponse({ error: 'Invalid request: metrics array required' }, 400, corsHeaders);
				}
				await db.batchUpsertStatusCodes(env.DB, body.metrics);
				return jsonResponse({ success: true, updated: body.metrics.length }, 200, corsHeaders);
			}

			if (path === '/metrics/latency' && request.method === 'GET') {
				const metrics = await db.getLatencyDistribution(env.DB);
				return jsonResponse({ success: true, data: metrics }, 200, corsHeaders);
			}

			if (path === '/metrics/latency' && request.method === 'POST') {
				const body = await request.json<{ metrics: db.LatencyMetric[] }>();
				if (!body.metrics || !Array.isArray(body.metrics)) {
					return jsonResponse({ error: 'Invalid request: metrics array required' }, 400, corsHeaders);
				}
				await db.batchUpsertLatencyDistribution(env.DB, body.metrics);
				return jsonResponse({ success: true, updated: body.metrics.length }, 200, corsHeaders);
			}

			// ============================================
			// 通用 SQL 执行 endpoint
			//
			// 由 Go 端持有所有 schema / migration 定义, worker 只做 D1 的薄壳代理:
			//   POST /admin/sql/batch   { statements: [{sql, params?}] }  -> 事务批量执行 (D1 batch 是事务的)
			//   POST /admin/sql/query   { sql, params? }                  -> 查询返回 rows
			//
			// 安全模型: 这两个 endpoint 能直接执行任意 SQL (含 DROP), 全部走 API_TOKEN 鉴权,
			// 与其他写入 endpoint 同等保护; 没设 API_TOKEN 的部署等于裸奔, 与现状一致
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
			if (path === '/metrics/path-timeseries' && request.method === 'POST') {
				const body = await request.json<{ points: db.PathTimeseriesPoint[] }>();
				if (!body.points || !Array.isArray(body.points)) {
					return jsonResponse(
						{ error: 'Invalid request: points array required' },
						400,
						corsHeaders
					);
				}
				await db.batchUpsertPathTimeseries(env.DB, body.points);
				return jsonResponse(
					{ success: true, updated: body.points.length },
					200,
					corsHeaders
				);
			}

			if (path === '/metrics/path-timeseries' && request.method === 'GET') {
				const minHour = parseInt(url.searchParams.get('min_hour') || '0');
				const maxHour = parseInt(url.searchParams.get('max_hour') || '0');
				if (!minHour || !maxHour || maxHour < minHour) {
					return jsonResponse(
						{ error: 'min_hour and max_hour required (max_hour >= min_hour)' },
						400,
						corsHeaders
					);
				}
				// 上限保护: 单次最多查询 31 天 (744 小时)
				if (maxHour - minHour > 744) {
					return jsonResponse(
						{ error: 'range too wide, max 744 hours' },
						400,
						corsHeaders
					);
				}
				const data = await db.getAggregatedTimeseries(env.DB, minHour, maxHour);
				return jsonResponse({ success: true, data }, 200, corsHeaders);
			}

			if (path === '/metrics/path-timeseries' && request.method === 'DELETE') {
				const cutoff = parseInt(url.searchParams.get('cutoff_hour') || '0');
				if (!cutoff) {
					return jsonResponse(
						{ error: 'cutoff_hour required' },
						400,
						corsHeaders
					);
				}
				const deleted = await db.pruneOldTimeseries(env.DB, cutoff);
				return jsonResponse({ success: true, deleted }, 200, corsHeaders);
			}

			// ============================================
			// Referer Daily API (host 天级时间序列)
			// ============================================
			if (path === '/metrics/referer-daily' && request.method === 'POST') {
				const body = await request.json<{ points: db.RefererDailyPoint[] }>();
				if (!body.points || !Array.isArray(body.points)) {
					return jsonResponse(
						{ error: 'Invalid request: points array required' },
						400,
						corsHeaders
					);
				}
				await db.batchUpsertRefererDaily(env.DB, body.points);
				return jsonResponse(
					{ success: true, updated: body.points.length },
					200,
					corsHeaders
				);
			}

			if (path === '/metrics/referer-daily' && request.method === 'GET') {
				const minDate = parseInt(url.searchParams.get('min_date') || '0');
				const maxDate = parseInt(url.searchParams.get('max_date') || '0');
				if (!minDate || !maxDate || maxDate < minDate) {
					return jsonResponse(
						{ error: 'min_date and max_date required (max_date >= min_date)' },
						400,
						corsHeaders
					);
				}
				// 上限保护: 单次最多查询 90 天
				if (maxDate - minDate > 90) {
					return jsonResponse(
						{ error: 'range too wide, max 90 days' },
						400,
						corsHeaders
					);
				}
				const data = await db.getAggregatedRefererDaily(env.DB, minDate, maxDate);
				return jsonResponse({ success: true, data }, 200, corsHeaders);
			}

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
							'Path Stats': {
								'GET /path-stats': 'Get all path statistics',
								'GET /path-stats?path=/xxx': 'Get stats for specific path',
								'POST /path-stats': 'Batch update path statistics',
							},
							'Banned IPs': {
								'GET /banned-ips': 'Get all banned IPs',
								'GET /banned-ips?active=true': 'Get active bans only',
								'POST /banned-ips': 'Batch update banned IPs',
								'GET /banned-ips/history': 'Get ban history',
								'GET /banned-ips/history?ip=xxx': 'Get history for specific IP',
							},
							'Config Maps': {
								'GET /config-maps': 'Get all path configurations',
								'GET /config-maps?enabled=true': 'Get enabled paths only',
								'GET /config-maps/{path}': 'Get specific path config',
								'POST /config-maps': 'Batch update path configs',
								'DELETE /config-maps/{path}': 'Delete path config',
							},
							'Config Other': {
								'GET /config-other': 'Get all system configs',
								'GET /config-other?key=xxx': 'Get specific config',
								'POST /config-other': 'Batch update system configs',
							},
							'Admin SQL (token-protected)': {
								'POST /admin/sql/batch': 'Run a transactional batch of {sql, params?} statements',
								'POST /admin/sql/query': 'Run a single read query and return rows',
							},
							'Metrics': {
								'GET /metrics/status-codes': 'Get HTTP status code statistics',
								'POST /metrics/status-codes': 'Batch update status code stats',
								'GET /metrics/latency': 'Get latency distribution',
								'POST /metrics/latency': 'Batch update latency distribution',
								'GET /metrics/path-timeseries?min_hour&max_hour': 'Get aggregated path time series',
								'POST /metrics/path-timeseries': 'Batch upload node-local time-series buckets',
								'DELETE /metrics/path-timeseries?cutoff_hour': 'Prune old time-series buckets',
								'GET /metrics/referer-daily?min_date&max_date': 'Get aggregated referer host daily series',
								'POST /metrics/referer-daily': 'Batch upload node-local referer daily buckets',
								'DELETE /metrics/referer-daily?cutoff_date': 'Prune old referer daily buckets',
							},
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
	},
};

function jsonResponse(data: any, status: number, headers: Record<string, string> = {}): Response {
	return new Response(JSON.stringify(data), {
		status,
		headers: {
			'Content-Type': 'application/json',
			...headers,
		},
	});
}
