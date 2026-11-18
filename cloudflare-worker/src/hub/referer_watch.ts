/**
 * 引用来源设置与"新来源"告警 (Hub DO 内, 状态存 DO SQLite kv)
 *
 * - 自有域名: 手动名单 + DNS 域名 (访问域名与 DNSPod 主域) 自动带出, 后缀匹配; 自有来源不告警、界面默认折叠
 * - 新来源告警: 每小时查一次 D1 近两天按 host 合计 (只扫近两天的行), 达到阈值且此前从未出现
 *   (referer_daily 里最早一天在近两天内, 且长期汇总表里没有) 的外部 host 推一条企业微信, 同一 host 只推一次
 */
import type { Env } from '../env';
import { hostMatches, normalizeRefererHost } from '../lib/referer';
import { localDayNumber } from '../lib/time';

const KEY_OWN = 'referer:own_hosts';
const KEY_THRESHOLD = 'referer:alert_threshold';
const KEY_LAST_TICK = 'referer:last_tick';
const ALERTED_PREFIX = 'referer:alerted:';
const TICK_INTERVAL_MS = 60 * 60_000;
const DEFAULT_THRESHOLD = 200;
const MAX_OWN = 200;
const MAX_PER_NOTICE = 10;

export interface RefererSettings {
	own_hosts: string[]; // 手动登记
	auto_own_hosts: string[]; // 由 DNS 域名带出, 只读
	alert_threshold: number; // 0 = 关闭告警
}

type Notify = (subject: string, message: string) => void;

export class RefererWatch {
	constructor(
		private sql: SqlStorage,
		private env: Env,
		private notify: Notify,
	) {}

	settings(): RefererSettings {
		const own = parseList(this.getKV(KEY_OWN));
		const raw = this.getKV(KEY_THRESHOLD);
		const threshold = raw === null ? DEFAULT_THRESHOLD : Math.max(0, parseInt(raw, 10) || 0);
		return { own_hosts: own, auto_own_hosts: this.autoOwn(), alert_threshold: threshold };
	}

	update(patch: { own_hosts?: unknown; alert_threshold?: unknown; add_own?: unknown; remove_own?: unknown }): RefererSettings {
		let own = parseList(this.getKV(KEY_OWN));
		if (Array.isArray(patch.own_hosts)) own = patch.own_hosts.map(normalizeRefererHost);
		if (patch.add_own !== undefined) own.push(normalizeRefererHost(patch.add_own));
		if (patch.remove_own !== undefined) {
			const h = normalizeRefererHost(patch.remove_own);
			own = own.filter((x) => x !== h);
		}
		own = [...new Set(own.filter((h) => /^[a-z0-9.-]+$/.test(h) && h.includes('.')))].sort().slice(0, MAX_OWN);
		this.setKV(KEY_OWN, JSON.stringify(own));
		if (patch.alert_threshold !== undefined) {
			const n = Math.max(0, Math.min(1_000_000, Math.floor(Number(patch.alert_threshold) || 0)));
			this.setKV(KEY_THRESHOLD, String(n));
		}
		return this.settings();
	}

	// tick 由 alarm 调用, 自身节流为每小时一次
	async tick(now = Date.now()): Promise<void> {
		const last = Number(this.getKV(KEY_LAST_TICK) ?? 0);
		if (now - last < TICK_INTERVAL_MS) return;
		this.setKV(KEY_LAST_TICK, String(now));
		// 去重记录留 3 天即可: 之后该 host 在 referer_daily 里已有更早的天, 不会再被当成新来源
		this.sql.exec('DELETE FROM kv WHERE key LIKE ? AND updated_at < ?', `${ALERTED_PREFIX}%`, now - 3 * 86400_000);

		const { own_hosts, auto_own_hosts, alert_threshold } = this.settings();
		if (alert_threshold <= 0) return;
		const today = localDayNumber(Math.floor(now / 1000));
		const since = today - 1;

		const cand = await this.env.DB.prepare(
			`SELECT host, SUM(requests) AS requests FROM referer_daily
			  WHERE ts_date >= ?1 GROUP BY host HAVING SUM(requests) >= ?2 ORDER BY requests DESC LIMIT 50`,
		)
			.bind(since, alert_threshold)
			.all<{ host: string; requests: number }>();
		const own = [...own_hosts, ...auto_own_hosts];
		const fresh = (cand.results ?? []).filter((r) => !hostMatches(r.host, own) && this.getKV(ALERTED_PREFIX + r.host) === null);
		if (fresh.length === 0) return;

		// 此前出现过的 host 不算新: 35 天窗口内更早的天, 或长期汇总里有记录
		const hosts = fresh.map((r) => r.host);
		const marks = hosts.map((_, i) => `?${i + 2}`).join(',');
		const seen = new Set<string>();
		const older = await this.env.DB.prepare(`SELECT DISTINCT host FROM referer_daily WHERE ts_date < ?1 AND host IN (${marks})`)
			.bind(since, ...hosts)
			.all<{ host: string }>();
		for (const r of older.results ?? []) seen.add(r.host);
		try {
			const arch = await this.env.DB.prepare(`SELECT host FROM referer_hosts WHERE host IN (${hosts.map((_, i) => `?${i + 1}`).join(',')})`)
				.bind(...hosts)
				.all<{ host: string }>();
			for (const r of arch.results ?? []) seen.add(r.host);
		} catch {
			/* 汇总表未建 */
		}

		const alerts = fresh.filter((r) => !seen.has(r.host));
		for (const r of fresh) this.setKV(ALERTED_PREFIX + r.host, String(now)); // 老来源也记上, 下次不再查
		if (alerts.length === 0) return;
		const lines = alerts.slice(0, MAX_PER_NOTICE).map((r) => `${r.host}  近两天 ${Number(r.requests).toLocaleString('en-US')} 次`);
		if (alerts.length > MAX_PER_NOTICE) lines.push(`另有 ${alerts.length - MAX_PER_NOTICE} 个`);
		this.notify(`${alerts.length} 个`, `首次出现且请求量达到 ${alert_threshold} 次的引用来源:\n${lines.join('\n')}`);
	}

	private autoOwn(): string[] {
		const out = new Set<string>();
		try {
			for (const r of this.sql.exec('SELECT hostname, dnspod_domain FROM dns_domains').toArray()) {
				for (const v of [r.hostname, r.dnspod_domain]) {
					const h = normalizeRefererHost(v);
					if (h.includes('.')) out.add(h);
				}
			}
		} catch {
			/* 表未建 */
		}
		return [...out].sort();
	}

	private getKV(key: string): string | null {
		const r = this.sql.exec('SELECT value FROM kv WHERE key = ?', key).toArray()[0];
		return r ? String(r.value) : null;
	}

	private setKV(key: string, value: string): void {
		this.sql.exec('INSERT OR REPLACE INTO kv (key, value, updated_at) VALUES (?, ?, ?)', key, value, Date.now());
	}
}

function parseList(raw: string | null): string[] {
	if (!raw) return [];
	try {
		const v = JSON.parse(raw);
		return Array.isArray(v) ? v.map(String) : [];
	} catch {
		return [];
	}
}
