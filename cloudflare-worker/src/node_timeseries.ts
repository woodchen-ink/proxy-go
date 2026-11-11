import { cachedJSON } from './lib/read_cache';

/**
 * node_timeseries 表读写: 节点整体 (不分路径) 的小时桶
 *
 * 表由 Go 侧迁移 0008 建立; 节点每 5 分钟心跳上报近 6 小时窗口。
 * 写入沿用 path_timeseries 的 MAX 语义 (节点重启后内存桶清零, 不能把已存的历史值踩小),
 * 并带 WHERE 跳过值未变的行 (不计 D1 写入行数)。
 */

// SPEED_COLUMNS 速度分档列, 顺序与节点 speedBucketBounds 一致: <1 / 1-5 / 5-20 / 20-50 / >=50 MB/s
export const SPEED_COLUMNS = ['speed_lt1', 'speed_1_5', 'speed_5_20', 'speed_20_50', 'speed_gt50'] as const;

const COUNTER_COLUMNS = [
	'requests',
	'bytes',
	'origin_bytes',
	'errors',
	'cache_hits',
	'cache_misses',
	'latency_ns',
	'big_bytes',
	'big_ns',
	'big_count',
	...SPEED_COLUMNS,
] as const;

// NodeTimeseriesUpload 节点上报的单个小时桶
export interface NodeTimeseriesUpload {
	node_id?: string;
	ts_hour: number;
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
	speed?: number[];
	updated_at: number;
}

// NodeHourRow 读出的一行, 速度档已拆成独立列
export type NodeHourRow = { node_id: string; ts_hour: number } & Record<(typeof COUNTER_COLUMNS)[number], number>;

export async function batchUpsertNodeTimeseries(db: D1Database, points: NodeTimeseriesUpload[]): Promise<void> {
	if (points.length === 0) return;
	const cols = COUNTER_COLUMNS.join(', ');
	const placeholders = COUNTER_COLUMNS.map((_, i) => `?${i + 3}`).join(', ');
	const sets = COUNTER_COLUMNS.map((c) => `${c} = MAX(excluded.${c}, ${c})`).join(', ');
	const changed = COUNTER_COLUMNS.map((c) => `excluded.${c} > ${c}`).join(' OR ');
	const sql = `INSERT INTO node_timeseries (node_id, ts_hour, ${cols}, updated_at)
		VALUES (?1, ?2, ${placeholders}, ?${COUNTER_COLUMNS.length + 3})
		ON CONFLICT(node_id, ts_hour) DO UPDATE SET ${sets}, updated_at = excluded.updated_at
		WHERE ${changed}`;
	const stmt = db.prepare(sql);
	await db.batch(
		points.map((p) => {
			const speed = SPEED_COLUMNS.map((_, i) => Number(p.speed?.[i] ?? 0));
			return stmt.bind(
				p.node_id || 'default',
				p.ts_hour,
				p.requests,
				p.bytes,
				p.origin_bytes,
				p.errors,
				p.cache_hits,
				p.cache_misses,
				p.latency_ns,
				p.big_bytes,
				p.big_ns,
				p.big_count,
				...speed,
				p.updated_at,
			);
		}),
	);
}

// loadNodeHours 读 [minHour, maxHour] 的全部节点小时桶; 表尚未建立 (还没有节点跑过迁移 0008) 时返回空
// D1 行每 5 分钟才更新一次, 在线节点的近期小时另由快照分钟桶补齐, 所以缓存 60s
export async function loadNodeHours(db: D1Database, minHour: number, maxHour: number): Promise<NodeHourRow[]> {
	return cachedJSON(`node-hours-v1:${minHour}:${maxHour}`, 60, () => queryNodeHours(db, minHour, maxHour));
}

async function queryNodeHours(db: D1Database, minHour: number, maxHour: number): Promise<NodeHourRow[]> {
	try {
		const res = await db
			.prepare(`SELECT node_id, ts_hour, ${COUNTER_COLUMNS.join(', ')} FROM node_timeseries WHERE ts_hour BETWEEN ?1 AND ?2`)
			.bind(minHour, maxHour)
			.all<NodeHourRow>();
		return res.results ?? [];
	} catch (e) {
		if (String(e).includes('no such table')) return [];
		throw e;
	}
}

export async function pruneNodeTimeseries(db: D1Database, cutoffHour: number): Promise<number> {
	try {
		const res = await db.prepare('DELETE FROM node_timeseries WHERE ts_hour < ?1').bind(cutoffHour).run();
		return res.meta?.changes || 0;
	} catch (e) {
		if (String(e).includes('no such table')) return 0;
		throw e;
	}
}
