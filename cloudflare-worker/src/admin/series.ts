/**
 * 路径访问趋势 (小时桶) 与 Referer 天级序列的视图组装
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

export async function buildRefererDaily(env: Env, daysParam: string | null, topParam: string | null) {
	let days = parseInt(daysParam ?? '', 10);
	if (!(days >= 1 && days <= 30)) days = 30;
	let top = parseInt(topParam ?? '', 10);
	if (!(top >= 1 && top <= 200)) top = 50;

	const nowSec = Math.floor(Date.now() / 1000);
	const maxDate = localDayNumber(nowSec);
	const minDate = maxDate - (days - 1);
	const startDay = localMidnightSec(nowSec) - (days - 1) * 86400;

	let points: db.AggregatedRefererDayPoint[] = [];
	try {
		points = await db.getAggregatedRefererDaily(env.DB, minDate, maxDate);
	} catch (e) {
		console.error('referer daily load failed', e);
	}

	const byHost = new Map<
		string,
		{ host: string; daily: DayPoint[]; total_requests: number; total_bytes: number; total_errors: number; first_seen_date: string; last_seen_date: string }
	>();
	for (const p of points) {
		let v = byHost.get(p.host);
		if (!v) {
			v = {
				host: p.host,
				daily: Array.from({ length: days }, (_, i) => ({ date: formatLocalDate(startDay + i * 86400), requests: 0, bytes: 0, errors: 0 })),
				total_requests: 0,
				total_bytes: 0,
				total_errors: 0,
				first_seen_date: '',
				last_seen_date: '',
			};
			byHost.set(p.host, v);
		}
		const idx = Number(p.ts_date) - minDate;
		if (idx < 0 || idx >= days) continue;
		const d = v.daily[idx];
		d.requests += Number(p.requests);
		d.bytes += Number(p.bytes);
		d.errors += Number(p.errors);
		v.total_requests += Number(p.requests);
		v.total_bytes += Number(p.bytes);
		v.total_errors += Number(p.errors);
		if (!v.first_seen_date || d.date < v.first_seen_date) v.first_seen_date = d.date;
		if (d.date > v.last_seen_date) v.last_seen_date = d.date;
	}

	const hosts = [...byHost.values()].sort((a, b) => b.total_requests - a.total_requests || a.host.localeCompare(b.host)).slice(0, top);
	return { generated_at: nowSec, timezone: TZ_NAME, days, hosts };
}
