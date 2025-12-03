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
			// 兼容旧 API (JSON 格式)
			// ============================================
			// 保留旧的 /config, /path_stats, /banned_ips 端点用于向后兼容

			// 根路径 - API 信息
			if (path === '/' || path === '') {
				return jsonResponse(
					{
						name: 'proxy-go-sync API v2',
						version: '2.0.0',
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
