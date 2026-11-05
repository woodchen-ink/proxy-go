/**
 * 节点程序发布的节点侧接口
 *
 *   POST /node/releases                   CI 登记新版本 (Bearer RELEASE_TOKEN) {version, build_time, title, assets: {arch: {url, sha256, size}}}
 *   GET  /node/release/<version>/<arch>   节点下载新程序 (Bearer API_TOKEN): 中控代理 GitHub Release, 节点只需能连上中控
 *                                          (国内节点直连 github.com 常常不稳)
 */
import { hubStub, type Env } from '../env';
import { json } from '../lib/http';
import type { Release } from '../hub/release_store';

export async function handleNodeRelease(request: Request, env: Env, path: string): Promise<Response> {
	const hub = hubStub(env);
	if (path === '/node/releases') {
		if (request.method !== 'POST') return json({ error: 'Method not allowed' }, 405);
		let body: Release;
		try {
			body = (await request.json()) as Release;
		} catch {
			return json({ error: 'invalid json' }, 400);
		}
		try {
			return json({ success: true, release: await hub.registerRelease(body) });
		} catch (e) {
			return json({ error: e instanceof Error ? e.message : String(e) }, 400);
		}
	}

	const m = /^\/node\/release\/([\w.-]{1,40})\/(amd64|arm64)$/.exec(path);
	if (!m || request.method !== 'GET') return json({ error: 'Not found' }, 404);
	const rel = await hub.getRelease(m[1]);
	const asset = rel?.assets[m[2]];
	if (!asset) return json({ error: 'release not found' }, 404);
	const upstream = await fetch(asset.url, { redirect: 'follow', cf: { cacheTtl: 86400, cacheEverything: true } });
	if (!upstream.ok || !upstream.body) return json({ error: `upstream HTTP ${upstream.status}` }, 502);
	const headers = new Headers({ 'Content-Type': 'application/octet-stream', 'Cache-Control': 'no-store' });
	const len = upstream.headers.get('Content-Length');
	if (len) headers.set('Content-Length', len);
	return new Response(upstream.body, { status: 200, headers });
}
