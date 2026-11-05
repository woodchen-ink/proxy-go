/**
 * 外部 CDN 缓存清理 (Cloudflare / EdgeOne), 由中控直接调用厂商 API, 不经节点
 *
 * 统一 purge 类型 all / urls / prefixes / hosts / tags, 厂商差异在这里收敛。
 * EdgeOne 走 TC3 签名 (POST /, service=teo, region=ap-guangzhou, CreatePurgeTask 2022-09-01)。
 */
import { HttpError } from '../lib/http';
import { callTencent, TencentError } from '../lib/tc3';
import type { CDNProvider } from './config';

export const PURGE_TYPES = ['all', 'urls', 'prefixes', 'hosts', 'tags'] as const;
export type PurgeType = (typeof PURGE_TYPES)[number];

export interface PurgeRequest {
	type: string;
	targets?: string[];
}

export interface PurgeResult {
	success: boolean;
	provider_id: string;
	provider: string;
	job_id?: string;
	message?: string;
	raw?: unknown;
}

export function activeProvider(providers: CDNProvider[]): CDNProvider | null {
	return providers.find((p) => p.Enabled) ?? null;
}

function validate(req: PurgeRequest): { type: PurgeType; targets: string[] } {
	if (!PURGE_TYPES.includes(req.type as PurgeType)) throw new HttpError(400, `invalid purge type: ${req.type}`);
	const targets = (req.targets ?? []).map((t) => String(t).trim());
	if (req.type !== 'all' && (targets.length === 0 || targets.some((t) => !t))) throw new HttpError(400, 'purge target is empty');
	return { type: req.type as PurgeType, targets };
}

const EDGEONE_TYPES: Record<PurgeType, string> = {
	all: 'purge_all',
	urls: 'purge_url',
	prefixes: 'purge_prefix',
	hosts: 'purge_host',
	tags: 'purge_cache_tag',
};

export async function purge(provider: CDNProvider, req: PurgeRequest): Promise<PurgeResult> {
	const { type, targets } = validate(req);
	const base = { provider_id: provider.ID, provider: provider.Type };

	if (provider.Type === 'cloudflare') {
		const body: Record<string, unknown> =
			type === 'all' ? { purge_everything: true } : { [type === 'urls' ? 'files' : type]: targets };
		const resp = await fetch(`https://api.cloudflare.com/client/v4/zones/${provider.Credentials.zoneId}/purge_cache`, {
			method: 'POST',
			headers: { Authorization: `Bearer ${provider.Credentials.apiToken}`, 'Content-Type': 'application/json' },
			body: JSON.stringify(body),
		});
		const raw = (await resp.json().catch(() => ({}))) as { success?: boolean; errors?: { message: string }[]; result?: { id?: string } };
		if (!raw.success) {
			return { ...base, success: false, message: raw.errors?.[0]?.message ?? 'Cloudflare purge 失败', raw };
		}
		return { ...base, success: true, job_id: raw.result?.id, raw };
	}

	if (provider.Type === 'edgeone') {
		try {
			const raw = await callTencent<{ JobId?: string }>(
				{ secretId: provider.Credentials.secretId, secretKey: provider.Credentials.secretKey },
				{
					service: 'teo',
					host: 'teo.tencentcloudapi.com',
					action: 'CreatePurgeTask',
					version: '2022-09-01',
					region: 'ap-guangzhou',
					payload: { ZoneId: provider.Credentials.zoneId, Type: EDGEONE_TYPES[type], Targets: type === 'all' ? [] : targets },
				},
			);
			return { ...base, success: true, job_id: raw.JobId, raw };
		} catch (e) {
			return { ...base, success: false, message: e instanceof TencentError ? e.message : String(e) };
		}
	}

	throw new HttpError(400, `unsupported CDN provider type: ${provider.Type}`);
}
