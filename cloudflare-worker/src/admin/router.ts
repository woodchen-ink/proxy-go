/**
 * 管理端 API (/admin/api/*), 契约与节点时代保持一致, 前端无需改调用路径
 *
 * 数据归属:
 *   - 配置 / 累计统计: D1 (中控是配置唯一写入方)
 *   - 实时指标 / 缓存统计: Hub 内存快照
 *   - 封禁等节点本地状态: 经 WS RPC 现场查询全部在线节点
 *   - 清缓存 / 开关缓存 / 解封 / 配置生效: 指令广播, 返回逐节点 ack
 */
import { hubStub, type Env } from '../env';
import { fail, HttpError, json, readJSON } from '../lib/http';
import type { CacheStats } from '../hub/protocol';
import { currentUser, handleAuthRoutes, isSameOrigin } from './auth';
import { activeProvider, purge, type PurgeRequest } from './cdn';
import { readConfig, validateProviders, writeConfig, type CDNProvider } from './config';
import { registerReleaseRoutes } from './release';
import { buildRefererDaily, buildTimeseries } from './series';
import { buildMetrics, buildNodeStats, buildPathStats, loadSources } from './stats';
import { buildNodesOverview, buildNodesSeries, type SeriesRange } from './nodes_view';

type Handler = (ctx: Ctx) => Promise<Response>;

interface Ctx {
	request: Request;
	env: Env;
	url: URL;
	user: string;
	params: string[];
}

const routes: [string, RegExp, Handler][] = [];
function route(method: string, pattern: string, h: Handler) {
	routes.push([method, new RegExp(`^${pattern}$`), h]);
}
registerReleaseRoutes(route);

export async function handleAdminAPI(request: Request, env: Env, url: URL): Promise<Response> {
	try {
		const authResp = await handleAuthRoutes(request, env, url.pathname);
		if (authResp) return authResp;

		const user = await currentUser(request, env);
		if (!user) return fail(401, 'Authentication required');
		if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method) && !isSameOrigin(request)) {
			return fail(403, 'cross-site request rejected');
		}

		for (const [method, re, h] of routes) {
			if (method !== request.method) continue;
			const m = re.exec(url.pathname);
			if (m) return await h({ request, env, url, user, params: m.slice(1) });
		}
		return fail(404, 'Not found');
	} catch (e) {
		if (e instanceof HttpError) return fail(e.status, e.message);
		console.error('admin api error', url.pathname, e);
		return fail(500, e instanceof Error ? e.message : 'internal error');
	}
}

// ---------- 统计 ----------

async function sources(env: Env) {
	const hub = hubStub(env);
	const [live, nodes] = await Promise.all([hub.liveSnapshots(), hub.nodeStatuses()]);
	return loadSources(env, live, nodes);
}

route('GET', '/admin/api/metrics', async ({ env, url }) => json(buildMetrics(await sources(env), url.searchParams.get('node') ?? '')));

// 节点维度: 每节点承载流量 / 回源 / 命中 / 下载速度, 与按节点拆开的序列
route('GET', '/admin/api/nodes/overview', async ({ env }) => {
	const hub = hubStub(env);
	const [live, nodes] = await Promise.all([hub.liveSnapshots(), hub.nodeStatuses()]);
	return json(await buildNodesOverview(env, live, nodes));
});

route('GET', '/admin/api/nodes/series', async ({ env, url }) => {
	const raw = url.searchParams.get('range');
	const range: SeriesRange = raw === '1h' || raw === '30d' ? raw : '24h';
	const hub = hubStub(env);
	const [live, nodes] = await Promise.all([hub.liveSnapshots(), hub.nodeStatuses()]);
	return json(await buildNodesSeries(env, live, nodes, range));
});

route('GET', '/admin/api/path-stats', async ({ env }) => {
	const [src, { config }] = await Promise.all([sources(env), readConfig(env)]);
	return json(buildPathStats(src, Object.keys(config.MAP)));
});

route('GET', '/admin/api/node-stats', async ({ env }) => json(buildNodeStats(await sources(env))));

