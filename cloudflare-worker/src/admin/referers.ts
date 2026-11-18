/**
 * 引用来源 (Referer host) 视图与操作
 *
 * 数据: referer_daily (近 35 天, 节点心跳 UPSERT) + referer_hosts (更早的天按 host 累加, 永久保留)。
 * 读 referer_daily 40 天窗口 + referer_hosts 的相关行与最近 1000 个 (索引查找, 不全表扫), 结果缓存 5 分钟;
 * 前端只在打开时加载、手动刷新, 不轮询。自有域名与告警阈值存 Hub DO kv。
 */
import * as db from '../db';
import { hubStub, type Env } from '../env';
import { HttpError, json, readJSON } from '../lib/http';
import { cachedJSON, dropCached } from '../lib/read_cache';
import { hostMatches, normalizeRefererHost } from '../lib/referer';
import { TZ_NAME, formatLocalDate, localDayNumber, localMidnightSec } from '../lib/time';
import { readConfig, saveAndBroadcast } from './config';

type Route = (method: string, pattern: string, h: (ctx: { request: Request; env: Env; user: string; url: URL; params: string[] }) => Promise<Response>) => void;

const VIEW_DAYS = 30;
const READ_DAYS = 40; // 覆盖 referer_daily 全部保留期, 全时合计才不漏清理前的几天
const CHART_HOSTS = 50; // 只给前 N 个 host 带逐日序列, 控制响应体积
const MAX_ACTIVE = 500;
const MAX_HISTORY = 1000;
const NEW_DAYS = 3;
const CACHE_KEY = 'referers-v2';
const CACHE_TTL = 300;

interface PathHit {
	p: string;
	n: number;
}

interface DayPoint {
	date: string;
	requests: number;
	bytes: number;
	errors: number;
	blocked: number;
}

interface HostRow {
	host: string;
	daily: DayPoint[];
	requests: number; // 近 30 天
	bytes: number;
	errors: number;
	blocked: number;
	redirected: number;
	distinct_paths: number; // 近 30 天单日最大值 (节点侧每天封顶 200)
	top_paths: PathHit[];
	all_requests: number; // 首次出现以来
	all_bytes: number;
	first_seen: string;
	last_seen: string;
	is_new: boolean;
	is_own: boolean;
	is_banned: boolean;
}

interface Acc {
	requests: number;
	bytes: number;
	errors: number;
	blocked: number;
	redirected: number;
	distinct: number;
	paths: Map<string, number>;
	firstDay: number;
	lastDay: number;
	allRequests: number;
	allBytes: number;
	allBlocked: number;
	byDay: Map<number, DayPoint>;
}

function mergePaths(into: Map<string, number>, raw: string | null): void {
	if (!raw) return;
	for (const part of raw.split('\x1e')) {
		try {
			const list = JSON.parse(part) as PathHit[];
			if (!Array.isArray(list)) continue;
			for (const h of list) if (h && typeof h.p === 'string') into.set(h.p, (into.get(h.p) ?? 0) + (Number(h.n) || 0));
		} catch {
			/* 坏数据跳过 */
		}
	}
}

function topOf(m: Map<string, number>, n = 10): PathHit[] {
	return [...m.entries()]
		.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
		.slice(0, n)
		.map(([p, c]) => ({ p, n: c }));
}

function bannedHosts(cfg: Record<string, unknown>): string[] {
	const rb = (cfg.Security as { RefererBan?: { Enabled?: boolean; Hosts?: string[] } } | undefined)?.RefererBan;
	return rb?.Enabled && Array.isArray(rb.Hosts) ? rb.Hosts.map(normalizeRefererHost) : [];
}

