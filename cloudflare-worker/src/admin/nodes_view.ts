/**
 * 节点维度视图: 每个节点承载的流量、回源、命中、下载速度, 以及按节点拆开的时间序列
 *
 * 数据源:
 *   - D1 node_timeseries 小时桶 (心跳 5 分钟上报, 最多滞后 5 分钟)
 *   - 在线节点快照里的 minute_series (最近 60 分钟, ≤15s 新鲜度)
 * 两者按小时对齐后逐字段取 MAX: 在线节点的当前小时 / 上一小时以分钟桶为准补齐 D1 的滞后,
 * 节点重启后分钟桶清零时又不会把 D1 已有的值盖小。
 *
 * 派生值 (命中率 / 错误率 / 均速 / 平均延迟 / 带宽) 只在合并后的原始量上计算, 不对派生值求和。
 */
import type { Env } from '../env';
import type { NodeStatus } from '../hub/hub';
import type { NodeSeriesPoint, NodeSnapshot } from '../hub/protocol';
import { TZ_NAME, formatLocalDate, localDayNumber, localMidnightSec } from '../lib/time';
import { SPEED_COLUMNS, loadNodeHours, type NodeHourRow } from '../node_timeseries';

type LiveMap = Record<string, NodeSnapshot & { received_at: number }>;

// SPEED_LABELS 速度分档文案, 与 SPEED_COLUMNS 同序
export const SPEED_LABELS = ['<1 MB/s', '1-5 MB/s', '5-20 MB/s', '20-50 MB/s', '≥50 MB/s'];

export interface Totals {
	requests: number;
	bytes: number;
	origin_bytes: number;
	errors: number;
	cache_hits: number;
	cache_misses: number;
	latency_ns: number;
	big_bytes: number;
	big_ns: number;
	big_count: number;
	speed: number[];
}

function emptyTotals(): Totals {
	return {
		requests: 0,
		bytes: 0,
		origin_bytes: 0,
		errors: 0,
		cache_hits: 0,
		cache_misses: 0,
		latency_ns: 0,
		big_bytes: 0,
		big_ns: 0,
		big_count: 0,
		speed: SPEED_COLUMNS.map(() => 0),
	};
}

const SCALAR_KEYS = ['requests', 'bytes', 'origin_bytes', 'errors', 'cache_hits', 'cache_misses', 'latency_ns', 'big_bytes', 'big_ns', 'big_count'] as const;

function addInto(a: Totals, b: Totals) {
	for (const k of SCALAR_KEYS) a[k] += b[k];
	for (let i = 0; i < a.speed.length; i++) a.speed[i] += b.speed[i] ?? 0;
}

function maxInto(a: Totals, b: Totals) {
	for (const k of SCALAR_KEYS) a[k] = Math.max(a[k], b[k]);
	for (let i = 0; i < a.speed.length; i++) a.speed[i] = Math.max(a.speed[i], b.speed[i] ?? 0);
}

function fromRow(r: NodeHourRow): Totals {
	const t = emptyTotals();
	for (const k of SCALAR_KEYS) t[k] = Number(r[k] ?? 0);
	t.speed = SPEED_COLUMNS.map((c) => Number(r[c] ?? 0));
	return t;
}

function fromPoint(p: NodeSeriesPoint): Totals {
	const t = emptyTotals();
	for (const k of SCALAR_KEYS) t[k] = Number(p[k] ?? 0);
	t.speed = SPEED_COLUMNS.map((_, i) => Number(p.speed?.[i] ?? 0));
	return t;
}

// derive 在合并后的原始量上算派生值; seconds 给出时附带平均带宽
export function derive(t: Totals, seconds?: number) {
	const lookups = t.cache_hits + t.cache_misses;
	return {
		...t,
		hit_rate: lookups > 0 ? t.cache_hits / lookups : 0,
		error_rate: t.requests > 0 ? t.errors / t.requests : 0,
		avg_latency_ms: t.requests > 0 ? t.latency_ns / t.requests / 1e6 : 0,
		avg_speed_bps: t.big_ns > 0 ? t.big_bytes / (t.big_ns / 1e9) : 0,
		avg_bandwidth_bps: seconds && seconds > 0 ? t.bytes / seconds : 0,
	};
}

