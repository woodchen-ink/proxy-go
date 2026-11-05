/**
 * Hub: 中控全局唯一的 Durable Object
 *
 * - 持有全部节点的 WebSocket (Hibernation API, 空闲不计时长)
 * - 指令日志 + 逐节点 ack, 断线重连按游标补发
 * - 节点实时快照 (只在内存, 节点每 15s 推一次, DO 冷启动后一个周期内补齐)
 * - 健康巡检与 DNS 故障切换 (alarm 驱动, 见 health.ts)
 *
 * 免费计划额度: DO 时长 13,000 GB-s/天; 多节点快照让 DO 基本常驻 ≈ 11,000 GB-s/天,
 * 快照周期不要再往下调, 节点数增加不影响时长 (时长按墙钟计, 不按消息数)。
 */
import { DurableObject } from 'cloudflare:workers';
import type { Env } from '../env';
import { notifyWecom, type NoticeLevel } from '../lib/wecom';
import { migrate } from './schema';
import { HealthMonitor } from './health';
import { DnsService } from './dns_service';
import { ReleaseStore, type Release } from './release_store';
import { RolloutService } from './rollout';
import { releaseOp } from './release_ops';
import type {
	CommandFrame,
	CommandType,
	HelloMsg,
	NodeMessage,
	NodeSnapshot,
	RpcFrame,
	RpcMethod,
	WelcomeFrame,
} from './protocol';

const ALIVE_WINDOW_MS = 60_000;
const REPLAY_WINDOW_MS = 7 * 86400_000;
const ALARM_INTERVAL_MS = 20_000;

export interface NodeRow {
	node_id: string;
	ipv4: string;
	ipv6: string;
	version: string;
	hostname: string;
	origin_host: string;
	note: string;
	first_seen: number;
	last_seen: number;
	state: 'up' | 'down';
	state_since: number;
}

export interface NodeStatus extends NodeRow {
	online: boolean;
	snapshot_at: number;
	cursor: number;
}

export interface CommandResult {
	seq: number;
	acks: { node_id: string; ok: boolean; message: string; result: unknown }[];
	pending: string[]; // 发出时在线但超时未回 ack 的节点 (之后仍会执行, 重连会补发)
	offline: string[]; // 发出时离线的已知节点 (重连后补发)
}

export interface RpcReply {
	node_id: string;
	ok: boolean;
	data?: unknown;
	error?: string;
}

// HubAPI Worker 侧经 DO stub 调用的方法集合
export interface HubAPI {
	nodeStatuses(): Promise<NodeStatus[]>;
	liveSnapshots(): Promise<Record<string, NodeSnapshot & { received_at: number }>>;
	issueCommand(type: CommandType, payload: Record<string, unknown>, source: string, waitMs?: number): Promise<CommandResult>;
	rpcAll(method: RpcMethod, params: Record<string, unknown>, timeoutMs?: number): Promise<{ replies: RpcReply[]; missing: string[] }>;
	listCommands(limit?: number): Promise<unknown[]>;
	listEvents(limit?: number): Promise<unknown[]>;
	listProbes(): Promise<unknown[]>;
	updateNode(nodeId: string, patch: { origin_host?: string; note?: string }): Promise<void>;
	removeNode(nodeId: string): Promise<void>;
	dnsOp(op: string, args?: Record<string, unknown>): Promise<unknown>;
	releaseOp(op: string, args?: Record<string, unknown>): Promise<unknown>;
	registerRelease(input: Release): Promise<Release>;
	getRelease(version: string): Promise<Release | null>;
}

interface Attachment {
	nodeId: string;
	connectedAt: number;
	ready?: boolean; // hello 处理完 (补发已排队) 才接收实时指令, 否则实时指令会插到补发之前
}

interface Waiter<T> {
	expect: Set<string>;
	got: Map<string, T>;
	resolve: () => void;
}