route('GET', '/admin/api/metrics/timeseries', async ({ env, url }) => json(await buildTimeseries(env, url.searchParams.get('days'))));

route('GET', '/admin/api/metrics/referer-daily', async ({ env, url }) =>
	json(await buildRefererDaily(env, url.searchParams.get('days'), url.searchParams.get('top'))),
);

// resetStats 删全节点 D1 行 + 写重置 marker (同一事务), 再下发 pulse 让各节点立即清自己的内存
async function resetStats(env: Env, prefix: string, user: string): Promise<Response> {
	const resetAt = Date.now();
	const stmts = [env.DB.prepare('INSERT INTO stats_resets (path, reset_at) VALUES (?, ?)').bind(prefix, resetAt)];
	stmts.push(
		prefix
			? env.DB.prepare('DELETE FROM path_stats WHERE path = ? OR path LIKE ?').bind(prefix, `${prefix}/%`)
			: env.DB.prepare('DELETE FROM path_stats'),
	);
	await env.DB.batch(stmts);
	const cmd = await hubStub(env).issueCommand('pulse', { reason: 'stats_reset' }, user);
	return json({
		success: true,
		message: `${prefix ? '路径统计已重置' : '所有路径统计已重置'}, ${cmd.acks.length} 个在线节点已同步${cmd.offline.length ? `, ${cmd.offline.length} 个离线节点上线后同步` : ''}`,
		cluster_wide: true,
		command: cmd,
	});
}

route('POST', '/admin/api/path-stats/reset', async ({ env, request, user }) => {
	const body = await readJSON<{ path?: string }>(request);
	if (!body.path) throw new HttpError(400, 'Path is required');
	return resetStats(env, body.path, user);
});

route('POST', '/admin/api/path-stats/reset-all', async ({ env, user }) => resetStats(env, '', user));

// ---------- 配置 ----------

async function saveAndBroadcast(env: Env, cfg: Awaited<ReturnType<typeof readConfig>>['config'], user: string) {
	const version = await writeConfig(env, cfg);
	const cmd = await hubStub(env).issueCommand('pulse', { reason: 'config', version }, user);
	return { version, command: cmd };
}

route('GET', '/admin/api/config/get', async ({ env }) => json((await readConfig(env)).config));

route('POST', '/admin/api/config/save', async ({ env, request, user }) => {
	const cfg = await readJSON<Awaited<ReturnType<typeof readConfig>>['config']>(request);
	const { version, command } = await saveAndBroadcast(env, cfg, user);
	return json({ message: `配置已保存 (版本 ${version}), ${command.acks.length} 个在线节点已生效`, version, command });
});

// config/pull 节点时代用于"从 D1 拉取"; 中控本身就是 D1 的权威读写方, 直接返回当前配置
route('POST', '/admin/api/config/pull', async ({ env }) => json((await readConfig(env)).config));

// ---------- 缓存 ----------

const CACHE_TYPES = new Set(['proxy', 'mirror', 'all']);

function sumCacheStats(list: (CacheStats | undefined)[]): CacheStats {
	const out: CacheStats = {
		total_items: 0,
		total_size: 0,
		disk_file_count: 0,
		disk_size: 0,
		hit_count: 0,
		miss_count: 0,
		hit_rate: 0,
		bytes_saved: 0,
		enabled: false,
		format_fallback_hit: 0,
		image_cache_hit: 0,
		regular_cache_hit: 0,
	};
	for (const s of list) {
		if (!s) continue;
		out.total_items += s.total_items;
		out.total_size += s.total_size;
		out.disk_file_count += s.disk_file_count;
		out.disk_size += s.disk_size;
		out.hit_count += s.hit_count;
		out.miss_count += s.miss_count;
		out.bytes_saved += s.bytes_saved;
		out.format_fallback_hit += s.format_fallback_hit;
		out.image_cache_hit += s.image_cache_hit;
		out.regular_cache_hit += s.regular_cache_hit;
		out.enabled = out.enabled || s.enabled;
	}
	const total = out.hit_count + out.miss_count;
	out.hit_rate = total > 0 ? out.hit_count / total : 0;
	return out;
}

