/**
 * 健康巡检 (Hub alarm 每 20s 调一次 tick)
 *
 * 节点状态: 以落盘的 nodes.last_seen 计失联时长, ≥ 60s 判 down, 重新 hello 即 up。
 * last_seen 记的是节点最近一次真实活动 (ping / 消息) 的时间, 不是"巡检时还算在线"的时间:
 * 半开连接 (节点宕机没发 close) 在在线窗口内仍算 alive, 若按巡检时刻刷新, 失联计时会被推迟一整个窗口。
 * 状态全部落 DO SQLite: DO 会被驱逐后由 alarm 唤醒, 内存里的计时与去重键随时可能清空;
 * 中控发版时节点几秒内重连, last_seen 距今远小于 60s, 不会误报掉线。
 *
 * 故障切换 (每个绑定独立):
 *   - 当前节点失联 ≥ 60s (从最后活动算, 判 down 的同一节拍即可切), 且对其回源主机名的两次探测都失败
 *     → 切到池里第一个 up 且探测通过的节点
 *   - 探测正常时不切, 但每分钟复探, 进程随后真挂了照样能切
 *   - 节点没有回源主机名时无法主动探测 (Worker 不能直连裸 IP), 只凭 WS 失联判断
 *   - 只是 WS 断了但回源探测正常 (中控到节点的链路问题, 用户流量不受影响) → 不切, 记一次事件
 *   - 没有可切的健康节点 → 不动解析 (删空或指向坏节点都更糟), 记一次事件
 * 切回: 首选节点 (pool[0]) 持续 up ≥ 10 分钟、探测通过、距上次切换 ≥ 10 分钟 → 切回。
 * 每 10 分钟对全部绑定做一次一致性巡检, 线下被改动时报一次漂移。
 */
import type { DnsService } from './dns_service';
import { probeTwice, type ProbeResult } from './probe';

const DOWN_AFTER_MS = 60_000;
const CHECKS_PER_TICK = 2; // 免费计划单次调用 50 个子请求, 每个绑定巡检约 3 个, 分摊到多个节拍
const FAILOVER_AFTER_LOST_MS = 60_000;
const FAILBACK_STABLE_MS = 10 * 60_000;
const CHECK_INTERVAL_MS = 10 * 60_000;
const PRUNE_INTERVAL_MS = 3600_000;
const RETRY_INTERVAL_MS = 60_000; // 切换失败 (如 EdgeOne 部署中拒绝修改) 或暂无健康节点时的重试间隔
const MAX_RETRIES = 30;
const RETENTION_MS = 30 * 86400_000;

export interface HealthDeps {
	isAlive(nodeId: string): boolean;
	lastActivityAt(nodeId: string): number; // 最近真实活动时间, 未知为 0
	inMaintenance(nodeId: string): boolean; // 滚动发布正在处理该节点
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
				// 冷启动后没有活动记录的新连接按当前时刻记; last_seen 只前进不回退
				const seen = Math.max(Number(r.last_seen) || 0, this.deps.lastActivityAt(id) || now);
				this.sql.exec('UPDATE nodes SET last_seen = ? WHERE node_id = ?', seen, id);
				if (r.state === 'down') this.markUp(id);
				continue;
			}
			// 发布中的节点断开是预期内的, 不判掉线 (也就不触发故障切换); 发布结束后照常判定
			if (this.deps.inMaintenance(id)) continue;
			if (r.state === 'up' && now - Number(r.last_seen) >= DOWN_AFTER_MS) {
				this.sql.exec("UPDATE nodes SET state = 'down', state_since = ? WHERE node_id = ?", now, id);
				this.deps.event('warning', 'node_down', id, '节点与中控失联超过 1 分钟', true);
			}
		}
	}

	private nodeState(nodeId: string): { state: string; since: number; lastSeen: number } | null {
		const r = this.sql.exec('SELECT state, state_since, last_seen FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		return r ? { state: String(r.state), since: Number(r.state_since), lastSeen: Number(r.last_seen) } : null;
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
			// 失联时长从最后活动算, 不再从判 down 的时刻另起 60s (两段叠加会拖到 2~3 分钟)
			if (b.auto_failover && active.state === 'down' && now - active.lastSeen >= FAILOVER_AFTER_LOST_MS) {
				const roundKey = `notice:${b.id}:${b.active_node}:${active.since}`;
				// 回合状态: 空 = 首次处理; retry:<次数>:<时间> = 上次没切成, 间隔后重试;
				// probe_ok:<时间> = 回源正常未切, 间隔后复探; 其余为终态
				const state = this.getKV(roundKey);
				let attempt = 0;
				let probeOkBefore = false;
				if (state?.startsWith('probe_ok')) {
					const at = Number(state.split(':')[1] ?? 0);
					if (now - at < RETRY_INTERVAL_MS) continue;
					probeOkBefore = true;
				} else if (state) {
					if (!state.startsWith('retry:')) continue;
					const [, n, at] = state.split(':');
					attempt = Number(n);
					if (now - Number(at) < RETRY_INTERVAL_MS) continue;
					if (attempt >= MAX_RETRIES) {
						this.setKV(roundKey, 'gave_up');
						this.deps.event('error', 'failover_stuck', b.hostname, `${b.active_node} 故障后已重试 ${attempt} 次仍未切换成功, 停止自动重试, 请人工处理`, true);
						continue;
					}
				}

				const activeProbe = await this.probe(b.active_node);
				if (activeProbe.ok) {
					this.setKV(roundKey, `probe_ok:${now}`);
					if (!probeOkBefore) this.deps.event('warning', 'node_down', b.hostname, `${b.active_node} 与中控失联, 但回源探测正常, 暂不切换, 每分钟复探`, false);
					continue;
				}

				let target = '';
				for (const candidate of b.pool) {
					if (candidate === b.active_node) continue;
					const st = this.nodeState(candidate);
					if (!st || st.state !== 'up') continue;
					const p = await this.probe(candidate);
					if (p.ok || p.unknown) {
						target = candidate;
						break;
					}
				}
				if (!target) {
					// 暂无健康节点: 解析保持不变, 之后按间隔重试 (备选节点可能稍后恢复); 只在第一次通知
					this.setKV(roundKey, `retry:${attempt + 1}:${now}`);
					if (attempt === 0) {
						this.deps.event('error', 'failover_stuck', b.hostname, `${b.active_node} 已故障 (${activeProbe.error}), 池内没有健康节点, 解析保持不变, 将每分钟重试`, true);
					}
					continue;
				}
				const res = await this.dns.switchTo(b.id, target, 'failover', `${b.active_node} 故障: ${activeProbe.error}`, attempt === 0);
				const anyOK = [res.cf, res.dnspod, res.eo].some((r) => r === 'updated' || r === 'unchanged');
				// 一处都没改成 (如 EdgeOne 部署中拒绝修改): active_node 未变, 按间隔重试; 改成任意一处即终态
				this.setKV(roundKey, anyOK ? 'switched' : `retry:${attempt + 1}:${now}`);
				continue;
			}

			// ---- 切回首选 ----
			const preferred = b.pool[0];
			if (!b.auto_failback || !preferred || preferred === b.active_node) continue;
			const pref = this.nodeState(preferred);
			if (!pref || pref.state !== 'up' || now - pref.since < FAILBACK_STABLE_MS) continue;
			if (now - b.last_switch_at < FAILBACK_STABLE_MS) continue;
			const pp = await this.probe(preferred);
			if (!pp.ok && !pp.unknown) continue;
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