// hourMatrix 每个节点 → 小时序号 → 合并后的桶 (D1 与分钟桶取 MAX)
async function hourMatrix(env: Env, live: LiveMap, minHour: number, maxHour: number): Promise<Map<string, Map<number, Totals>>> {
	const out = new Map<string, Map<number, Totals>>();
	const cell = (node: string, hour: number) => {
		let m = out.get(node);
		if (!m) out.set(node, (m = new Map()));
		let t = m.get(hour);
		if (!t) m.set(hour, (t = emptyTotals()));
		return t;
	};
	for (const r of await loadNodeHours(env.DB, minHour, maxHour)) {
		maxInto(cell(r.node_id, Number(r.ts_hour)), fromRow(r));
	}
	for (const [node, snap] of Object.entries(live)) {
		const perHour = new Map<number, Totals>();
		for (const p of snap.minute_series ?? []) {
			const h = Math.floor(p.ts / 3600);
			if (h < minHour || h > maxHour) continue;
			const t = perHour.get(h) ?? emptyTotals();
			addInto(t, fromPoint(p));
			perHour.set(h, t);
		}
		for (const [h, t] of perHour) maxInto(cell(node, h), t);
	}
	return out;
}

function sumHours(hours: Map<number, Totals> | undefined, from: number, to: number): Totals {
	const t = emptyTotals();
	for (const [h, v] of hours ?? []) if (h >= from && h <= to) addInto(t, v);
	return t;
}

function sumMinutes(points: NodeSeriesPoint[] | undefined): Totals {
	const t = emptyTotals();
	for (const p of points ?? []) addInto(t, fromPoint(p));
	return t;
}

// ---------- GET /admin/api/nodes/overview ----------

export async function buildNodesOverview(env: Env, live: LiveMap, nodes: NodeStatus[]) {
	const nowSec = Math.floor(Date.now() / 1000);
	const curHour = Math.floor(nowSec / 3600);
	const todayStartHour = localMidnightSec(nowSec) / 3600;
	const minHour = Math.min(todayStartHour, curHour - 23);
	const matrix = await hourMatrix(env, live, minHour, curHour);

	const ids = new Set<string>([...nodes.map((n) => n.node_id), ...Object.keys(live), ...matrix.keys()]);
	ids.delete('');
	const meta = new Map(nodes.map((n) => [n.node_id, n]));
	const todaySec = nowSec - localMidnightSec(nowSec);

	const cluster = { last_hour: emptyTotals(), today: emptyTotals(), last24h: emptyTotals() };
	let clusterBps = 0;
	let clusterRps = 0;

	const rows = [...ids].sort().map((id) => {
		const n = meta.get(id);
		const s = live[id];
		const lastHour = sumMinutes(s?.minute_series);
		const today = sumHours(matrix.get(id), todayStartHour, curHour);
		const last24h = sumHours(matrix.get(id), curHour - 23, curHour);
		addInto(cluster.last_hour, lastHour);
		addInto(cluster.today, today);
		addInto(cluster.last24h, last24h);
		clusterBps += s?.current_bandwidth_bps ?? 0;
		clusterRps += s?.requests_per_second ?? 0;
		return {
			node_id: id,
			online: !!s,
			state: n?.state ?? 'down',
			version: n?.version ?? '',
			hostname: n?.hostname ?? '',
			note: n?.note ?? '',
			last_seen: n?.last_seen ?? 0,
			live: s
				? {
						bandwidth_bps: s.current_bandwidth_bps,
						requests_per_second: s.requests_per_second,
						active_requests: s.active_requests,
						uptime_sec: s.uptime_sec,
						memory_bytes: s.memory_bytes,
						has_series: Array.isArray(s.minute_series),
					}
				: null,
			last_hour: derive(lastHour, 3600),
			today: derive(today, todaySec),
			last24h: derive(last24h, 86400),
		};
	});

	return {
		generated_at: nowSec,
		timezone: TZ_NAME,
		speed_labels: SPEED_LABELS,
		nodes: rows,
		cluster: {
			bandwidth_bps: clusterBps,
			requests_per_second: clusterRps,
			last_hour: derive(cluster.last_hour, 3600),
			today: derive(cluster.today, todaySec),
			last24h: derive(cluster.last24h, 86400),
		},
	};
}

