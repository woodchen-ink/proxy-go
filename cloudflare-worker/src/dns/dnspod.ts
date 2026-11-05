/**
 * DNSPod API 3.0 (DNS 绑定所需的最小子集)
 *
 * 平台事实 (源自 splitdns):
 * - 免费版 TTL 最低 600, 线路只有 默认 / 境内 / 境外
 * - 腾讯云同语义错误码挂在不同前缀下, 按后缀匹配 (NoDataOfRecord 等)
 * - 改记录用 ModifyRecord 就地改, 不删了重建, 不存在"记录短暂消失"的窗口
 */
import { callTencent, TencentError, type TencentCredential } from '../lib/tc3';

const BASE = { service: 'dnspod', host: 'dnspod.tencentcloudapi.com', version: '2021-03-23' };

export const DEFAULT_TTL = 600;

export interface DnspodRecord {
	id: number;
	name: string;
	type: string;
	line: string;
	value: string;
	ttl: number;
	enabled: boolean;
}

interface RawRecord {
	RecordId: number;
	Name: string;
	Type: string;
	Line: string;
	Value: string;
	TTL: number;
	Status: string;
}

export async function listRecords(cred: TencentCredential, domain: string, subdomain?: string): Promise<DnspodRecord[]> {
	const out: DnspodRecord[] = [];
	for (let offset = 0; offset < 3000; offset += 100) {
		let resp: { RecordList?: RawRecord[]; RecordCountInfo?: { TotalCount?: number } };
		try {
			resp = await callTencent(cred, {
				...BASE,
				action: 'DescribeRecordList',
				payload: { Domain: domain, Offset: offset, Limit: 100, ...(subdomain ? { Subdomain: subdomain } : {}) },
			});
		} catch (e) {
			if (e instanceof TencentError && e.code.endsWith('NoDataOfRecord')) return out;
			throw e;
		}
		for (const r of resp.RecordList ?? []) {
			out.push({
				id: r.RecordId,
				name: r.Name,
				type: r.Type,
				line: r.Line,
				value: r.Value,
				ttl: r.TTL,
				enabled: r.Status === 'ENABLE',
			});
		}
		const total = resp.RecordCountInfo?.TotalCount ?? 0;
		if (out.length >= total || !resp.RecordList?.length) break;
	}
	return out;
}

export async function modifyRecord(
	cred: TencentCredential,
	domain: string,
	record: { id: number; name: string; type: string; line: string; value: string; ttl?: number },
): Promise<void> {
	await callTencent(cred, {
		...BASE,
		action: 'ModifyRecord',
		payload: {
			Domain: domain,
			RecordId: record.id,
			SubDomain: record.name,
			RecordType: record.type,
			RecordLine: record.line,
			Value: record.value,
			TTL: Math.max(record.ttl ?? DEFAULT_TTL, DEFAULT_TTL),
		},
	});
}

export async function listDomains(cred: TencentCredential): Promise<string[]> {
	const resp = await callTencent<{ DomainList?: { Name: string }[] }>(cred, {
		...BASE,
		action: 'DescribeDomainList',
		payload: { Limit: 3000 },
	});
	return (resp.DomainList ?? []).map((d) => d.Name);
}

// normalizeHost DNSPod 的 CNAME 值带结尾点, 比较前统一去掉并小写
export function normalizeHost(v: string): string {
	return v.trim().toLowerCase().replace(/\.$/, '');
}
