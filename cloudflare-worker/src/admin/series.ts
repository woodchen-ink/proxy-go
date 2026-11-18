/**
 * 路径访问趋势 (小时桶) 的视图组装 (引用来源见 referers.ts)
 *
 * 数据来自 D1 (节点每 5 分钟 UPSERT, worker 侧跨节点求和), 按东八区切日;
 * 视图形状与节点时代 handler/timeseries.go / referer_daily.go 保持一致。
 */
import * as db from '../db';
import type { Env } from '../env';
import { TZ_NAME, formatLocalDate, localDayNumber, localMidnightSec } from '../lib/time';

interface HourPoint {
	hour: number;
	requests: number;
	bytes: number;
	errors: number;
}

interface DayPoint {
	date: string;
	requests: number;
	bytes: number;
	errors: number;
}

export async function buildTimeseries(env: Env, daysParam: string | null) {
	let days = parseInt(daysParam ?? '', 10);
	if (!(days >= 1 && days <= 31)) days = 31;

	const nowSec = Math.floor(Date.now() / 1000);
	const todayStart = localMidnightSec(nowSec);
	const yesterdayStart = todayStart - 86400;
	const startDay = todayStart - (days - 1) * 86400;
	const currentHour = Math.floor((nowSec - todayStart) / 3600);
	const todayStartHour = todayStart / 3600;
	const yestStartHour = yesterdayStart / 3600;

	let points: db.AggregatedHourPoint[] = [];
	try {
		points = await db.getAggregatedTimeseries(env.DB, Math.floor(startDay / 3600), Math.floor(nowSec / 3600));
	} catch (e) {
		console.error('timeseries load failed', e);
	}

	const byPath = new Map<
		string,
		{
			path: string;
			today: HourPoint[];
			yesterday: HourPoint[];
			daily: DayPoint[];
			today_requests: number;
			today_bytes: number;
			yesterday_requests: number;
			yesterday_bytes: number;
			month_requests: number;
			month_bytes: number;
		}
	>();

	for (const p of points) {
		let v = byPath.get(p.path);
		if (!v) {
			v = {
				path: p.path,
				today: Array.from({ length: currentHour + 1 }, (_, h) => ({ hour: h, requests: 0, bytes: 0, errors: 0 })),
				yesterday: Array.from({ length: 24 }, (_, h) => ({ hour: h, requests: 0, bytes: 0, errors: 0 })),
				daily: Array.from({ length: days }, (_, i) => ({ date: formatLocalDate(startDay + i * 86400), requests: 0, bytes: 0, errors: 0 })),
				today_requests: 0,
				today_bytes: 0,
				yesterday_requests: 0,
				yesterday_bytes: 0,
				month_requests: 0,
				month_bytes: 0,
			};
			byPath.set(p.path, v);
		}
		const req = Number(p.requests);
		const bytes = Number(p.bytes);
		const errs = Number(p.errors);
		const ts = Number(p.ts_hour);

		if (ts >= todayStartHour && ts <= todayStartHour + currentHour) {
			const h = v.today[ts - todayStartHour];
			h.requests += req;
			h.bytes += bytes;
			h.errors += errs;
			v.today_requests += req;
			v.today_bytes += bytes;
		} else if (ts >= yestStartHour && ts < todayStartHour) {
			const h = v.yesterday[ts - yestStartHour];
			h.requests += req;
			h.bytes += bytes;
			h.errors += errs;
			v.yesterday_requests += req;
			v.yesterday_bytes += bytes;
		}

		const dayIdx = localDayNumber(ts * 3600) - localDayNumber(startDay);
		if (dayIdx >= 0 && dayIdx < days) {
			const d = v.daily[dayIdx];
			d.requests += req;
			d.bytes += bytes;
			d.errors += errs;
			v.month_requests += req;
			v.month_bytes += bytes;
		}
	}

	const paths = [...byPath.values()].sort((a, b) => b.month_requests - a.month_requests || a.path.localeCompare(b.path));
	return { generated_at: nowSec, timezone: TZ_NAME, days, paths };
}
