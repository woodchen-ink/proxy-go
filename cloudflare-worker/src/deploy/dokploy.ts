/**
 * Dokploy API (重新部署节点容器)
 *
 * 鉴权头 `x-api-key` (Dokploy 设置 → Profile → API Keys 生成);
 * 接口形如 `POST <url>/api/application.redeploy`, 与 Dokploy 的 OpenAPI 一致。
 */

export interface DokployServer {
	url: string;
	apiKey: string;
}

export type DokployAppType = 'application' | 'compose';

export interface DokployApp {
	id: string;
	type: DokployAppType;
	name: string;
	project: string;
}

async function call(s: DokployServer, method: 'GET' | 'POST', path: string, body?: unknown): Promise<unknown> {
	const resp = await fetch(`${s.url.replace(/\/+$/, '')}/api/${path}`, {
		method,
		headers: { 'x-api-key': s.apiKey, 'Content-Type': 'application/json', Accept: 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body),
		signal: AbortSignal.timeout(20_000),
	});
	const text = await resp.text();
	if (!resp.ok) throw new Error(`Dokploy ${path} HTTP ${resp.status}: ${text.slice(0, 200)}`);
	try {
		return text ? JSON.parse(text) : null;
	} catch {
		return text;
	}
}

// redeploy 重新拉镜像并重建容器; Dokploy 只负责排队, 是否起来由中控看节点重连判断
export async function redeploy(s: DokployServer, type: DokployAppType, appId: string, reason: string): Promise<void> {
	if (type === 'compose') await call(s, 'POST', 'compose.redeploy', { composeId: appId, title: reason });
	else await call(s, 'POST', 'application.redeploy', { applicationId: appId, title: reason });
}

export interface DokployDeployment {
	status: string; // running | done | error | idle
	created_at: number;
	finished_at: number; // 旧版 Dokploy 没有该字段时为 0
	error: string;
}

// latestDeployment 应用最近一次部署; 没有记录返回 null
export async function latestDeployment(s: DokployServer, type: DokployAppType, appId: string): Promise<DokployDeployment | null> {
	const path = type === 'compose' ? `deployment.allByCompose?composeId=${encodeURIComponent(appId)}` : `deployment.all?applicationId=${encodeURIComponent(appId)}`;
	const list = (await call(s, 'GET', path)) as Record<string, unknown>[] | null;
	if (!Array.isArray(list) || list.length === 0) return null;
	const ts = (v: unknown) => (typeof v === 'string' && v ? Date.parse(v) || 0 : 0);
	const latest = list.reduce((a, b) => (ts(b.createdAt) > ts(a.createdAt) ? b : a));
	return { status: String(latest.status ?? ''), created_at: ts(latest.createdAt), finished_at: ts(latest.finishedAt), error: String(latest.errorMessage ?? '').slice(0, 300) };
}

// listApps 列出实例下全部应用供页面下拉选择
//
// project.all 的嵌套结构随 Dokploy 版本变 (新版多一层 environments), 这里按字段名递归收集,
// 不依赖具体层级: 带 applicationId / composeId 的对象就是一个应用, 最近的带 projectId 的祖先是项目
export async function listApps(s: DokployServer): Promise<DokployApp[]> {
	const out: DokployApp[] = [];
	const walk = (v: unknown, project: string): void => {
		if (Array.isArray(v)) {
			for (const x of v) walk(x, project);
			return;
		}
		if (!v || typeof v !== 'object') return;
		const o = v as Record<string, unknown>;
		const isProject = typeof o.projectId === 'string' && typeof o.name === 'string' && !o.environmentId && !o.applicationId && !o.composeId;
		const proj = isProject ? String(o.name) : project;
		// project.all 里的应用只带 id / name / status 精简字段
		if (typeof o.applicationId === 'string' && typeof o.name === 'string') {
			out.push({ id: o.applicationId, type: 'application', name: o.name, project: proj });
		} else if (typeof o.composeId === 'string' && typeof o.name === 'string') {
			out.push({ id: o.composeId, type: 'compose', name: o.name, project: proj });
		}
		for (const x of Object.values(o)) if (x && typeof x === 'object') walk(x, proj);
	};
	walk(await call(s, 'GET', 'project.all'), '');
	const seen = new Set<string>();
	return out.filter((a) => (seen.has(a.id) ? false : (seen.add(a.id), true)));
}
