/**
 * 管理 API: 节点程序版本 / 滚动发布 / Dokploy 实例与节点应用登记
 */
import { hubStub, type Env } from '../env';
import { json, readJSON } from '../lib/http';

type Route = (method: string, pattern: string, h: (ctx: { request: Request; env: Env; user: string; params: string[] }) => Promise<Response>) => void;

const op = (env: Env, name: string, args: Record<string, unknown> = {}) => hubStub(env).releaseOp(name, args);

export function registerReleaseRoutes(route: Route): void {
	route('GET', '/admin/api/hub/releases', async ({ env }) => json(await op(env, 'overview')));

	route('POST', '/admin/api/hub/releases/auto', async ({ env, request }) => {
		const body = await readJSON<{ auto_binary?: boolean }>(request);
		return json(await op(env, 'setAuto', { auto_binary: !!body.auto_binary }));
	});

	route('POST', '/admin/api/hub/rollouts', async ({ env, request, user }) => {
		const body = await readJSON<{ kind?: string; version?: string; nodes?: string[] }>(request);
		return json(await op(env, 'start', { ...body, user }));
	});

	route('POST', '/admin/api/hub/rollouts/([0-9]+)/cancel', async ({ env, params, user }) => json(await op(env, 'cancel', { id: Number(params[0]), user })));

	route('POST', '/admin/api/hub/dokploy', async ({ env, request }) => json(await op(env, 'saveServer', await readJSON(request))));

	route('DELETE', '/admin/api/hub/dokploy/([0-9]+)', async ({ env, params }) => json(await op(env, 'deleteServer', { id: Number(params[0]) })));

	route('GET', '/admin/api/hub/dokploy/([0-9]+)/apps', async ({ env, params }) => json(await op(env, 'apps', { id: Number(params[0]) })));

	route('POST', '/admin/api/hub/nodes/([^/]+)/dokploy', async ({ env, request, params }) => {
		const body = await readJSON<Record<string, unknown>>(request);
		return json(await op(env, 'setNodeApp', { ...body, node_id: decodeURIComponent(params[0]) }));
	});
}
