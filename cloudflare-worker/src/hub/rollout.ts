/**
 * 滚动发布 (Hub alarm 每 20s 推进一次, 发起时立即推进一次)
 *
 * 一次只升一个节点, 前一个确认恢复后再动下一个; 任一节点失败即整批停止并通知, 已升级的不回滚。
 *   - binary:    RPC `upgrade` 让节点在容器内下载新程序并 exec; 节点以新版本号重连 → 完成
 *   - container: 调 Dokploy 重新部署该节点的应用; Dokploy 报部署完成且节点在那之后重新 hello → 完成
 * 完成前还要求回源探测通过 (节点有回源主机名时)。处理中的节点由 activeNode 告知健康巡检,
 * 期间不判掉线、不触发故障切换 (重建容器拉镜像可能超过 60s)。
 * 状态全部落 DO SQLite, DO 被驱逐后由 alarm 接着推进; 只有 RPC 失败回执放内存, 丢了按超时兜底。
 */
import { latestDeployment, redeploy, type DokployAppType } from '../deploy/dokploy';
import { probeTwice } from './probe';
import type { RpcReply } from './hub';
import type { ReleaseStore } from './release_store';

export type RolloutKind = 'binary' | 'container';
type StepStatus = 'pending' | 'running' | 'done' | 'skipped' | 'failed';

export interface RolloutStep {
	node_id: string;
	status: StepStatus;
	started_at: number;
	finished_at: number;
	message: string;
}

export interface Rollout {
	id: number;
	kind: RolloutKind;
	version: string;
	status: 'running' | 'done' | 'failed' | 'cancelled';
	steps: RolloutStep[];
	source: string;
	message: string;
	created_at: number;
	updated_at: number;
}

export interface RolloutDeps {
	isAlive(nodeId: string): boolean;
	rpcNode(nodeId: string, method: 'upgrade', params: Record<string, unknown>, timeoutMs: number): Promise<RpcReply | null>;
	event(level: 'info' | 'warning' | 'error', kind: string, subject: string, message: string, notify?: boolean): void;
}

const SETTLE_MS = 30_000; // 上一个节点恢复后观察一会儿再动下一个
const CLOCK_SKEW_MS = 30_000; // Dokploy 主机与 Worker 的时钟偏差容忍
const TIMEOUT_MS: Record<RolloutKind, number> = { binary: 5 * 60_000, container: 10 * 60_000 };
const UPGRADE_RPC_TIMEOUT_MS = 4 * 60_000;
const AUTO_KEY = 'release_auto_binary';

interface NodeRow {
	node_id: string;
	version: string;
	origin_host: string;
	hello_at: number;
	dokploy_server_id: number;
	dokploy_app_id: string;
	dokploy_app_type: DokployAppType;
}

export class RolloutService {
	private rpcErrors = new Map<string, string>(); // `${rolloutId}:${nodeId}` → 节点回的失败原因
	private ticking = false;

	constructor(
		private sql: SqlStorage,
		private store: ReleaseStore,
		private deps: RolloutDeps,
	) {}

	// ---------- 查询 / 设置 ----------

	list(limit = 10): Rollout[] {
		return this.sql.exec('SELECT * FROM rollouts ORDER BY id DESC LIMIT ?', limit).toArray().map(toRollout);
	}

	autoBinary(): boolean {
		return this.sql.exec('SELECT value FROM kv WHERE key = ?', AUTO_KEY).toArray()[0]?.value === '1';
	}

	setAutoBinary(on: boolean): void {
		this.sql.exec('INSERT OR REPLACE INTO kv (key, value, updated_at) VALUES (?, ?, ?)', AUTO_KEY, on ? '1' : '0', Date.now());
	}

	// activeNode 正在处理的节点 (健康巡检据此暂停掉线判定)
	activeNode(): string | null {
		return this.running()?.steps.find((s) => s.status === 'running')?.node_id ?? null;
	}

	// ---------- 发起 / 取消 ----------

	start(kind: RolloutKind, version: string, nodeIds: string[], source: string): number {
		if (kind !== 'binary' && kind !== 'container') throw new Error('kind 只能是 binary / container');
		if (this.running()) throw new Error('已有发布在进行中, 等它结束或先取消');
		if (kind === 'binary' && !this.store.get(version)) throw new Error(`版本 ${version} 不存在`);
		const known = new Set(this.sql.exec('SELECT node_id FROM nodes').toArray().map((r) => String(r.node_id)));
		const ids = [...new Set(nodeIds)].filter((id) => known.has(id));
		if (ids.length === 0) throw new Error('没有选中任何已知节点');
		const steps: RolloutStep[] = ids.map((node_id) => ({ node_id, status: 'pending', started_at: 0, finished_at: 0, message: '' }));
		const now = Date.now();
		const id = Number(
			this.sql
				.exec(
					`INSERT INTO rollouts (kind, version, status, steps, source, created_at, updated_at) VALUES (?, ?, 'running', ?, ?, ?, ?) RETURNING id`,
					kind,
					kind === 'binary' ? version : '',
					JSON.stringify(steps),
					source,
					now,
					now,
				)
				.one().id,
		);
		const what = kind === 'binary' ? `程序升级到 ${version}` : '重建容器';
		this.deps.event('info', 'rollout_start', `#${id}`, `${source} 发起${what}, 共 ${ids.length} 个节点: ${ids.join(', ')}`);
		return id;
	}

