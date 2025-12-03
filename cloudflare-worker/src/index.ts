/**
 * Cloudflare Worker for proxy-go data synchronization
 * Stores config, path_stats, and banned_ips data in D1 database
 */

export interface Env {
	DB: D1Database;
	API_TOKEN?: string; // Optional: for authentication
}

// 数据类型
type DataType = 'config' | 'path_stats' | 'banned_ips';

// 表名映射
const TABLE_MAP: Record<DataType, string> = {
	config: 'config',
	path_stats: 'path_stats',
	banned_ips: 'banned_ips',
};

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		// CORS 头
		const corsHeaders = {
			'Access-Control-Allow-Origin': '*',
			'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
			'Access-Control-Allow-Headers': 'Content-Type, Authorization',
		};

		// 处理 OPTIONS 请求
		if (request.method === 'OPTIONS') {
			return new Response(null, { headers: corsHeaders });
		}

		const url = new URL(request.url);
		const path = url.pathname;

		// 可选: API Token 认证
		if (env.API_TOKEN) {
			const authHeader = request.headers.get('Authorization');
			if (!authHeader || authHeader !== `Bearer ${env.API_TOKEN}`) {
				return jsonResponse({ error: 'Unauthorized' }, 401, corsHeaders);
			}
		}

		try {
			// GET /{type} - 获取数据
			if (request.method === 'GET' && path.length > 1) {
				const type = path.substring(1) as DataType;
				if (!isValidType(type)) {
					return jsonResponse({ error: 'Invalid data type' }, 400, corsHeaders);
				}
				return await handleGet(env.DB, type, corsHeaders);
			}

			// POST /{type} - 保存数据
			if (request.method === 'POST' && path.length > 1) {
				const type = path.substring(1) as DataType;
				if (!isValidType(type)) {
					return jsonResponse({ error: 'Invalid data type' }, 400, corsHeaders);
				}
				return await handlePost(env.DB, type, request, corsHeaders);
			}

			// 根路径返回 API 信息
			if (path === '/' || path === '') {
				return jsonResponse(
					{
						name: 'proxy-go-sync API',
						version: '1.0.0',
						endpoints: {
							'GET /config': 'Get config data',
							'POST /config': 'Save config data',
							'GET /path_stats': 'Get path statistics',
							'POST /path_stats': 'Save path statistics',
							'GET /banned_ips': 'Get banned IPs',
							'POST /banned_ips': 'Save banned IPs',
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

// 验证数据类型
function isValidType(type: string): type is DataType {
	return type === 'config' || type === 'path_stats' || type === 'banned_ips';
}

// 处理 GET 请求
async function handleGet(db: D1Database, type: DataType, corsHeaders: Record<string, string>): Promise<Response> {
	const tableName = TABLE_MAP[type];

	const result = await db
		.prepare(`SELECT data, updated_at FROM ${tableName} WHERE id = 1`)
		.first<{ data: string; updated_at: number }>();

	if (!result) {
		return jsonResponse(
			{
				error: 'Data not found',
				type,
			},
			404,
			corsHeaders
		);
	}

	return jsonResponse(
		{
			type,
			data: JSON.parse(result.data),
			updated_at: result.updated_at,
		},
		200,
		corsHeaders
	);
}

// 处理 POST 请求
async function handlePost(
	db: D1Database,
	type: DataType,
	request: Request,
	corsHeaders: Record<string, string>
): Promise<Response> {
	const tableName = TABLE_MAP[type];

	// 解析请求体
	let requestData: any;
	try {
		requestData = await request.json();
	} catch (error) {
		return jsonResponse({ error: 'Invalid JSON' }, 400, corsHeaders);
	}

	// 验证数据
	if (!requestData.data) {
		return jsonResponse({ error: 'Missing data field' }, 400, corsHeaders);
	}

	const dataStr = JSON.stringify(requestData.data);
	const updatedAt = Date.now();

	// 使用 INSERT OR REPLACE 确保只有一行数据
	await db
		.prepare(
			`INSERT INTO ${tableName} (id, data, updated_at)
       VALUES (1, ?1, ?2)
       ON CONFLICT(id) DO UPDATE SET
       data = excluded.data,
       updated_at = excluded.updated_at`
		)
		.bind(dataStr, updatedAt)
		.run();

	return jsonResponse(
		{
			success: true,
			type,
			updated_at: updatedAt,
		},
		200,
		corsHeaders
	);
}

// JSON 响应辅助函数
function jsonResponse(data: any, status: number, headers: Record<string, string> = {}): Response {
	return new Response(JSON.stringify(data), {
		status,
		headers: {
			'Content-Type': 'application/json',
			...headers,
		},
	});
}