route('GET', '/admin/api/cache/stats', async ({ env }) => {
	const live = await hubStub(env).liveSnapshots();
	const snaps = Object.entries(live);
	return json({
		proxy: sumCacheStats(snaps.map(([, s]) => s.cache?.proxy)),
		mirror: sumCacheStats(snaps.map(([, s]) => s.cache?.mirror)),
		nodes: snaps.map(([id, s]) => ({ node_id: id, proxy: s.cache?.proxy ?? null, mirror: s.cache?.mirror ?? null })),
	});
});

route('POST', '/admin/api/cache/enable', async ({ env, request, user }) => {
	const body = await readJSON<{ type?: string; enabled?: boolean }>(request);
	if (body.type !== 'proxy' && body.type !== 'mirror') throw new HttpError(400, 'Invalid cache type');
	const cmd = await hubStub(env).issueCommand('cache_enable', { type: body.type, enabled: !!body.enabled }, user);
	return json({ success: true, command: cmd });
});

function cacheClearResponse(cmd: Awaited<ReturnType<ReturnType<typeof hubStub>['issueCommand']>>, what: string) {
	const cleared = cmd.acks.reduce((sum, a) => sum + (Number((a.result as { cleared_items?: number } | undefined)?.cleared_items) || 0), 0);
	const failed = cmd.acks.filter((a) => !a.ok);
	return json({
		success: failed.length === 0,
		cleared_items: cleared,
		message: `${what}: ${cmd.acks.length} 个节点共清理 ${cleared} 项${cmd.pending.length ? `, ${cmd.pending.length} 个节点未及时回应` : ''}${cmd.offline.length ? `, ${cmd.offline.length} 个离线节点上线后执行` : ''}`,
		command: cmd,
	});
}

route('POST', '/admin/api/cache/clear', async ({ env, request, user }) => {
	const body = await readJSON<{ type?: string }>(request);
	if (!CACHE_TYPES.has(body.type ?? '')) throw new HttpError(400, 'Invalid cache type');
	return cacheClearResponse(await hubStub(env).issueCommand('cache_clear', { type: body.type, scope: 'all' }, user), '已清空缓存');
});

route('POST', '/admin/api/cache/clear-by-path', async ({ env, request, user }) => {
	const body = await readJSON<{ type?: string; path_prefix?: string }>(request);
	if (!body.path_prefix) throw new HttpError(400, 'path_prefix is required');
	const type = body.type || 'all';
	if (!CACHE_TYPES.has(type)) throw new HttpError(400, 'Invalid cache type');
	return cacheClearResponse(
		await hubStub(env).issueCommand('cache_clear', { type, scope: 'prefix', prefix: body.path_prefix }, user),
		`已清理前缀 ${body.path_prefix}`,
	);
});

route('POST', '/admin/api/cache/clear-by-urls', async ({ env, request, user }) => {
	const body = await readJSON<{ type?: string; urls?: string[] }>(request);
	if (!body.urls?.length) throw new HttpError(400, 'urls is required and cannot be empty');
	const type = body.type || 'all';
	if (!CACHE_TYPES.has(type)) throw new HttpError(400, 'Invalid cache type');
	return cacheClearResponse(await hubStub(env).issueCommand('cache_clear', { type, scope: 'urls', urls: body.urls }, user), `已清理 ${body.urls.length} 个 URL`);
});

route('GET', '/admin/api/cache/config', async ({ env }) => {
	const { config } = await readConfig(env);
	return json({ proxy: config.Cache ?? {}, mirror: config.MirrorCache ?? {} });
});

route('POST', '/admin/api/cache/config', async ({ env, request, user }) => {
	const body = await readJSON<{ type?: string; config?: Record<string, unknown> }>(request);
	if (body.type !== 'proxy' && body.type !== 'mirror') throw new HttpError(400, 'Invalid cache type');
	if (!body.config || typeof body.config !== 'object') throw new HttpError(400, 'config is required');
	const { config } = await readConfig(env);
	config[body.type === 'proxy' ? 'Cache' : 'MirrorCache'] = body.config;
	await saveAndBroadcast(env, config, user);
	return json({ success: true });
});