export class Hub extends DurableObject<Env> implements HubAPI {
	private sql: SqlStorage;
	private snapshots = new Map<string, NodeSnapshot & { received_at: number }>();
	private lastMessageAt = new Map<string, number>();
	private cursors = new Map<string, number>();
	private ackWaiters = new Map<number, Waiter<{ ok: boolean; message: string; result: unknown }>>();
	private rpcWaiters = new Map<string, Waiter<RpcReply>>();
	readonly health: HealthMonitor;
	readonly dns: DnsService;
	readonly releases: ReleaseStore;
	readonly rollouts: RolloutService;

	constructor(ctx: DurableObjectState, env: Env) {
		super(ctx, env);
		this.sql = ctx.storage.sql;
		migrate(this.sql);
		ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('ping', 'pong'));
		this.dns = new DnsService(this.sql, env, (lvl, kind, subject, msg, notify) => this.recordEvent(lvl, kind, subject, msg, notify));
		this.health = new HealthMonitor(this.sql, this.dns, {
			isAlive: (id) => this.isAlive(id),
			inMaintenance: (id) => this.rollouts?.activeNode() === id,
			event: (lvl, kind, subject, msg, notify) => this.recordEvent(lvl, kind, subject, msg, notify),
		});
		this.releases = new ReleaseStore(this.sql, env.CRED_KEY);
		this.rollouts = new RolloutService(this.sql, this.releases, {
			isAlive: (id) => this.isAlive(id),
			rpcNode: (id, method, params, timeoutMs) => this.rpcNode(id, method, params, timeoutMs),
			event: (lvl, kind, subject, msg, notify) => this.recordEvent(lvl, kind, subject, msg, notify),
		});
	}

	// ============ WebSocket 接入 ============

	async fetch(request: Request): Promise<Response> {
		const nodeId = request.headers.get('X-Node-Id')?.trim() ?? '';
		if (request.headers.get('Upgrade') !== 'websocket' || !/^[A-Za-z0-9._:-]{1,64}$/.test(nodeId)) {
			return new Response('expected websocket with X-Node-Id', { status: 400 });
		}
		// 同一节点只保留最新连接: 旧连接多半是对端已断而我们还没感知到
		for (const old of this.ctx.getWebSockets(nodeId)) {
			try {
				old.close(4000, 'replaced by new connection');
			} catch {
				/* already closed */
			}
		}
		const pair = new WebSocketPair();
		const [client, server] = Object.values(pair);
		this.ctx.acceptWebSocket(server, [nodeId]);
		server.serializeAttachment({ nodeId, connectedAt: Date.now() } satisfies Attachment);
		this.lastMessageAt.set(nodeId, Date.now());
		await this.ensureAlarm();
		return new Response(null, { status: 101, webSocket: client });
	}

	async webSocketMessage(ws: WebSocket, raw: string | ArrayBuffer): Promise<void> {
		const att = ws.deserializeAttachment() as Attachment | null;
		if (!att) return;
		const nodeId = att.nodeId;
		this.lastMessageAt.set(nodeId, Date.now());
		if (typeof raw !== 'string') return;

		let msg: NodeMessage;
		try {
			msg = JSON.parse(raw) as NodeMessage;
		} catch {
			return;
		}

		switch (msg.t) {
			case 'hello':
				await this.onHello(ws, nodeId, msg);
				break;
			case 'snapshot':
				this.snapshots.set(nodeId, { ...msg.data, received_at: Date.now() });
				break;
			case 'ack':
				this.onAck(nodeId, msg.seq, msg.ok, msg.message ?? '', msg.result);
				break;
			case 'rpc_res': {
				const w = this.rpcWaiters.get(msg.id);
				if (w && w.expect.has(nodeId)) {
					w.got.set(nodeId, { node_id: nodeId, ok: msg.ok, data: msg.data, error: msg.error });
					if (w.got.size >= w.expect.size) w.resolve();
				}
				break;
			}
		}
	}

	async webSocketClose(ws: WebSocket, code: number, reason: string): Promise<void> {
		const att = ws.deserializeAttachment() as Attachment | null;
		try {
			ws.close(code, reason);
		} catch {
			/* already closed */
		}
		if (att) console.log(`node ${att.nodeId} ws closed: ${code} ${reason}`);
	}

	async webSocketError(ws: WebSocket, err: unknown): Promise<void> {
		const att = ws.deserializeAttachment() as Attachment | null;
		console.error(`node ${att?.nodeId} ws error:`, err);
	}

	private async onHello(ws: WebSocket, nodeId: string, msg: HelloMsg): Promise<void> {
		const now = Date.now();
		const existing = this.sql.exec('SELECT state FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		if (!existing) {
			this.sql.exec(
				`INSERT INTO nodes (node_id, ipv4, ipv6, version, hostname, first_seen, last_seen, state, state_since, hello_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, 'up', ?, ?)`,
				nodeId,
				msg.ipv4 ?? '',
				msg.ipv6 ?? '',
				msg.version ?? '',
				msg.hostname ?? '',
				now,
				now,
				now,
				now,
			);
			this.recordEvent('info', 'node_join', nodeId, `新节点接入 (${msg.version || 'unknown'}, ${msg.ipv4 || msg.ipv6 || '-'})`, true);
		} else {
			this.sql.exec(
				`UPDATE nodes SET ipv4 = COALESCE(NULLIF(?, ''), ipv4), ipv6 = COALESCE(NULLIF(?, ''), ipv6),
				 version = ?, hostname = ?, last_seen = ?, hello_at = ? WHERE node_id = ?`,
				msg.ipv4 ?? '',
				msg.ipv6 ?? '',
				msg.version ?? '',
				msg.hostname ?? '',
				now,
				now,
				nodeId,
			);
			if (existing.state === 'down') this.health.markUp(nodeId);
		}

		const latest = this.latestSeq();
		const cursor = Number(msg.cursor) || 0;
		const att = ws.deserializeAttachment() as Attachment;
		ws.serializeAttachment({ ...att, ready: true } satisfies Attachment);
		this.cursors.set(nodeId, cursor);
		this.send(ws, { t: 'welcome', latest_seq: latest, server_time: now } satisfies WelcomeFrame);

		// 游标为 0: 新节点或丢了游标文件, 无法区分, 只快进不重放
		if (cursor > 0 && cursor < latest) {
			const rows = this.sql
				.exec('SELECT seq, type, payload FROM commands WHERE seq > ? AND created_at > ? ORDER BY seq', cursor, now - REPLAY_WINDOW_MS)
				.toArray();
			for (const r of rows) {
				this.send(ws, { t: 'cmd', seq: Number(r.seq), type: r.type as CommandType, payload: JSON.parse(String(r.payload)) });
			}
		}
	}

	private onAck(nodeId: string, seq: number, ok: boolean, message: string, result: unknown): void {
		this.sql.exec(
			`INSERT OR REPLACE INTO command_acks (seq, node_id, ok, message, result, acked_at) VALUES (?, ?, ?, ?, ?, ?)`,
			seq,
			nodeId,
			ok ? 1 : 0,
			message.slice(0, 500),
			result === undefined ? '' : JSON.stringify(result).slice(0, 4000),
			Date.now(),
		);
		if (seq > (this.cursors.get(nodeId) ?? 0)) this.cursors.set(nodeId, seq);
		const w = this.ackWaiters.get(seq);
		if (w && w.expect.has(nodeId)) {
			w.got.set(nodeId, { ok, message, result });
			if (w.got.size >= w.expect.size) w.resolve();
		}
	}

	private send(ws: WebSocket, frame: CommandFrame | RpcFrame | WelcomeFrame): void {
		try {
			ws.send(JSON.stringify(frame));
		} catch (e) {
			console.error('ws send failed:', e);
		}
	}

	private latestSeq(): number {
		const r = this.sql.exec('SELECT COALESCE(MAX(seq), 0) AS s FROM commands').one();
		return Number(r.s);
	}

	// ============ 在线判定 ============

	private socketsByNode(): Map<string, WebSocket> {
		const out = new Map<string, WebSocket>();
		for (const ws of this.ctx.getWebSockets()) {
			const att = ws.deserializeAttachment() as Attachment | null;
			if (att?.ready && ws.readyState === WebSocket.OPEN) out.set(att.nodeId, ws);
		}
		return out;
	}

	private lastActivity(nodeId: string, ws: WebSocket): number {
		const ping = this.ctx.getWebSocketAutoResponseTimestamp(ws)?.getTime() ?? 0;
		return Math.max(ping, this.lastMessageAt.get(nodeId) ?? 0);
	}

	isAlive(nodeId: string): boolean {
		const ws = this.socketsByNode().get(nodeId);
		if (!ws) return false;
		const last = this.lastActivity(nodeId, ws);
		// DO 刚冷启动时内存里没有消息时间, 以 ping 时间为准; 都没有说明连接是刚建立的
		return last === 0 || Date.now() - last < ALIVE_WINDOW_MS;
	}

	// ============ 对 Worker 暴露的 RPC ============

	async nodeStatuses(): Promise<NodeStatus[]> {
		const rows = this.sql.exec('SELECT * FROM nodes ORDER BY node_id').toArray() as unknown as NodeRow[];
		return rows.map((r) => ({
			...r,
			online: this.isAlive(r.node_id),
			snapshot_at: this.snapshots.get(r.node_id)?.received_at ?? 0,
			cursor: this.cursors.get(r.node_id) ?? 0,
		}));
	}

	async liveSnapshots(): Promise<Record<string, NodeSnapshot & { received_at: number }>> {
		const out: Record<string, NodeSnapshot & { received_at: number }> = {};
		const now = Date.now();
		for (const [id, snap] of this.snapshots) {
			if (now - snap.received_at < ALIVE_WINDOW_MS && this.isAlive(id)) out[id] = snap;
		}
		return out;
	}

	// issueCommand 写指令日志并广播, 最多等 waitMs 收齐在线节点的 ack
	async issueCommand(type: CommandType, payload: Record<string, unknown>, source: string, waitMs = 5000): Promise<CommandResult> {
		const now = Date.now();
		const seq = Number(
			this.sql
				.exec('INSERT INTO commands (type, payload, source, created_at) VALUES (?, ?, ?, ?) RETURNING seq', type, JSON.stringify(payload), source, now)
				.one().seq,
		);

		const sockets = this.socketsByNode();
		const known = this.sql.exec('SELECT node_id FROM nodes').toArray().map((r) => String(r.node_id));
		const online = [...sockets.keys()].filter((id) => this.isAlive(id));
		const offline = known.filter((id) => !online.includes(id));

		const waiter = this.makeWaiter<{ ok: boolean; message: string; result: unknown }>(online);
		this.ackWaiters.set(seq, waiter);
		for (const id of online) this.send(sockets.get(id)!, { t: 'cmd', seq, type, payload });

		await this.waitFor(waiter, waitMs);
		this.ackWaiters.delete(seq);

		return {
			seq,
			acks: [...waiter.got.entries()].map(([node_id, a]) => ({ node_id, ...a })),
			pending: online.filter((id) => !waiter.got.has(id)),
			offline,
		};
	}

	// rpcAll 向全部在线节点发起查询, 等齐或超时后返回已收到的结果
	async rpcAll(method: RpcMethod, params: Record<string, unknown>, timeoutMs = 5000): Promise<{ replies: RpcReply[]; missing: string[] }> {
		const sockets = this.socketsByNode();
		const online = [...sockets.keys()].filter((id) => this.isAlive(id));
		const id = crypto.randomUUID();
		const waiter = this.makeWaiter<RpcReply>(online);
		this.rpcWaiters.set(id, waiter);
		for (const n of online) this.send(sockets.get(n)!, { t: 'rpc', id, method, params });
		await this.waitFor(waiter, timeoutMs);
		this.rpcWaiters.delete(id);
		return { replies: [...waiter.got.values()], missing: online.filter((n) => !waiter.got.has(n)) };
	}

	// rpcNode 只问一个节点 (滚动升级); 不在线或超时返回 null
	async rpcNode(nodeId: string, method: RpcMethod, params: Record<string, unknown>, timeoutMs = 5000): Promise<RpcReply | null> {
		const ws = this.socketsByNode().get(nodeId);
		if (!ws || !this.isAlive(nodeId)) return null;
		const id = crypto.randomUUID();
		const waiter = this.makeWaiter<RpcReply>([nodeId]);
		this.rpcWaiters.set(id, waiter);
		this.send(ws, { t: 'rpc', id, method, params });
		await this.waitFor(waiter, timeoutMs);
		this.rpcWaiters.delete(id);
		return waiter.got.get(nodeId) ?? null;
	}

	async listCommands(limit = 50): Promise<unknown[]> {
		const cmds = this.sql.exec('SELECT * FROM commands ORDER BY seq DESC LIMIT ?', Math.min(limit, 200)).toArray();
		if (cmds.length === 0) return [];
		const minSeq = Number(cmds[cmds.length - 1].seq);
		const acks = this.sql.exec('SELECT seq, node_id, ok, message, acked_at FROM command_acks WHERE seq >= ?', minSeq).toArray();
		return cmds.map((c) => ({
			seq: Number(c.seq),
			type: c.type,
			payload: JSON.parse(String(c.payload)),
			source: c.source,
			created_at: Number(c.created_at),
			acks: acks
				.filter((a) => Number(a.seq) === Number(c.seq))
				.map((a) => ({ node_id: a.node_id, ok: Number(a.ok) === 1, message: a.message, acked_at: Number(a.acked_at) })),
		}));
	}

	async listEvents(limit = 100): Promise<unknown[]> {
		return this.sql.exec('SELECT * FROM events ORDER BY id DESC LIMIT ?', Math.min(limit, 500)).toArray();
	}

	async updateNode(nodeId: string, patch: { origin_host?: string; note?: string }): Promise<void> {
		const row = this.sql.exec('SELECT node_id FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		if (!row) throw new Error('节点不存在');
		if (patch.origin_host !== undefined) {
			const host = patch.origin_host.trim().toLowerCase().replace(/\.$/, '');
			if (host && !/^[a-z0-9.-]+\.[a-z]{2,}$/.test(host)) throw new Error('回源主机名格式不对');
			this.sql.exec('UPDATE nodes SET origin_host = ? WHERE node_id = ?', host, nodeId);
		}
		if (patch.note !== undefined) this.sql.exec('UPDATE nodes SET note = ? WHERE node_id = ?', patch.note.slice(0, 200), nodeId);
	}

	// removeNode 移除已下线节点 (仍在线的拒绝: 它下一次 hello 就会自己回来)
	async removeNode(nodeId: string): Promise<void> {
		if (this.isAlive(nodeId)) throw new Error('节点仍在线, 不能移除');
		const used = this.dns.bindingsUsingNode(nodeId);
		if (used.length) throw new Error(`节点仍在 DNS 绑定 ${used.join(', ')} 的节点池里, 先从池中移除`);
		this.sql.exec('DELETE FROM nodes WHERE node_id = ?', nodeId);
		this.sql.exec('DELETE FROM probes WHERE node_id = ?', nodeId);
		this.snapshots.delete(nodeId);
	}

	async listProbes(): Promise<unknown[]> {
		return this.sql.exec('SELECT * FROM probes').toArray();
	}

	// dnsOp DNS 绑定管理的统一入口 (DnsService 住在 DO 里, 与健康巡检共用状态)
	async dnsOp(op: string, args: Record<string, unknown> = {}): Promise<unknown> {
		const d = this.dns;
		switch (op) {
			case 'listCredentials':
				return d.listCredentials();
			case 'saveCredential':
				return { id: await d.saveCredential(args as never) };
			case 'deleteCredential':
				d.deleteCredential(Number(args.id));
				return { ok: true };
			case 'discover':
				return d.discover(Number(args.id));
			case 'suggestOrigins':
				return d.suggestOrigins(Number(args.credential_id), String(args.zone_id));
			case 'listBindings':
				return d.listBindings();
			case 'saveBinding':
				return { id: d.saveBinding(args as never) };
			case 'deleteBinding':
				d.deleteBinding(Number(args.id));
				return { ok: true };
			case 'check':
				return d.check(Number(args.id));
			case 'switch':
				return d.switchTo(Number(args.id), String(args.node_id), 'switch', `由 ${String(args.user ?? 'admin')} 手动切换`);
			default:
				throw new Error(`unknown dns op: ${op}`);
		}
	}

	// ============ 发布 ============

	async releaseOp(op: string, args: Record<string, unknown> = {}): Promise<unknown> {
		return releaseOp(
			{ sql: this.sql, store: this.releases, rollouts: this.rollouts, kick: () => this.ctx.waitUntil(this.rollouts.tick()) },
			op,
			args,
		);
	}

	async registerRelease(input: Release): Promise<Release> {
		const rel = this.releases.register(input);
		this.recordEvent('info', 'release', rel.version, rel.title || '新版本已登记');
		this.rollouts.onRelease(rel.version);
		this.ctx.waitUntil(this.rollouts.tick());
		await this.ensureAlarm();
		return rel;
	}

	async getRelease(version: string): Promise<Release | null> {
		return this.releases.get(version);
	}

	// ============ 告警巡检 ============

	async alarm(): Promise<void> {
		try {
			await this.health.tick();
		} catch (e) {
			console.error('health tick failed:', e);
		}
		try {
			await this.rollouts.tick();
		} catch (e) {
			console.error('rollout tick failed:', e);
		}
		await this.ctx.storage.setAlarm(Date.now() + ALARM_INTERVAL_MS);
	}

	async ensureAlarm(): Promise<void> {
		const at = await this.ctx.storage.getAlarm();
		if (!at || at < Date.now() - 60_000) await this.ctx.storage.setAlarm(Date.now() + ALARM_INTERVAL_MS);
	}

	// recordEvent 写事件日志; notify 为真时同时推企业微信 (调用方保证同一事件只调一次)
	recordEvent(level: NoticeLevel | 'error', kind: string, subject: string, message: string, notify = false): void {
		this.sql.exec(
			'INSERT INTO events (at, level, kind, subject, message) VALUES (?, ?, ?, ?, ?)',
			Date.now(),
			level,
			kind,
			subject,
			message.slice(0, 1000),
		);
		if (notify) {
			const title = EVENT_TITLES[kind] ?? kind;
			this.ctx.waitUntil(notifyWecom(this.env.WECOM_WEBHOOK_URL, level === 'info' ? 'info' : 'warning', title, [subject ? `**${subject}**` : '', message]));
		}
	}

	// ============ 工具 ============

	private makeWaiter<T>(expect: string[]): Waiter<T> {
		const w: Waiter<T> = { expect: new Set(expect), got: new Map(), resolve: () => {} };
		return w;
	}

	private waitFor<T>(w: Waiter<T>, ms: number): Promise<void> {
		if (w.expect.size === 0) return Promise.resolve();
		return new Promise((resolve) => {
			const timer = setTimeout(resolve, ms);
			w.resolve = () => {
				clearTimeout(timer);
				resolve();
			};
		});
	}
}

const EVENT_TITLES: Record<string, string> = {
	node_join: '新节点接入',
	node_down: '节点掉线',
	node_up: '节点恢复',
	failover: '节点故障, 已自动切换',
	failback: '首选节点恢复, 已切回',
	failover_stuck: '节点故障, 但没有可切换的健康节点',
	switch: '手动切换节点',
	switch_failed: '切换节点失败',
	drift: '解析与配置不一致',
	rollout_failed: '滚动发布失败, 已停止',
	rollout_done: '滚动发布完成',
};