async function buildView(env: Env) {
	const nowSec = Math.floor(Date.now() / 1000);
	const today = localDayNumber(nowSec);
	const viewMin = today - (VIEW_DAYS - 1);
	const startSec = localMidnightSec(nowSec) - (VIEW_DAYS - 1) * 86400;

	const [points, { config }, settings] = await Promise.all([
		db.getAggregatedRefererDaily(env.DB, today - READ_DAYS, today).catch((e) => {
			console.error('referer daily load failed', e);
			return [] as db.AggregatedRefererDayPoint[];
		}),
		readConfig(env),
		hubStub(env).refererSettings(),
	]);
	const archive = await db.getRefererHostArchive(env.DB, [...new Set(points.map((p) => p.host))], MAX_HISTORY);
	const own = [...settings.own_hosts, ...settings.auto_own_hosts];
	const banned = bannedHosts(config);
	const archByHost = new Map(archive.map((a) => [a.host, a]));

	const acc = new Map<string, Acc>();
	for (const p of points) {
		const day = Number(p.ts_date);
		let a = acc.get(p.host);
		if (!a) {
			a = { requests: 0, bytes: 0, errors: 0, blocked: 0, redirected: 0, distinct: 0, paths: new Map(), firstDay: day, lastDay: day, allRequests: 0, allBytes: 0, allBlocked: 0, byDay: new Map() };
			acc.set(p.host, a);
		}
		const req = Number(p.requests) || 0;
		const bytes = Number(p.bytes) || 0;
		a.allRequests += req;
		a.allBytes += bytes;
		a.allBlocked += Number(p.blocked) || 0;
		a.firstDay = Math.min(a.firstDay, day);
		a.lastDay = Math.max(a.lastDay, day);
		if (day < viewMin) continue;
		a.requests += req;
		a.bytes += bytes;
		a.errors += Number(p.errors) || 0;
		a.blocked += Number(p.blocked) || 0;
		a.redirected += Number(p.redirected) || 0;
		a.distinct = Math.max(a.distinct, Number(p.distinct_paths) || 0);
		mergePaths(a.paths, p.paths);
		a.byDay.set(day, { date: '', requests: req, bytes, errors: Number(p.errors) || 0, blocked: Number(p.blocked) || 0 });
	}

	const active: HostRow[] = [];
	for (const [host, a] of acc) {
		if (a.requests === 0) continue; // 只在 30 天前出现过: 归入历史
		const arch = archByHost.get(host);
		const firstDay = arch ? Math.min(arch.first_date, a.firstDay) : a.firstDay;
		active.push({
			host,
			daily: [],
			requests: a.requests,
			bytes: a.bytes,
			errors: a.errors,
			blocked: a.blocked,
			redirected: a.redirected,
			distinct_paths: a.distinct,
			top_paths: topOf(a.paths),
			all_requests: a.allRequests + (Number(arch?.requests) || 0),
			all_bytes: a.allBytes + (Number(arch?.bytes) || 0),
			first_seen: formatLocalDate(firstDay * 86400),
			last_seen: formatLocalDate(a.lastDay * 86400),
			is_new: firstDay >= today - (NEW_DAYS - 1),
			is_own: hostMatches(host, own),
			is_banned: hostMatches(host, banned),
		});
	}
	active.sort((x, y) => y.requests - x.requests || x.host.localeCompare(y.host));
	for (const row of active.slice(0, CHART_HOSTS)) {
		const a = acc.get(row.host)!;
		row.daily = Array.from({ length: VIEW_DAYS }, (_, i) => {
			const d = a.byDay.get(viewMin + i);
			return { date: formatLocalDate(startSec + i * 86400), requests: d?.requests ?? 0, bytes: d?.bytes ?? 0, errors: d?.errors ?? 0, blocked: d?.blocked ?? 0 };
		});
	}

	// 历史: 近 30 天没出现、但曾经出现过的 host (长期汇总 + 35 天窗口里 30 天之前的几天)
	const activeSet = new Set(active.map((r) => r.host));
	const history = new Map<string, { host: string; requests: number; bytes: number; blocked: number; firstDay: number; lastDay: number; peakDay: number; paths: PathHit[] }>();
	for (const a of archive) {
		if (activeSet.has(a.host)) continue;
		const m = new Map<string, number>();
		mergePaths(m, a.peak_paths);
		history.set(a.host, {
			host: a.host,
			requests: Number(a.requests) || 0,
			bytes: Number(a.bytes) || 0,
			blocked: Number(a.blocked) || 0,
			firstDay: Number(a.first_date),
			lastDay: Number(a.last_date),
			peakDay: Number(a.peak_date),
			paths: topOf(m),
		});
	}
	for (const [host, a] of acc) {
		if (activeSet.has(host)) continue;
		const h = history.get(host) ?? { host, requests: 0, bytes: 0, blocked: 0, firstDay: a.firstDay, lastDay: a.lastDay, peakDay: 0, paths: [] };
		h.requests += a.allRequests;
		h.bytes += a.allBytes;
		h.blocked += a.allBlocked;
		h.firstDay = Math.min(h.firstDay, a.firstDay);
		h.lastDay = Math.max(h.lastDay, a.lastDay);
		history.set(host, h);
	}
	const historyRows = [...history.values()]
		.sort((x, y) => y.lastDay - x.lastDay || y.requests - x.requests)
		.slice(0, MAX_HISTORY)
		.map((h) => ({
			host: h.host,
			requests: h.requests,
			bytes: h.bytes,
			blocked: h.blocked,
			first_seen: formatLocalDate(h.firstDay * 86400),
			last_seen: formatLocalDate(h.lastDay * 86400),
			peak_date: h.peakDay ? formatLocalDate(h.peakDay * 86400) : '',
			top_paths: h.paths,
			is_own: hostMatches(h.host, own),
			is_banned: hostMatches(h.host, banned),
		}));

	return {
		generated_at: nowSec,
		timezone: TZ_NAME,
		days: VIEW_DAYS,
		new_days: NEW_DAYS,
		hosts: active.slice(0, MAX_ACTIVE),
		total_hosts: active.length,
		history: historyRows,
		history_truncated: history.size > MAX_HISTORY,
		settings,
	};
}