	cancel(id: number, user: string): void {
		const r = this.get(id);
		if (!r || r.status !== 'running') throw new Error('该发布不在进行中');
		r.status = 'cancelled';
		r.message = `由 ${user} 取消`;
		this.save(r);
		this.deps.event('warning', 'rollout_cancelled', `#${id}`, r.message);
	}

	// onRelease CI 登记新版本后按设置自动发起程序升级
	onRelease(version: string): void {
		if (!this.autoBinary()) return;
		if (this.running()) {
			this.deps.event('warning', 'rollout_skipped', version, '已有发布在进行中, 新版本未自动发布');
			return;
		}
		const ids = this.sql.exec('SELECT node_id FROM nodes ORDER BY node_id').toArray().map((r) => String(r.node_id));
		if (ids.length) this.start('binary', version, ids, 'auto');
	}

	// ---------- 推进 ----------

	async tick(): Promise<void> {
		if (this.ticking) return; // 推进中有 await (探测 / Dokploy), 防止 alarm 与手动发起并发改同一行
		this.ticking = true;
		try {
			const r = this.running();
			if (r) await this.advance(r);
		} finally {
			this.ticking = false;
		}
	}

	private async advance(r: Rollout): Promise<void> {
		const step = r.steps.find((s) => s.status === 'pending' || s.status === 'running');
		if (!step) return this.finish(r);
		const now = Date.now();
		if (step.status === 'pending') {
			const lastDone = Math.max(0, ...r.steps.filter((s) => s.status === 'done').map((s) => s.finished_at));
			if (now - lastDone < SETTLE_MS) return;
			await this.startStep(r, step);
		} else {
			await this.checkStep(r, step);
		}
		if (step.status === 'failed') {
			r.status = 'failed';
			r.message = `${step.node_id}: ${step.message}`;
			this.deps.event('error', 'rollout_failed', `#${r.id}`, `${r.message}; 其余节点未处理`, true);
		}
		this.save(r);
		// 跳过的节点不必等, 直接看下一个
		if (step.status === 'skipped' && r.status === 'running') await this.advance(r);
	}

	private async startStep(r: Rollout, step: RolloutStep): Promise<void> {
		const node = this.node(step.node_id);
		const now = Date.now();
		if (!node) return this.end(step, 'skipped', '节点已移除');
		if (r.kind === 'binary') {
			if (!this.deps.isAlive(node.node_id)) return this.end(step, 'skipped', '离线, 未升级');
			if (node.version === r.version) return this.end(step, 'skipped', '已是目标版本');
			const rel = this.store.get(r.version);
			if (!rel) return this.end(step, 'failed', `版本 ${r.version} 已不存在`);
			const assets = Object.fromEntries(Object.entries(rel.assets).map(([arch, a]) => [arch, { sha256: a.sha256, size: a.size }]));
			step.status = 'running';
			step.started_at = now;
			step.message = `从 ${node.version || '未知版本'} 升级`;
			const key = `${r.id}:${node.node_id}`;
			// 不等结果: 下载可能要一两分钟, 由后续节拍按版本号判断; 节点明确失败才记下来
			void this.deps
				.rpcNode(node.node_id, 'upgrade', { version: rel.version, build_time: rel.build_time, assets }, UPGRADE_RPC_TIMEOUT_MS)
				.then((reply) => {
					if (reply && !reply.ok) this.rpcErrors.set(key, reply.error || '节点拒绝升级');
				});
			return;
		}
		if (!node.dokploy_server_id || !node.dokploy_app_id) return this.end(step, 'failed', '未登记 Dokploy 应用');
		// 先落盘 running 再调 Dokploy: 调用后 DO 被驱逐也不会在下一拍重复触发部署
		step.status = 'running';
		step.started_at = now;
		step.message = '正在触发 Dokploy 重新部署';
		this.save(r);
		try {
			const server = await this.store.server(node.dokploy_server_id);
			await redeploy(server, node.dokploy_app_type, node.dokploy_app_id, `proxy-go 中控滚动发布 #${r.id}`);
		} catch (e) {
			return this.end(step, 'failed', e instanceof Error ? e.message : String(e));
		}
		step.message = '已触发 Dokploy 重新部署, 等待部署完成';
	}