// ---------- CDN ----------

route('GET', '/admin/api/cdn/providers', async ({ env }) => json({ providers: (await readConfig(env)).config.CDN.Providers }));

route('POST', '/admin/api/cdn/providers', async ({ env, request, user }) => {
	const body = await readJSON<{ providers?: CDNProvider[] }>(request);
	const providers = body.providers ?? [];
	validateProviders(providers);
	const { config } = await readConfig(env);
	config.CDN = { Providers: providers };
	await saveAndBroadcast(env, config, user);
	return json({ message: 'CDN 配置已保存', providers });
});

route('POST', '/admin/api/cdn/purge', async ({ env, request }) => {
	const req = await readJSON<PurgeRequest>(request);
	const provider = activeProvider((await readConfig(env)).config.CDN.Providers);
	if (!provider) return json({ error: 'no enabled CDN provider' }, 424);
	const result = await purge(provider, req);
	return result.success ? json({ result }) : json({ error: result.message ?? 'purge failed', result }, 502);
});

// ---------- 安全 (节点本地状态, RPC 汇总) ----------

route('GET', '/admin/api/security/banned-ips', async ({ env }) => {
	const { replies, missing } = await hubStub(env).rpcAll('security_banned', {});
	const bans: Record<string, unknown>[] = [];
	for (const r of replies) {
		if (!r.ok) continue;
		const data = r.data as { banned_ips?: Record<string, unknown>[] } | undefined;
		for (const b of data?.banned_ips ?? []) bans.push({ ...b, node_id: r.node_id });
	}
	return json({ banned_ips: bans, count: bans.length, missing_nodes: missing });
});

route('POST', '/admin/api/security/unban', async ({ env, request, user }) => {
	const body = await readJSON<{ ip?: string }>(request);
	if (!body.ip) throw new HttpError(400, 'IP address is required');
	const cmd = await hubStub(env).issueCommand('unban', { ip: body.ip }, user);
	const unbanned = cmd.acks.some((a) => (a.result as { unbanned?: boolean } | undefined)?.unbanned);
	return json({ success: unbanned, message: unbanned ? `IP ${body.ip} 已在全部节点解封` : `IP ${body.ip} 未在任何在线节点被封禁`, command: cmd });
});

route('GET', '/admin/api/security/stats', async ({ env }) => {
	const { replies, missing } = await hubStub(env).rpcAll('security_stats', {});
	const merged: Record<string, unknown> = { banned_ips_count: 0, error_records_count: 0, enabled: false, config: {} };
	const nodes: Record<string, unknown> = {};
	for (const r of replies) {
		if (!r.ok || !r.data) continue;
		const d = r.data as Record<string, unknown>;
		nodes[r.node_id] = d;
		for (const [k, v] of Object.entries(d)) {
			if (typeof v === 'number') merged[k] = Number(merged[k] ?? 0) + v;
		}
		merged.enabled = merged.enabled || !!d.enabled;
		if (d.config && Object.keys(merged.config as object).length === 0) merged.config = d.config;
	}
	return json({ ...merged, nodes, missing_nodes: missing });
});

route('GET', '/admin/api/security/check-ip', async ({ env, url, request }) => {
	const ip = url.searchParams.get('ip') || request.headers.get('CF-Connecting-IP') || '';
	const { replies } = await hubStub(env).rpcAll('security_check', { ip });
	const per = replies.filter((r) => r.ok).map((r) => ({ node_id: r.node_id, ...(r.data as Record<string, unknown>) }));
	const banned = per.find((p) => (p as { banned?: boolean }).banned);
	return json({ ip, banned: !!banned, ...(banned ?? {}), nodes: per });
});

route('GET', '/admin/api/security/ban-history', async ({ env, url }) => {
	const limit = Math.min(parseInt(url.searchParams.get('limit') ?? '100', 10) || 100, 500);
	const { replies } = await hubStub(env).rpcAll('security_history', { limit });
	const history: Record<string, unknown>[] = [];
	for (const r of replies) {
		const data = r.data as { history?: Record<string, unknown>[] } | undefined;
		for (const h of data?.history ?? []) history.push({ ...h, node_id: r.node_id });
	}
	history.sort((a, b) => String(b.ban_time ?? '').localeCompare(String(a.ban_time ?? '')));
	return json({ history: history.slice(0, limit), total: Math.min(history.length, limit) });
});