export function registerRefererRoutes(route: Route): void {
	route('GET', '/admin/api/referers', async ({ env }) => json(await cachedJSON(CACHE_KEY, CACHE_TTL, () => buildView(env))));

	// 自有域名 / 告警阈值
	route('POST', '/admin/api/referers/settings', async ({ env, request }) => {
		const body = await readJSON<Record<string, unknown>>(request);
		const settings = await hubStub(env).updateRefererSettings(body);
		await dropCached(CACHE_KEY);
		return json(settings);
	});

	// 一键拉黑 / 取消: 改全局 Referer 黑名单并广播配置
	route('POST', '/admin/api/referers/ban', async ({ env, request, user }) => {
		const body = await readJSON<{ host?: string; banned?: boolean }>(request);
		const host = normalizeRefererHost(body.host);
		if (!/^[a-z0-9.-]+$/.test(host) || !host.includes('.')) throw new HttpError(400, 'invalid host');
		const { config } = await readConfig(env);
		const security = (config.Security ?? {}) as Record<string, unknown>;
		const rb = { Enabled: false, Mode: 'blacklist', Hosts: [] as string[], BlockEmpty: false, ...((security.RefererBan as object) ?? {}) } as {
			Enabled: boolean;
			Hosts: string[];
		};
		// 原有条目原样保留, 只按归一化后的值比对
		const hosts = Array.isArray(rb.Hosts) ? rb.Hosts : [];
		const normalized = hosts.map(normalizeRefererHost);
		let next: string[];
		if (body.banned === false) {
			next = hosts.filter((_, i) => normalized[i] !== host);
			if (next.length === hosts.length) throw new HttpError(409, '该来源不在全局黑名单里 (可能由上级域名或路径级规则拦截), 请到配置页处理');
		} else {
			if (hostMatches(host, normalized) && rb.Enabled) return json({ message: '已在全局黑名单中', changed: false });
			next = normalized.includes(host) ? hosts : [...hosts, host];
		}
		const wasEnabled = !!rb.Enabled;
		config.Security = { ...security, RefererBan: { ...rb, Hosts: next, Enabled: body.banned === false ? rb.Enabled : true } };
		const { version, command } = await saveAndBroadcast(env, config, user);
		await dropCached(CACHE_KEY);
		const verb = body.banned === false ? '已移出全局黑名单' : '已加入全局黑名单';
		const extra = body.banned !== false && !wasEnabled && hosts.length ? ', 并已启用全局 Referer 黑名单 (原有名单一并生效)' : '';
		return json({ message: `${host} ${verb}${extra}, ${command.acks.length} 个在线节点已生效`, version, changed: true });
	});
}
