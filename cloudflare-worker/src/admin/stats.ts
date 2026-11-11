/**
 * 仪表盘累计 / 实时指标的跨节点聚合
 *
 * 数据源按节点二选一:
 *   - 在线节点: Hub 内存里的实时快照 (≤15s 新鲜度), 标记 live=true
 *   - 离线节点: D1 里最后一次上报的行 (心跳 5 分钟一次), 带 reported_at
 * 同一节点绝不两边相加。
 *
 * 派生值禁止直接相加或取平均: 命中率按合并后的 hits/(hits+misses) 重算,
 * 平均延迟按合并后的 total_latency_ns / request_count 重算。
 * 逐路径行与分节点明细走同一套路径前缀归并 (resolvePrefix), 否则明细挂不到合计行上。
 */
import type { Env } from '../env';
import type { NodeStatus } from '../hub/hub';
import type { NodeSnapshot, RawPathStat, RequestLog } from '../hub/protocol';
import { cachedJSON } from '../lib/read_cache';

// CLUSTER_ROWS_KEY D1 累计统计三张表的读缓存键; 只用于离线节点 (离线节点的行不会再变), 重置统计时失效
export const CLUSTER_ROWS_KEY = 'cluster-rows-v1';

type LiveMap = Record<string, NodeSnapshot & { received_at: number }>;

interface D1PathRow extends RawPathStat {
	node_id: string;
	updated_at: number;
}

export interface ClusterSources {
	live: LiveMap;
	pathRows: D1PathRow[]; // 仅离线节点
	statusRows: { node_id: string; status_code: string; count: number }[];
	latencyRows: { node_id: string; bucket: string; count: number }[];
	nodes: NodeStatus[];
	allNodeIds: string[];
}

export async function loadSources(env: Env, live: LiveMap, nodes: NodeStatus[]): Promise<ClusterSources> {
	const [paths, codes, lat] = await cachedJSON(CLUSTER_ROWS_KEY, 60, async () => {
		const res = await env.DB.batch([
			env.DB.prepare('SELECT * FROM path_stats'),
			env.DB.prepare('SELECT node_id, status_code, count FROM status_codes'),
			env.DB.prepare('SELECT node_id, bucket, count FROM latency_distribution'),
		]);
		return res.map((r) => ({ results: r.results ?? [] }));
	});
	const isLive = (id: string) => id in live;
	const pathRows = ((paths.results ?? []) as unknown as D1PathRow[]).filter((r) => !isLive(r.node_id));
	const statusRows = ((codes.results ?? []) as unknown as ClusterSources['statusRows']).filter((r) => !isLive(r.node_id));
	const latencyRows = ((lat.results ?? []) as unknown as ClusterSources['latencyRows']).filter((r) => !isLive(r.node_id));

	const ids = new Set<string>([...Object.keys(live), ...nodes.map((n) => n.node_id), ...pathRows.map((r) => r.node_id)]);
	ids.delete('');
	return { live, pathRows, statusRows, latencyRows, nodes, allNodeIds: [...ids].sort() };
}

export function clusterInfo(src: ClusterSources) {
	return {
		node_id: '',
		hub: true,
		nodes: src.allNodeIds,
		live_nodes: Object.keys(src.live).sort(),
		aggregated: src.allNodeIds.length > 1,
		degraded: false,
		fetched_at: Math.floor(Date.now() / 1000),
	};
}

