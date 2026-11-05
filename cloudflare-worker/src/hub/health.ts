/**
 * 健康巡检 (Hub alarm 每 20s 调一次 tick)
 *
 * 节点状态: WS 连续失联 ≥ 60s 判 down, 重新 hello 即 up; DO 冷启动后先给 90s 宽限,
 * 否则中控一发版 (所有 WS 断开重连) 就会刷出一排"节点掉线"。
 *
 * 故障切换 (每个绑定独立):
 *   - 当前节点 down ≥ 60s, 且对其回源主机名的两次探测都失败 → 切到池里第一个 up 且探测通过的节点
 *   - 只是 WS 断了但回源探测正常 (中控到节点的链路问题, 用户流量不受影响) → 不切, 记一次事件
 *   - 没有可切的健康节点 → 不动解析 (删空或指向坏节点都更糟), 记一次事件
 * 切回: 首选节点 (pool[0]) 持续 up ≥ 10 分钟、探测通过、距上次切换 ≥ 10 分钟 → 切回。
 * 每 10 分钟对全部绑定做一次一致性巡检, 线下被改动时报一次漂移。
 */
import type { DnsService } from './dns_service';
import { probeTwice, type ProbeResult } from './probe';

const DOWN_AFTER_MS = 60_000;
const BOOT_GRACE_MS = 90_000;
const FAILOVER_AFTER_DOWN_MS = 60_000;
const FAILBACK_STABLE_MS = 10 * 60_000;
const CHECK_INTERVAL_MS = 10 * 60_000;
const PRUNE_INTERVAL_MS = 3600_000;
const RETENTION_MS = 30 * 86400_000;

export interface HealthDeps {
	isAlive(nodeId: string): boolean;
	event(level: 'info' | 'warning' | 'error', kind: string, subject: string, message: string, notify?: boolean): void;
}

export class HealthMonitor {
	private bootAt = Date.now();
	private notAliveSince = new Map<string, number>();
	private lastCheckAt = 0;
	private lastPruneAt = 0;
	private noticed = new Set<string>(); // 每个"故障回合"只报一次的去重键

	constructor(
		private sql: SqlStorage,
		private dns: DnsService,
		private deps: HealthDeps,
	) {}

	async tick(): Promise<void> {
		this.updateNodeStates();
		await this.evaluateBindings();
		const now = Date.now();
		if (now - this.lastCheckAt >= CHECK_INTERVAL_MS) {
			this.lastCheckAt = now;
			for (const b of this.dns.listBindings()) {
				if (b.enabled) await this.dns.check(b.id, true).catch((e) => console.error('binding check failed', b.hostname, e));
			}
		}
		if (now - this.lastPruneAt >= PRUNE_INTERVAL_MS) {
			this.lastPruneAt = now;
			const cutoff = now - RETENTION_MS;
			this.sql.exec('DELETE FROM command_acks WHERE seq IN (SELECT seq FROM commands WHERE created_at < ?)', cutoff);
			this.sql.exec('DELETE FROM commands WHERE created_at < ?', cutoff);
			this.sql.exec('DELETE FROM events WHERE at < ?', cutoff);
		}
	}

