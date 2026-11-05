/**
 * 发布管理的统一入口 (管理 API → Hub.releaseOp), 与 dnsOp 同一模式, 免得 HubAPI 方法表无限变长
 */
import type { DokployAppType } from '../deploy/dokploy';
import type { ReleaseStore } from './release_store';
import type { RolloutKind, RolloutService } from './rollout';

export interface ReleaseOpContext {
	sql: SqlStorage;
	store: ReleaseStore;
	rollouts: RolloutService;
	kick(): void; // 发起后立即推进一步, 不等下一个 alarm
}

export async function releaseOp(c: ReleaseOpContext, op: string, args: Record<string, unknown>): Promise<unknown> {
	switch (op) {
		case 'overview':
			return {
				releases: c.store.list(20),
				rollouts: c.rollouts.list(10),
				auto_binary: c.rollouts.autoBinary(),
				dokploy_servers: c.store.listServers(),
			};
		case 'setAuto':
			c.rollouts.setAutoBinary(Boolean(args.auto_binary));
			return { ok: true };
		case 'start': {
			const nodes = Array.isArray(args.nodes) ? args.nodes.map(String) : [];
			const id = c.rollouts.start(args.kind as RolloutKind, String(args.version ?? ''), nodes, String(args.user ?? 'admin'));
			c.kick();
			return { id };
		}
		case 'cancel':
			c.rollouts.cancel(Number(args.id), String(args.user ?? 'admin'));
			return { ok: true };
		case 'saveServer':
			return { id: await c.store.saveServer(args as never) };
		case 'deleteServer':
			c.store.deleteServer(Number(args.id));
			return { ok: true };
		case 'apps':
			return { apps: await c.store.apps(Number(args.id)) };
		case 'setNodeApp':
			setNodeApp(c.sql, String(args.node_id), Number(args.server_id) || 0, String(args.app_id ?? ''), String(args.app_type ?? 'application'));
			return { ok: true };
		default:
			throw new Error(`unknown release op: ${op}`);
	}
}

// setNodeApp 登记节点对应的 Dokploy 应用; server_id 为 0 即解除登记
function setNodeApp(sql: SqlStorage, nodeId: string, serverId: number, appId: string, appType: string): void {
	if (!sql.exec('SELECT node_id FROM nodes WHERE node_id = ?', nodeId).toArray().length) throw new Error('节点不存在');
	const type: DokployAppType = appType === 'compose' ? 'compose' : 'application';
	if (serverId && !sql.exec('SELECT id FROM dokploy_servers WHERE id = ?', serverId).toArray().length) throw new Error('Dokploy 实例不存在');
	if (serverId && !appId.trim()) throw new Error('请选择 Dokploy 应用');
	sql.exec(
		'UPDATE nodes SET dokploy_server_id = ?, dokploy_app_id = ?, dokploy_app_type = ? WHERE node_id = ?',
		serverId,
		serverId ? appId.trim() : '',
		type,
		nodeId,
	);
}