// ---------- GET /admin/api/nodes/series?range=1h|24h|30d ----------

export type SeriesRange = '1h' | '24h' | '30d';

interface SeriesPoint {
	ts: number;
	requests: number;
	bytes: number;
	origin_bytes: number;
	errors: number;
	cache_hits: number;
	cache_misses: number;
	bandwidth_bps: number;
	avg_speed_bps: number;
}

function toPoint(ts: number, t: Totals, seconds: number): SeriesPoint {
	const d = derive(t, seconds);
	return {
		ts,
		requests: t.requests,
		bytes: t.bytes,
		origin_bytes: t.origin_bytes,
		errors: t.errors,
		cache_hits: t.cache_hits,
		cache_misses: t.cache_misses,
		bandwidth_bps: d.avg_bandwidth_bps,
		avg_speed_bps: d.avg_speed_bps,
	};
}

export async function buildNodesSeries(env: Env, live: LiveMap, nodes: NodeStatus[], range: SeriesRange) {
	const nowSec = Math.floor(Date.now() / 1000);
	const series: Record<string, SeriesPoint[]> = {};
	const totals: Record<string, ReturnType<typeof derive>> = {};
	let buckets: number[] = [];
	let step = 0;

	if (range === '1h') {
		// 分钟桶只在实时快照里, 离线节点没有这一档
		step = 60;
		const cur = Math.floor(nowSec / 60);
		buckets = Array.from({ length: 60 }, (_, i) => (cur - 59 + i) * 60);
		for (const [id, s] of Object.entries(live)) {
			const byTs = new Map((s.minute_series ?? []).map((p) => [p.ts, fromPoint(p)]));
			series[id] = buckets.map((ts) => toPoint(ts, byTs.get(ts) ?? emptyTotals(), step));
			totals[id] = derive(sumMinutes(s.minute_series), 3600);
		}
	} else {
		const curHour = Math.floor(nowSec / 3600);
		const days = range === '30d' ? 30 : 0;
		const minHour = days ? localMidnightSec(nowSec) / 3600 - (days - 1) * 24 : curHour - 23;
		const matrix = await hourMatrix(env, live, minHour, curHour);
		if (days) {
			step = 86400;
			const firstDay = localDayNumber(minHour * 3600);
			buckets = Array.from({ length: days }, (_, i) => localMidnightSec(nowSec) - (days - 1 - i) * 86400);
			for (const [id, hours] of matrix) {
				const perDay = buckets.map(() => emptyTotals());
				for (const [h, t] of hours) {
					const idx = localDayNumber(h * 3600) - firstDay;
					if (idx >= 0 && idx < days) addInto(perDay[idx], t);
				}
				series[id] = perDay.map((t, i) => toPoint(buckets[i], t, step));
				totals[id] = derive(sumHours(hours, minHour, curHour), days * 86400);
			}
		} else {
			step = 3600;
			buckets = Array.from({ length: 24 }, (_, i) => (minHour + i) * 3600);
			for (const [id, hours] of matrix) {
				series[id] = buckets.map((ts) => toPoint(ts, hours.get(ts / 3600) ?? emptyTotals(), step));
				totals[id] = derive(sumHours(hours, minHour, curHour), 86400);
			}
		}
	}

	const known = new Set(nodes.map((n) => n.node_id));
	return {
		generated_at: nowSec,
		timezone: TZ_NAME,
		range,
		step,
		buckets,
		labels: buckets.map((ts) => (step === 86400 ? formatLocalDate(ts) : ts)),
		speed_labels: SPEED_LABELS,
		node_ids: Object.keys(series).sort(),
		unknown_nodes: Object.keys(series).filter((id) => !known.has(id)),
		series,
		totals,
	};
}