// ---------- 中控: 节点 / 指令 / 事件 ----------

route('GET', '/admin/api/hub/nodes', async ({ env }) => {
	const hub = hubStub(env);
	const [nodes, probes, live] = await Promise.all([hub.nodeStatuses(), hub.listProbes(), hub.liveSnapshots()]);
	return json({
		nodes: nodes.map((n) => {
			const s = live[n.node_id];
			return {
				...n,
				probe: (probes as { node_id: string }[]).find((p) => p.node_id === n.node_id) ?? null,
				live: s
					? {
							requests_per_second: s.requests_per_second,
							current_bandwidth_bps: s.current_bandwidth_bps,
							active_requests: s.active_requests,
							uptime_sec: s.uptime_sec,
							memory_bytes: s.memory_bytes,
							config_version: s.config_version,
						}
					: null,
			};
		}),
		config_version: (await readConfig(env)).version,
	});
});

route('POST', '/admin/api/hub/nodes/([^/]+)', async ({ env, request, params }) => {
	const body = await readJSON<{ origin_host?: string; note?: string }>(request);
	await hubStub(env).updateNode(decodeURIComponent(params[0]), body);
	return json({ success: true });
});

route('DELETE', '/admin/api/hub/nodes/([^/]+)', async ({ env, params }) => {
	await hubStub(env).removeNode(decodeURIComponent(params[0]));
	return json({ success: true });
});

route('GET', '/admin/api/hub/commands', async ({ env, url }) =>
	json({ commands: await hubStub(env).listCommands(parseInt(url.searchParams.get('limit') ?? '50', 10)) }),
);

route('GET', '/admin/api/hub/events', async ({ env, url }) =>
	json({ events: await hubStub(env).listEvents(parseInt(url.searchParams.get('limit') ?? '100', 10)) }),
);

// ---------- DNS 绑定 ----------

const dns = (env: Env, op: string, args: Record<string, unknown> = {}) => hubStub(env).dnsOp(op, args);

route('GET', '/admin/api/dns/credentials', async ({ env }) => json({ credentials: await dns(env, 'listCredentials') }));
route('POST', '/admin/api/dns/credentials', async ({ env, request }) => json(await dns(env, 'saveCredential', await readJSON(request))));
route('DELETE', '/admin/api/dns/credentials/(\\d+)', async ({ env, params }) => json(await dns(env, 'deleteCredential', { id: Number(params[0]) })));
route('GET', '/admin/api/dns/credentials/(\\d+)/discover', async ({ env, params }) => json(await dns(env, 'discover', { id: Number(params[0]) })));
route('GET', '/admin/api/dns/suggest-origins', async ({ env, url }) =>
	json({
		origins: await dns(env, 'suggestOrigins', { credential_id: Number(url.searchParams.get('credential_id')), zone_id: url.searchParams.get('zone_id') ?? '' }),
	}),
);
route('GET', '/admin/api/dns/bindings', async ({ env }) => json({ bindings: await dns(env, 'listBindings') }));
route('POST', '/admin/api/dns/bindings', async ({ env, request }) => json(await dns(env, 'saveBinding', await readJSON(request))));
route('DELETE', '/admin/api/dns/bindings/(\\d+)', async ({ env, params }) => json(await dns(env, 'deleteBinding', { id: Number(params[0]) })));
route('POST', '/admin/api/dns/bindings/(\\d+)/check', async ({ env, params }) => json(await dns(env, 'check', { id: Number(params[0]) })));
route('POST', '/admin/api/dns/bindings/(\\d+)/switch', async ({ env, params, request, user }) => {
	const body = await readJSON<{ node_id?: string }>(request);
	if (!body.node_id) throw new HttpError(400, 'node_id is required');
	return json(await dns(env, 'switch', { id: Number(params[0]), node_id: body.node_id, user }));
});