// ---------- 通用 ----------

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export function formatBytes(n: number): string {
	if (!Number.isFinite(n) || n < 1024) return `${Math.max(0, Math.round(n || 0))} B`;
	let i = 0;
	let v = n;
	while (v >= 1024 && i < UNITS.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(2)} ${UNITS[i]}`;
}

function formatMs(ns: number): string {
	return `${(ns / 1e6).toFixed(2)}ms`;
}

function formatUptime(sec: number): string {
	const d = Math.floor(sec / 86400);
	const h = Math.floor((sec % 86400) / 3600);
	const m = Math.floor((sec % 3600) / 60);
	const s = Math.floor(sec % 60);
	if (d > 0) return `${d}天${h}时${m}分${s}秒`;
	if (h > 0) return `${h}时${m}分${s}秒`;
	if (m > 0) return `${m}分${s}秒`;
	return `${s}秒`;
}

function addCounts(into: Record<string, number>, from: Record<string, number> | undefined) {
	for (const [k, v] of Object.entries(from ?? {})) into[k] = (into[k] ?? 0) + Number(v || 0);
}

// ---------- /admin/api/metrics ----------

// nodeFilter 非空时最近请求只取该节点 (先过滤再截 100 条, 否则繁忙节点会挤掉其他节点)
export function buildMetrics(src: ClusterSources, nodeFilter = '') {
	const liveList = Object.entries(src.live);
	const statusCodes: Record<string, number> = {};
	const latencyDist: Record<string, number> = {};
	let totalBytes = 0;
	let latencyNs = 0;
	let latencyReqs = 0;

	// 累计量一律取逐路径累计 (启动时已从 D1 恢复); 进程级 total_bytes / latency_sum_ns 每次重启归零, 不能与历史混算
	for (const [, s] of liveList) {
		addCounts(statusCodes, s.status_codes);
		addCounts(latencyDist, s.latency_distribution);
		for (const r of s.path_stats ?? []) {
			totalBytes += Number(r.bytes_transferred);
			latencyNs += Number(r.total_latency_ns);
			latencyReqs += Number(r.request_count);
		}
	}
	for (const r of src.statusRows) statusCodes[r.status_code] = (statusCodes[r.status_code] ?? 0) + Number(r.count);
	for (const r of src.latencyRows) latencyDist[r.bucket] = (latencyDist[r.bucket] ?? 0) + Number(r.count);
	for (const r of src.pathRows) {
		totalBytes += Number(r.bytes_transferred);
		latencyNs += Number(r.total_latency_ns);
		latencyReqs += Number(r.request_count);
	}

	let totalRequests = 0;
	let totalErrors = 0;
	for (const [code, count] of Object.entries(statusCodes)) {
		totalRequests += count;
		if (parseInt(code, 10) >= 400) totalErrors += count;
	}

	const bandwidth: Record<string, number> = {};
	let rps = 0;
	let active = 0;
	let currentBps = 0;
	let goroutines = 0;
	let memory = 0;
	let sessionReqs = 0;
	let bytesPerSec = 0;
	let maxUptime = 0;
	let minLat = Infinity;
	let maxLat = 0;
	const recent: (RequestLog & { node_id: string })[] = [];
	const referers = new Map<string, { path: string; request_count: number; error_count: number; bytes_transferred: number; latency_weighted: number; last_access_time: number }>();

	for (const [id, s] of liveList) {
		addCounts(bandwidth, s.bandwidth_history);
		rps += s.requests_per_second;
		active += s.active_requests;
		currentBps += s.current_bandwidth_bps;
		goroutines += s.num_goroutine;
		memory += s.memory_bytes;
		sessionReqs += s.session_requests;
		bytesPerSec += s.uptime_sec > 0 ? s.total_bytes / s.uptime_sec : 0;
		maxUptime = Math.max(maxUptime, s.uptime_sec);
		if (s.min_latency_ns > 0) minLat = Math.min(minLat, s.min_latency_ns);
		maxLat = Math.max(maxLat, s.max_latency_ns);
		if (!nodeFilter || nodeFilter === id) for (const r of s.recent_requests ?? []) recent.push({ ...r, node_id: id });
		for (const raw of (s.top_referers ?? []) as Record<string, unknown>[]) {
			const key = String(raw.path ?? '');
			const reqs = Number(raw.request_count ?? 0);
			const avgMs = parseFloat(String(raw.avg_latency ?? '0')) || 0;
			const cur = referers.get(key) ?? { path: key, request_count: 0, error_count: 0, bytes_transferred: 0, latency_weighted: 0, last_access_time: 0 };
			cur.request_count += reqs;
			cur.error_count += Number(raw.error_count ?? 0);
			cur.bytes_transferred += Number(raw.bytes_transferred ?? 0);
			cur.latency_weighted += avgMs * reqs;
			cur.last_access_time = Math.max(cur.last_access_time, Number(raw.last_access_time ?? 0));
			referers.set(key, cur);
		}
	}

	recent.sort((a, b) => Date.parse(String(b.Time)) - Date.parse(String(a.Time)));
	const topReferers = [...referers.values()]
		.sort((a, b) => b.request_count - a.request_count || a.path.localeCompare(b.path))
		.slice(0, 20)
		.map((r) => ({
			path: r.path,
			request_count: r.request_count,
			error_count: r.error_count,
			bytes_transferred: r.bytes_transferred,
			avg_latency: r.request_count > 0 ? `${(r.latency_weighted / r.request_count).toFixed(2)}ms` : '0.00ms',
			last_access_time: r.last_access_time,
		}));

	const bandwidthHistory: Record<string, string> = {};
	for (const [k, v] of Object.entries(bandwidth)) bandwidthHistory[k] = `${formatBytes(v)}/min`;

	return {
		uptime: liveList.length ? formatUptime(maxUptime) : '-',
		active_requests: active,
		total_requests: totalRequests,
		total_errors: totalErrors,
		error_rate: totalRequests > 0 ? totalErrors / totalRequests : 0,
		num_goroutine: goroutines,
		memory_usage: formatBytes(memory),
		avg_response_time: latencyReqs > 0 ? formatMs(latencyNs / latencyReqs) : '0.00ms',
		requests_per_second: rps,
		current_session_requests: sessionReqs,
		total_bytes: totalBytes,
		bytes_per_second: bytesPerSec,
		status_code_stats: statusCodes,
		recent_requests: recent.slice(0, 100),
		top_referers: topReferers,
		latency_stats: {
			min: formatMs(Number.isFinite(minLat) ? minLat : 0),
			max: formatMs(maxLat),
			distribution: latencyDist,
		},
		bandwidth_history: bandwidthHistory,
		current_bandwidth: `${formatBytes(currentBps)}/s`,
		cluster: clusterInfo(src),
	};
}

// ---------- 路径前缀归并 ----------

export function prefixResolver(paths: string[]): (p: string) => string {
	const prefixes = paths.filter((p) => p !== '/').sort((a, b) => b.length - a.length);
	return (p: string) => {
		for (const prefix of prefixes) {
			if (p === prefix || (p.startsWith(prefix) && p[prefix.length] === '/')) return prefix;
		}
		return p;
	};
}

interface Acc {
	request_count: number;
	error_count: number;
	bytes_transferred: number;
	total_latency_ns: number;
	last_access_time: number;
	status_2xx: number;
	status_3xx: number;
	status_4xx: number;
	status_5xx: number;
	cache_hits: number;
	cache_misses: number;
	bytes_saved: number;
}

function emptyAcc(): Acc {
	return {
		request_count: 0,
		error_count: 0,
		bytes_transferred: 0,
		total_latency_ns: 0,
		last_access_time: 0,
		status_2xx: 0,
		status_3xx: 0,
		status_4xx: 0,
		status_5xx: 0,
		cache_hits: 0,
		cache_misses: 0,
		bytes_saved: 0,
	};
}

function addAcc(a: Acc, r: RawPathStat) {
	a.request_count += Number(r.request_count);
	a.error_count += Number(r.error_count);
	a.bytes_transferred += Number(r.bytes_transferred);
	a.total_latency_ns += Number(r.total_latency_ns);
	a.last_access_time = Math.max(a.last_access_time, Number(r.last_access_time || 0));
	a.status_2xx += Number(r.status_2xx);
	a.status_3xx += Number(r.status_3xx);
	a.status_4xx += Number(r.status_4xx);
	a.status_5xx += Number(r.status_5xx);
	a.cache_hits += Number(r.cache_hits);
	a.cache_misses += Number(r.cache_misses);
	a.bytes_saved += Number(r.bytes_saved);
}

function hitRate(a: Acc): number {
	const t = a.cache_hits + a.cache_misses;
	return t > 0 ? a.cache_hits / t : 0;
}

function avgLatency(a: Acc): string {
	return a.request_count > 0 ? formatMs(a.total_latency_ns / a.request_count) : '0.00ms';
}

// perNodeRows 每个节点一组逐路径原始行, 附带数据来源与时点
function perNodeRows(src: ClusterSources): { nodeId: string; live: boolean; reportedAt: number; rows: RawPathStat[] }[] {
	const out: { nodeId: string; live: boolean; reportedAt: number; rows: RawPathStat[] }[] = [];
	for (const [id, s] of Object.entries(src.live)) out.push({ nodeId: id, live: true, reportedAt: Math.floor(s.received_at / 1000), rows: s.path_stats ?? [] });
	const byNode = new Map<string, D1PathRow[]>();
	for (const r of src.pathRows) {
		const list = byNode.get(r.node_id) ?? [];
		list.push(r);
		byNode.set(r.node_id, list);
	}
	for (const [id, rows] of byNode) {
		const reportedAt = Math.max(...rows.map((r) => Number(r.updated_at || 0)));
		out.push({ nodeId: id, live: false, reportedAt: Math.floor(reportedAt / 1000), rows });
	}
	return out;
}

// ---------- /admin/api/path-stats ----------

export function buildPathStats(src: ClusterSources, configPaths: string[]) {
	const resolve = prefixResolver(configPaths);
	const totals = new Map<string, Acc>();
	const breakdown = new Map<string, Map<string, Acc & { live: boolean }>>();

	for (const group of perNodeRows(src)) {
		for (const r of group.rows) {
			const prefix = resolve(r.path);
			const total = totals.get(prefix) ?? emptyAcc();
			addAcc(total, r);
			totals.set(prefix, total);

			const nodes = breakdown.get(prefix) ?? new Map();
			const n = nodes.get(group.nodeId) ?? { ...emptyAcc(), live: group.live };
			addAcc(n, r);
			nodes.set(group.nodeId, n);
			breakdown.set(prefix, nodes);
		}
	}

	const result = [...totals.entries()].map(([path, a]) => ({
		path,
		request_count: a.request_count,
		error_count: a.error_count,
		bytes_transferred: a.bytes_transferred,
		avg_latency: avgLatency(a),
		last_access_time: a.last_access_time,
		status_2xx: a.status_2xx,
		status_3xx: a.status_3xx,
		status_4xx: a.status_4xx,
		status_5xx: a.status_5xx,
		cache_hits: a.cache_hits,
		cache_misses: a.cache_misses,
		cache_hit_rate: hitRate(a),
		bytes_saved: a.bytes_saved,
		nodes:
			src.allNodeIds.length > 1
				? [...(breakdown.get(path) ?? new Map()).entries()]
						.map(([nodeId, n]) => ({
							node_id: nodeId,
							is_current: n.live,
							request_count: n.request_count,
							error_count: n.error_count,
							error_rate: n.request_count > 0 ? n.error_count / n.request_count : 0,
							bytes_transferred: n.bytes_transferred,
							cache_hits: n.cache_hits,
							cache_misses: n.cache_misses,
							cache_hit_rate: hitRate(n),
							avg_latency: avgLatency(n),
						}))
						.sort((x, y) => x.node_id.localeCompare(y.node_id))
				: [],
	}));
	result.sort((a, b) => b.request_count - a.request_count || a.path.localeCompare(b.path));
	return { path_stats: result, cluster: clusterInfo(src) };
}

// ---------- /admin/api/node-stats ----------

export function buildNodeStats(src: ClusterSources) {
	const groups = new Map(perNodeRows(src).map((g) => [g.nodeId, g]));
	const nodes = src.allNodeIds.map((id) => {
		const g = groups.get(id);
		const a = emptyAcc();
		for (const r of g?.rows ?? []) addAcc(a, r);
		const live = !!g?.live;
		return {
			node_id: id,
			is_current: live,
			request_count: a.request_count,
			error_count: a.error_count,
			error_rate: a.request_count > 0 ? a.error_count / a.request_count : 0,
			bytes_transferred: a.bytes_transferred,
			bytes_saved: a.bytes_saved,
			cache_hit_rate: hitRate(a),
			avg_latency: avgLatency(a),
			reported_at: live ? 0 : (g?.reportedAt ?? 0),
		};
	});
	return { nodes, cluster: clusterInfo(src) };
}
