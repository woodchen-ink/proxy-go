/**
 * Cloudflare API (DNS 绑定所需的最小子集)
 *
 * 平台事实 (源自 splitdns 踩坑记录):
 * - SaaS 自定义主机名的 custom_origin_server 必须是本账号里一条橙云记录, 不能填 IP;
 *   没建或灰云时回源直接失败, 而自定义主机名页面照样显示有效
 * - custom_origin_sni 是企业版字段, 与源服务器同名时不发 (CF 默认拿源服务器名当 SNI)
 */

const API = 'https://api.cloudflare.com/client/v4';

export class CloudflareError extends Error {
	constructor(
		public status: number,
		message: string,
	) {
		super(message);
	}
}

async function cf<T>(token: string, method: string, path: string, body?: unknown): Promise<T> {
	const resp = await fetch(API + path, {
		method,
		headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body),
	});
	const data = (await resp.json().catch(() => null)) as {
		success?: boolean;
		errors?: { code: number; message: string }[];
		result?: T;
	} | null;
	if (!data || !data.success) {
		const msg = data?.errors?.map((e) => `${e.code} ${e.message}`).join('; ') || `HTTP ${resp.status}`;
		throw new CloudflareError(resp.status, `Cloudflare: ${msg}`);
	}
	return data.result as T;
}

export interface Zone {
	id: string;
	name: string;
}

export async function listZones(token: string): Promise<Zone[]> {
	const out: Zone[] = [];
	for (let page = 1; page <= 10; page++) {
		const list = await cf<Zone[]>(token, 'GET', `/zones?per_page=50&page=${page}`);
		out.push(...list.map((z) => ({ id: z.id, name: z.name })));
		if (list.length < 50) break;
	}
	return out;
}

export interface CustomHostname {
	id: string;
	hostname: string;
	status: string;
	custom_origin_server?: string;
	ssl?: { status?: string };
}

export async function findCustomHostname(token: string, zoneId: string, hostname: string): Promise<CustomHostname | null> {
	const list = await cf<CustomHostname[]>(
		token,
		'GET',
		`/zones/${zoneId}/custom_hostnames?hostname=${encodeURIComponent(hostname)}&per_page=50`,
	);
	return list.find((h) => h.hostname === hostname) ?? null;
}

export async function updateCustomOrigin(token: string, zoneId: string, hostnameId: string, origin: string): Promise<void> {
	await cf(token, 'PATCH', `/zones/${zoneId}/custom_hostnames/${hostnameId}`, { custom_origin_server: origin });
}

export interface DNSRecord {
	id: string;
	name: string;
	type: string;
	content: string;
	proxied: boolean;
}

// listAddressRecords SaaS 区里的 A / AAAA 记录, 用来把节点出口 IP 对上它的回源主机名
export async function listAddressRecords(token: string, zoneId: string): Promise<DNSRecord[]> {
	const out: DNSRecord[] = [];
	for (let page = 1; page <= 20; page++) {
		const list = await cf<DNSRecord[]>(token, 'GET', `/zones/${zoneId}/dns_records?per_page=100&page=${page}`);
		out.push(...list.filter((r) => r.type === 'A' || r.type === 'AAAA'));
		if (list.length < 100) break;
	}
	return out;
}
