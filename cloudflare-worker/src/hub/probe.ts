/**
 * 节点主动探测
 *
 * 经 CF 访问节点的回源主机名 (橙云), 走的就是真实用户的回源路径。
 * 判定只看"源站有没有在应答": CF 的 52x / 502 / 503 / 504 视为挂了, 其他任何状态码都算活着
 * —— 节点升级前没有 /__proxy-go/health 时会走代理逻辑返回 404, 那也说明进程在应答。
 */

const DEAD_STATUSES = new Set([502, 503, 504, 520, 521, 522, 523, 524, 525, 526, 530]);

export interface ProbeResult {
	ok: boolean;
	status: number;
	error: string;
	unknown?: boolean; // 节点没有可探测的回源主机名 (Worker 不能直连裸 IP), 只能靠 WS 状态判断
}

export async function probeOrigin(originHost: string, timeoutMs = 6000): Promise<ProbeResult> {
	if (!originHost) return { ok: false, status: 0, error: '无回源主机名, 无法主动探测', unknown: true };
	try {
		const resp = await fetch(`https://${originHost}/__proxy-go/health`, {
			method: 'GET',
			headers: { 'User-Agent': 'proxy-go-hub-probe/1' },
			signal: AbortSignal.timeout(timeoutMs),
			redirect: 'manual',
			cf: { cacheTtl: 0, cacheEverything: false },
		});
		await resp.body?.cancel();
		if (DEAD_STATUSES.has(resp.status)) return { ok: false, status: resp.status, error: `HTTP ${resp.status}` };
		return { ok: true, status: resp.status, error: '' };
	} catch (e) {
		return { ok: false, status: 0, error: e instanceof Error ? e.message : String(e) };
	}
}

// probeTwice 两次探测都失败才算挂, 过滤单次网络抖动
export async function probeTwice(originHost: string): Promise<ProbeResult> {
	const first = await probeOrigin(originHost);
	if (first.ok || first.unknown) return first;
	await new Promise((r) => setTimeout(r, 1500));
	return probeOrigin(originHost);
}
