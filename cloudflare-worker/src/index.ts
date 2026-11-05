/**
 * proxy-go 中控 (i-manage.czl.net)
 *
 * 路由:
 *   /node/ws                 节点 WebSocket (Bearer API_TOKEN + X-Node-Id) → Hub DO
 *   /node/releases           CI 登记新版本 (Bearer RELEASE_TOKEN)
 *   /node/release/<v>/<arch> 节点下载新程序 (Bearer API_TOKEN, 代理 GitHub Release)
 *   /api/cache/clear-url     公开清缓存 (Bearer CACHE_CLEAR_REMOTE_TOKEN)
 *   /admin/api/*             管理 API (OAuth 会话 cookie)
 *   /admin/*                 管理前端静态资源 (Next.js 静态导出, basePath=/admin)
 *   其余带 Bearer API_TOKEN  节点 REST 旧协议 (D1 读写 / 心跳), 见 legacy.ts
 */
import { hubStub, type Env } from './env';
import { handleAdminAPI } from './admin/router';
import { handleLegacy } from './legacy';
import { bearerMatches, json } from './lib/http';
import { handleCacheClearURL } from './public/cache_clear';
import { handleNodeRelease } from './public/release';

export { Hub } from './hub/hub';

export default {
	async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
		const url = new URL(request.url);
		const path = url.pathname;

		if (path === '/node/ws') {
			if (!bearerMatches(request, env.API_TOKEN)) return json({ error: 'Unauthorized' }, 401);
			return hubStub(env).fetch(request);
		}

		// 登记版本只认 CI 专用的 RELEASE_TOKEN: 节点都持有 API_TOKEN, 任一节点泄露不能借此给全网推程序
		if (path === '/node/releases') {
			if (!bearerMatches(request, env.RELEASE_TOKEN)) return json({ error: 'Unauthorized' }, 401);
			return handleNodeRelease(request, env, path);
		}
		if (path.startsWith('/node/release/')) {
			if (!bearerMatches(request, env.API_TOKEN)) return json({ error: 'Unauthorized' }, 401);
			return handleNodeRelease(request, env, path);
		}

		if (path === '/api/cache/clear-url') return handleCacheClearURL(request, env, ctx);

		// 旧协议的通用 SQL 执行器挂在 /admin/sql/*, 必须先于管理前端的 /admin/* 分发
		if (path.startsWith('/admin/sql/')) {
			return bearerMatches(request, env.API_TOKEN) ? handleLegacy(request, env) : json({ error: 'Unauthorized' }, 401);
		}

		if (path.startsWith('/admin/api/')) return handleAdminAPI(request, env, url);

		if (path === '/' || path === '/admin') return Response.redirect(`${url.origin}/admin/`, 302);

		if (path.startsWith('/admin/')) return serveAdminAsset(request, env, url);

		// 节点旧协议: 路径不重叠, 以 Bearer API_TOKEN 区分
		if (bearerMatches(request, env.API_TOKEN)) return handleLegacy(request, env);

		// 漏了 /admin 前缀的后台页面地址 (旧书签 / 手输), 补上前缀跳回
		if ((path === '/login' || path.startsWith('/login/') || path === '/dashboard' || path.startsWith('/dashboard/')) && request.method === 'GET') {
			return Response.redirect(`${url.origin}/admin${path}${url.search}`, 302);
		}

		return json({ error: 'Not found' }, 404);
	},
};

// serveAdminAsset 剥掉 /admin 前缀映射到静态导出产物; 页面路由缺失时回落到 404.html
//
// 资源服务按 html_handling 补结尾斜杠时会回 307, Location 是剥掉前缀后的路径
// (/admin/dashboard → /dashboard/), 必须补回 /admin, 否则浏览器跳到前缀外落进 404
async function serveAdminAsset(request: Request, env: Env, url: URL): Promise<Response> {
	const inner = new URL(url);
	inner.pathname = url.pathname.slice('/admin'.length) || '/';
	const resp = await env.ASSETS.fetch(new Request(inner, request));
	if (resp.status >= 300 && resp.status < 400) {
		const location = resp.headers.get('Location');
		if (location) {
			const target = new URL(location, inner);
			if (target.origin === url.origin && !target.pathname.startsWith('/admin/')) {
				target.pathname = '/admin' + target.pathname;
			}
			const headers = new Headers(resp.headers);
			headers.set('Location', target.pathname + target.search);
			return new Response(null, { status: resp.status, headers });
		}
	}
	if (resp.status !== 404) return resp;
	const notFound = new URL(url);
	notFound.pathname = '/404.html';
	const page = await env.ASSETS.fetch(new Request(notFound, { method: 'GET' }));
	return new Response(page.body, { status: 404, headers: page.headers });
}