	private async checkStep(r: Rollout, step: RolloutStep): Promise<void> {
		const key = `${r.id}:${step.node_id}`;
		const rpcErr = this.rpcErrors.get(key);
		if (rpcErr) {
			this.rpcErrors.delete(key);
			return this.end(step, 'failed', rpcErr);
		}
		const node = this.node(step.node_id);
		if (!node) return this.end(step, 'failed', '节点已移除');
		let since = step.started_at;
		if (r.kind === 'container') {
			const deployedAt = await this.deploymentDone(node, step);
			if (deployedAt === null) return this.timeoutCheck(r, step, step.message);
			if (deployedAt < 0) return; // 已判失败
			since = Math.max(since, deployedAt - CLOCK_SKEW_MS);
		}
		// 必须是触发之后 (容器: 部署完成之后) 的重连, 排除中控链路抖动造成的重连
		const reconnected = this.deps.isAlive(node.node_id) && node.hello_at > since;
		const versionOK = r.kind !== 'binary' || node.version === r.version;
		if (reconnected && versionOK) {
			const probe = await probeTwice(node.origin_host);
			if (probe.ok || probe.unknown) {
				return this.end(step, 'done', r.kind === 'binary' ? `已运行 ${node.version}` : `已重连 (${node.version || '-'})`);
			}
			step.message = `已重连, 回源探测失败: ${probe.error}`;
		}
		this.timeoutCheck(r, step, !reconnected ? '节点未重连' : !versionOK ? `仍在运行 ${node.version}` : step.message);
	}

	private timeoutCheck(r: Rollout, step: RolloutStep, why: string): void {
		if (Date.now() - step.started_at > TIMEOUT_MS[r.kind]) this.end(step, 'failed', `超时: ${why}`);
	}

	// deploymentDone 查 Dokploy 本次部署: 完成返回完成时间 (旧版无该字段时返回触发时间), 未完成 null, 失败 -1
	private async deploymentDone(node: NodeRow, step: RolloutStep): Promise<number | null> {
		try {
			const d = await latestDeployment(await this.store.server(node.dokploy_server_id), node.dokploy_app_type, node.dokploy_app_id);
			if (!d || d.created_at < step.started_at - CLOCK_SKEW_MS) {
				step.message = '等待 Dokploy 开始部署';
				return null;
			}
			if (d.status === 'error') {
				this.end(step, 'failed', `Dokploy 部署失败: ${d.error || '详见 Dokploy 部署日志'}`);
				return -1;
			}
			if (d.status !== 'done') {
				step.message = 'Dokploy 部署中';
				return null;
			}
			step.message = 'Dokploy 部署完成, 等待节点重连';
			return d.finished_at || step.started_at;
		} catch (e) {
			step.message = `查询 Dokploy 部署状态失败: ${e instanceof Error ? e.message : String(e)}`;
			return null;
		}
	}

	private finish(r: Rollout): void {
		r.status = 'done';
		const done = r.steps.filter((s) => s.status === 'done').length;
		const skipped = r.steps.filter((s) => s.status === 'skipped');
		r.message = `完成 ${done} 个` + (skipped.length ? `, 跳过 ${skipped.map((s) => `${s.node_id} (${s.message})`).join(', ')}` : '');
		this.save(r);
		const what = r.kind === 'binary' ? `程序已升级到 ${r.version}` : '容器已重建';
		this.deps.event('info', 'rollout_done', `#${r.id}`, `${what}: ${r.message}`, done > 0);
	}

	// ---------- 工具 ----------

	private end(step: RolloutStep, status: StepStatus, message: string): void {
		step.status = status;
		step.message = message;
		step.finished_at = Date.now();
	}

	private running(): Rollout | null {
		const row = this.sql.exec("SELECT * FROM rollouts WHERE status = 'running' ORDER BY id LIMIT 1").toArray()[0];
		return row ? toRollout(row) : null;
	}

	private get(id: number): Rollout | null {
		const row = this.sql.exec('SELECT * FROM rollouts WHERE id = ?', id).toArray()[0];
		return row ? toRollout(row) : null;
	}

	// save 只在库里仍为 running 时写 (取消与推进交错时, 以取消为准)
	private save(r: Rollout): void {
		const current = this.get(r.id);
		if (!current || current.status !== 'running') return;
		this.sql.exec(
			'UPDATE rollouts SET status = ?, steps = ?, message = ?, updated_at = ? WHERE id = ?',
			r.status,
			JSON.stringify(r.steps),
			r.message,
			Date.now(),
			r.id,
		);
	}

	private node(id: string): NodeRow | null {
		const r = this.sql
			.exec('SELECT node_id, version, origin_host, hello_at, dokploy_server_id, dokploy_app_id, dokploy_app_type FROM nodes WHERE node_id = ?', id)
			.toArray()[0];
		return r ? (r as unknown as NodeRow) : null;
	}
}

function toRollout(r: Record<string, SqlStorageValue>): Rollout {
	return {
		id: Number(r.id),
		kind: String(r.kind) as RolloutKind,
		version: String(r.version),
		status: String(r.status) as Rollout['status'],
		steps: JSON.parse(String(r.steps)) as RolloutStep[],
		source: String(r.source),
		message: String(r.message),
		created_at: Number(r.created_at),
		updated_at: Number(r.updated_at),
	};
}
