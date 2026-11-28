/**
 * Gravatar 兼容头像 /avatar/<md5|sha256>[.ext]?s=&d=
 *
 * Gravatar 有头像用 Gravatar; 没有且哈希来自 "<QQ号>@qq.com" 时换成 QQ 头像; 都没有按原参数 (d=) 交给 Gravatar 出默认图。
 * QQ 号由 R2 对照表 (QQ_AVATAR, cmd/qqavatar 本地生成上传) 还原, 格式见 cmd/qqavatar/format.go:
 *   <algo>/<哈希首字节>.bin = 65537 个 uint32 LE 分组累计数 (按哈希第 2~3 字节分组) + 按完整哈希排序的 QQ 号 (uint32 LE)
 * 表里只存 QQ 号, 组内二分时现场计算候选号的哈希比较, 命中即已验证。
 */
import type { Env } from '../env';

const GRAVATAR = 'https://0.gravatar.com/avatar/';
const HEADER_ENTRIES = 65537;
// qlogo 只有这几档尺寸, 取不小于请求尺寸的最小一档
const QQ_SIZES = [40, 100, 140, 640];
const LOOKUP_TTL = 7 * 86400;
const QQ_IMAGE_TTL = 86400;

type Algo = 'md5' | 'sha256';

export async function handleAvatar(request: Request, env: Env, ctx: ExecutionContext, url: URL): Promise<Response> {
	if (request.method !== 'GET' && request.method !== 'HEAD') {
		return new Response('Method Not Allowed', { status: 405, headers: { Allow: 'GET, HEAD' } });
	}
	const head = request.method === 'HEAD';
	const name = url.pathname.slice('/avatar/'.length);
	const hash = name.replace(/\.(jpe?g|png|gif|webp)$/i, '').toLowerCase();
	const algo: Algo | null = /^[0-9a-f]{32}$/.test(hash) ? 'md5' : /^[0-9a-f]{64}$/.test(hash) ? 'sha256' : null;
	const params = url.searchParams;
	const forceDefault = ['y', 'yes', 'true', '1'].includes((params.get('f') ?? params.get('forcedefault') ?? '').toLowerCase());
	if (!algo || !env.QQ_AVATAR || forceDefault) return relay(await fetch(GRAVATAR + name + url.search), 'gravatar', head);

	// 问 Gravatar 与查表并行; 查表出错只降级成不换 QQ 头像
	const probe = fetch(gravatarURL(hash, params, '404'));
	const lookup = findQQ(env.QQ_AVATAR, ctx, url.origin, algo, hash).catch((err) => {
		console.error('qq avatar lookup failed', algo, err);
		return null;
	});
	// 查到 QQ 号就先拉图, 不等 Gravatar 回 404 (qlogo 从海外机房拉是最慢的一段)
	const qqImage = lookup.then((qq) => (qq === null ? null : fetch(`https://q1.qlogo.cn/g?b=qq&nk=${qq}&s=${qqSize(params)}`).catch(() => null)));
	const gravatar = await probe;
	if (gravatar.ok) {
		ctx.waitUntil(qqImage.then((r) => r?.body?.cancel()));
		return relay(gravatar, 'gravatar', head);
	}

	const qqResp = await qqImage;
	if (qqResp?.ok) return relay(qqResp, 'qq', head, QQ_IMAGE_TTL);
	const fallback = (params.get('d') ?? params.get('default')) === '404' ? gravatar : await fetch(GRAVATAR + name + url.search);
	return relay(fallback, 'default', head);
}

function gravatarURL(hash: string, params: URLSearchParams, d: string): string {
	const q = new URLSearchParams(params);
	q.delete('default');
	q.set('d', d);
	return `${GRAVATAR}${hash}?${q}`;
}

function qqSize(params: URLSearchParams): number {
	const s = Number(params.get('s') ?? params.get('size') ?? 80) || 80;
	return QQ_SIZES.find((v) => v >= s) ?? QQ_SIZES[QQ_SIZES.length - 1];
}

// relay 转出上游图片, 只带与图片相关的头; X-Avatar-Source 便于排查走了哪一路
function relay(upstream: Response, source: string, head: boolean, ttl?: number): Response {
	const headers = new Headers();
	for (const key of ['Content-Type', 'Content-Length', 'Cache-Control', 'Last-Modified', 'ETag']) {
		const v = upstream.headers.get(key);
		if (v) headers.set(key, v);
	}
	if (ttl) headers.set('Cache-Control', `public, max-age=${ttl}`);
	headers.set('Access-Control-Allow-Origin', '*');
	headers.set('X-Avatar-Source', source);
	return new Response(head ? null : upstream.body, { status: upstream.status, headers });
}

// findQQ 查表结果 (含未命中) 缓存在本机房, 同一哈希 7 天内不再读 R2
async function findQQ(bucket: R2Bucket, ctx: ExecutionContext, origin: string, algo: Algo, hash: string): Promise<number | null> {
	const key = new Request(`${origin}/__avatar_lookup/${algo}/${hash}`);
	const cache = caches.default;
	const hit = await cache.match(key);
	if (hit) {
		const v = Number(await hit.text());
		return v > 0 ? v : null;
	}
	const qq = await lookupTable(bucket, algo, hash);
	ctx.waitUntil(cache.put(key, new Response(String(qq ?? 0), { headers: { 'Cache-Control': `max-age=${LOOKUP_TTL}` } })));
	return qq;
}

async function lookupTable(bucket: R2Bucket, algo: Algo, hash: string): Promise<number | null> {
	const target = hexBytes(hash);
	const file = `${algo}/${hash.slice(0, 2)}.bin`;
	const group = (target[1] << 8) | target[2];
	const head = await readRange(bucket, file, group * 4, 8);
	if (!head) return null; // 表未上传
	const start = head.getUint32(0, true);
	const end = head.getUint32(4, true);
	if (end <= start) return null;
	const ids = await readRange(bucket, file, (HEADER_ENTRIES + start) * 4, (end - start) * 4);
	if (!ids) return null;

	const enc = new TextEncoder();
	const digestName = algo === 'md5' ? 'MD5' : 'SHA-256';
	let lo = 0;
	let hi = (end - start) - 1;
	while (lo <= hi) {
		const mid = (lo + hi) >>> 1;
		const qq = ids.getUint32(mid * 4, true);
		const h = new Uint8Array(await crypto.subtle.digest(digestName, enc.encode(`${qq}@qq.com`)));
		const c = compareBytes(h, target);
		if (c === 0) return qq;
		if (c < 0) lo = mid + 1;
		else hi = mid - 1;
	}
	return null;
}

async function readRange(bucket: R2Bucket, key: string, offset: number, length: number): Promise<DataView | null> {
	const obj = await bucket.get(key, { range: { offset, length } });
	if (!obj) return null;
	const buf = await obj.arrayBuffer();
	return buf.byteLength === length ? new DataView(buf) : null;
}

function hexBytes(hex: string): Uint8Array {
	const out = new Uint8Array(hex.length / 2);
	for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
	return out;
}

function compareBytes(a: Uint8Array, b: Uint8Array): number {
	for (let i = 0; i < a.length; i++) {
		if (a[i] !== b[i]) return a[i] - b[i];
	}
	return 0;
}