	markUp(nodeId: string): void {
		const row = this.sql.exec('SELECT state, state_since FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		if (!row || row.state === 'up') return;
		const now = Date.now();
		this.sql.exec("UPDATE nodes SET state = 'up', state_since = ? WHERE node_id = ?", now, nodeId);
		this.notAliveSince.delete(nodeId);
		const downFor = Math.round((now - Number(row.state_since)) / 60000);
		this.deps.event('info', 'node_up', nodeId, `节点重新连上中控 (离线约 ${downFor} 分钟)`, true);
	}

	private updateNodeStates(): void {
		const now = Date.now();
		for (const r of this.sql.exec('SELECT node_id, state FROM nodes').toArray()) {
			const id = String(r.node_id);
			if (this.deps.isAlive(id)) {
				this.notAliveSince.delete(id);
				this.sql.exec('UPDATE nodes SET last_seen = ? WHERE node_id = ?', now, id);
				if (r.state === 'down') this.markUp(id);
				continue;
			}
			if (!this.notAliveSince.has(id)) this.notAliveSince.set(id, now);
			const silentFor = now - this.notAliveSince.get(id)!;
			if (r.state === 'up' && silentFor >= DOWN_AFTER_MS && now - this.bootAt >= BOOT_GRACE_MS) {
				this.sql.exec("UPDATE nodes SET state = 'down', state_since = ? WHERE node_id = ?", now, id);
				this.deps.event('warning', 'node_down', id, '节点与中控失联超过 1 分钟', true);
			}
		}
	}

	private nodeState(nodeId: string): { state: string; since: number } | null {
		const r = this.sql.exec('SELECT state, state_since FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		return r ? { state: String(r.state), since: Number(r.state_since) } : null;
	}

	private async probe(nodeId: string): Promise<ProbeResult> {
		const res = await probeTwice(this.dns.originOf(nodeId));
		const now = Date.now();
		const prev = this.sql.exec('SELECT ok, fails, ok_since FROM probes WHERE node_id = ?', nodeId).toArray()[0];
		const fails = res.ok ? 0 : Number(prev?.fails ?? 0) + 1;
		const okSince = res.ok ? (prev && Number(prev.ok) === 1 ? Number(prev.ok_since) : now) : 0;
		this.sql.exec(
			'INSERT OR REPLACE INTO probes (node_id, ok, fails, checked_at, ok_since, error) VALUES (?, ?, ?, ?, ?, ?)',
			nodeId,
			res.ok ? 1 : 0,
			fails,
			now,
			okSince,
			res.error,
		);
		return res;
	}

	private async evaluateBindings(): Promise<void> {
		const now = Date.now();
		for (const b of this.dns.listBindings()) {
			if (!b.enabled || !b.active_node) continue;
			const active = this.nodeState(b.active_node);
			if (!active) continue;

			// ---- 故障切换 ----
			if (b.auto_failover && active.state === 'down' && now - active.since >= FAILOVER_AFTER_DOWN_MS) {
				const roundKey = `${b.id}:${b.active_node}:${active.since}`;
				if (this.noticed.has(roundKey)) continue;

				const activeProbe = await this.probe(b.active_node);
				if (activeProbe.ok) {
					this.noticed.add(roundKey);
					this.deps.event('warning', 'node_down', b.hostname, `${b.active_node} 与中控失联, 但回源探测正常, 未切换`, false);
					continue;
				}

				let target = '';
				for (const candidate of b.pool) {
					if (candidate === b.active_node) continue;
					const st = this.nodeState(candidate);
					if (!st || st.state !== 'up') continue;
					if ((await this.probe(candidate)).ok) {
						target = candidate;
						break;
					}
				}
				this.noticed.add(roundKey);
				if (!target) {
					this.deps.event('error', 'failover_stuck', b.hostname, `${b.active_node} 已故障 (${activeProbe.error}), 池内没有健康节点, 解析保持不变`, true);
					continue;
				}
				await this.dns.switchTo(b.id, target, 'failover', `${b.active_node} 故障: ${activeProbe.error}`);
				continue;
			}

			// ---- 切回首选 ----
			const preferred = b.pool[0];
			if (!b.auto_failback || !preferred || preferred === b.active_node) continue;
			const pref = this.nodeState(preferred);
			if (!pref || pref.state !== 'up' || now - pref.since < FAILBACK_STABLE_MS) continue;
			if (now - b.last_switch_at < FAILBACK_STABLE_MS) continue;
			if (!(await this.probe(preferred)).ok) continue;
			await this.dns.switchTo(b.id, preferred, 'failback', '首选节点已稳定恢复');
		}
	}
}
