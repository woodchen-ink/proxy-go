/**
 * 公开远程清缓存接口 POST /api/cache/clear-url (Bearer CACHE_CLEAR_REMOTE_TOKEN)
 *
 * 契约与节点时代一致 (路由 / 令牌 / 请求体 / 响应体都不变), 第三方只需把域名换成中控:
 *   body { url: 完整 URL 或站内路径, type?: proxy|mirror|all }
 * 区别是现在清的是全部节点: 指令广播后最多等 5s 汇总 ack, cleared_items 为各节点之和。
 * 本地缓存清理后, 若有启用中的 CDN provider, 后台异步按 urls 类型清理对应完整 URL;
 * 输入是站内路径时需要额外传 host (中控自己的域名不是代理域名), 未传则跳过 CDN 清理。
 */
import { hubStub, type Env } from '../env';
import { bearerMatches, json } from '../lib/http';
import { readConfig } from '../admin/config';
import { activeProvider, purge } from '../admin/cdn';

interface ClearRequest {
	url?: string;
	type?: string;
	host?: string;
}

function respond(code: number, msg: string, data?: unknown): Response {
	return json(data === undefined ? { code, msg } : { code, data, msg }, code);
}

// normalizeURL 只取路径: 忽略 query 与 fragment, 去掉结尾斜杠 (根路径除外)
export function normalizeURL(raw: string): string | null {
	const trimmed = raw.trim();
	if (!trimmed) return null;
	let path: string;
	if (trimmed.startsWith('/')) {
		path = trimmed.split(/[?#]/)[0];
	} else {
		try {
			const u = new URL(trimmed);
			if (!u.host) return null;
			path = u.pathname || '/';
		} catch {
			return null;
		}
	}
	return path === '/' ? path : path.replace(/\/$/, '');
}

function cdnTarget(input: string, normalized: string, host?: string): string | null {
	try {
		const u = new URL(input.trim());
		if (u.protocol === 'http:' || u.protocol === 'https:') return `${u.protocol}//${u.host}${normalized}`;
		return null;
	} catch {
		const h = host?.trim();
		return h && /^[a-z0-9.-]+(:\d+)?$/i.test(h) ? `https://${h}${normalized}` : null;
	}
}

export async function handleCacheClearURL(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
	if (request.method !== 'POST') return respond(405, 'method not allowed');
	if (!env.CACHE_CLEAR_REMOTE_TOKEN) return respond(404, 'not found');
	if (!bearerMatches(request, env.CACHE_CLEAR_REMOTE_TOKEN)) {
		console.log(`[RemoteCacheClear] AUTH_FAIL ip=${request.headers.get('CF-Connecting-IP')}`);
		return respond(401, 'unauthorized');
	}

	let body: ClearRequest;
	try {
		body = (await request.json()) as ClearRequest;
	} catch {
		return respond(400, 'invalid request body');
	}
	const type = (body.type ?? '').trim() || 'all';
	if (!['proxy', 'mirror', 'all'].includes(type)) return respond(400, 'invalid cache type');
	const input = String(body.url ?? '');
	if (!input.trim()) return respond(400, 'url is required');
	const normalized = normalizeURL(input);
	if (!normalized) return respond(400, 'invalid url');

	const cmd = await hubStub(env).issueCommand('cache_clear', { type, scope: 'url', url: normalized }, 'api:clear-url', 3000);
	const cleared = cmd.acks.reduce((sum, a) => sum + (Number((a.result as { cleared_items?: number } | undefined)?.cleared_items) || 0), 0);
	console.log(`[RemoteCacheClear] OK type=${type} input=${JSON.stringify(input)} normalized=${normalized} nodes=${cmd.acks.length} cleared=${cleared}`);

	const target = cdnTarget(input, normalized, body.host);
	if (target) {
		ctx.waitUntil(
			(async () => {
				const provider = activeProvider((await readConfig(env)).config.CDN.Providers);
				if (!provider) return;
				const res = await purge(provider, { type: 'urls', targets: [target] }).catch((e) => ({ success: false, message: String(e), job_id: '' }));
				console.log(`[RemoteCacheClear] CDN_${res.success ? 'OK' : 'ERR'} target=${target} ${res.success ? `job_id=${res.job_id ?? ''}` : res.message}`);
			})(),
		);
	}

	return respond(200, 'cache cleared', {
		input_url: input,
		normalized_url: normalized,
		type,
		cleared_items: cleared,
		nodes: cmd.acks.length,
		pending_nodes: cmd.pending.length + cmd.offline.length,
	});
}
