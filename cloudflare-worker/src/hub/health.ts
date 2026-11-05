/**
 * 健康巡检 (Hub alarm 每 20s 调一次 tick)
 *
 * 节点状态: 以落盘的 nodes.last_seen 计失联时长, ≥ 60s 判 down, 重新 hello 即 up。
 * 状态全部落 DO SQLite: DO 会被驱逐后由 alarm 唤醒, 内存里的计时与去重键随时可能清空;
 * 中控发版时节点几秒内重连, last_seen 距今远小于 60s, 不会误报掉线。
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
const CHECKS_PER_TICK = 2; // 免费计划单次调用 50 个子请求, 每个绑定巡检约 3 个, 分摊到多个节拍
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

	constructor(
		private sql: SqlStorage,
		private dns: DnsService,
		private deps: HealthDeps,
	) {}

	async tick(): Promise<void> {
		this.updateNodeStates();
		await this.evaluateBindings();
		const now = Date.now();
		const due = this.dns
			.listBindings()
			.filter((b) => b.enabled && now - (b.last_check?.at ?? 0) >= CHECK_INTERVAL_MS)
			.slice(0, CHECKS_PER_TICK);
		for (const b of due) {
			await this.dns.check(b.id, true).catch((e) => console.error('binding check failed', b.hostname, e));
		}
		if (now - Number(this.getKV('last_prune_at') ?? 0) >= PRUNE_INTERVAL_MS) {
			this.setKV('last_prune_at', String(now));
			this.sql.exec("DELETE FROM kv WHERE key LIKE 'notice:%' AND updated_at < ?", now - RETENTION_MS);
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
		this.sql.exec("UPDATE nodes SET state = 'up', state_since = ?, last_seen = ? WHERE node_id = ?", now, now, nodeId);
		const downFor = Math.round((now - Number(row.state_since)) / 60000);
		this.deps.event('info', 'node_up', nodeId, `节点重新连上中控 (离线约 ${downFor} 分钟)`, true);
	}

	private updateNodeStates(): void {
		const now = Date.now();
		for (const r of this.sql.exec('SELECT node_id, state, last_seen FROM nodes').toArray()) {
			const id = String(r.node_id);
			if (this.deps.isAlive(id)) {
				this.sql.exec('UPDATE nodes SET last_seen = ? WHERE node_id = ?', now, id);
				if (r.state === 'down') this.markUp(id);
				continue;
			}
			if (r.state === 'up' && now - Number(r.last_seen) >= DOWN_AFTER_MS) {
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
				const roundKey = `notice:${b.id}:${b.active_node}:${active.since}`;
				if (this.getKV(roundKey)) continue;

				const activeProbe = await this.probe(b.active_node);
				if (activeProbe.ok) {
					this.setKV(roundKey, 'probe_ok');
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
				this.setKV(roundKey, target ? 'switched' : 'stuck');
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

	private getKV(key: string): string | null {
		const r = this.sql.exec('SELECT value FROM kv WHERE key = ?', key).toArray()[0];
		return r ? String(r.value) : null;
	}

	private setKV(key: string, value: string): void {
		this.sql.exec('INSERT OR REPLACE INTO kv (key, value, updated_at) VALUES (?, ?, ?)', key, value, Date.now());
	}
}
