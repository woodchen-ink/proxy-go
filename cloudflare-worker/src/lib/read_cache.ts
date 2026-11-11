/**
 * D1 读取的短时缓存 (Cache API, 按数据中心)
 *
 * 仪表盘每 10~15s 轮询, 但大部分 D1 数据最快 5 分钟才变一次 (节点心跳), 离线节点的行干脆不变;
 * 每次都全表读会白白消耗 D1 读取行数 (免费 500 万行 / 天)。这里缓存 60s, 实时性由 DO 内存快照保证。
 * 写入方改了数据 (如重置统计) 时调 dropCached 立即失效。workers.dev 域名上 Cache API 是空操作, 自动退化为直读。
 */

const ORIGIN = 'https://read-cache.internal';

export async function cachedJSON<T>(key: string, ttlSec: number, load: () => Promise<T>): Promise<T> {
	const req = new Request(`${ORIGIN}/${encodeURIComponent(key)}`);
	const cache = caches.default;
	try {
		const hit = await cache.match(req);
		if (hit) return (await hit.json()) as T;
	} catch {
		/* 缓存不可用时直读 */
	}
	const value = await load();
	try {
		await cache.put(
			req,
			new Response(JSON.stringify(value), {
				headers: { 'Content-Type': 'application/json', 'Cache-Control': `max-age=${ttlSec}` },
			}),
		);
	} catch {
		/* 写缓存失败不影响结果 */
	}
	return value;
}

export async function dropCached(key: string): Promise<void> {
	try {
		await caches.default.delete(new Request(`${ORIGIN}/${encodeURIComponent(key)}`));
	} catch {
		/* ignore */
	}
}
