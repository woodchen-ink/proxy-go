/**
 * EdgeOne 加速域名的回源切换 (teo 2022-09-01)
 *
 * EdgeOne 线路的"解析"实际是加速域名的源站配置: 切节点 = 把 OriginInfo.Origin 改成目标节点 IP。
 * 修改时先读当前配置, 只替换 Origin, 其余源站字段 (协议 / 端口 / 回源 Host 等) 原样带回,
 * 避免 ModifyAccelerationDomain 把未传字段重置为默认值。
 */
import { callTencent, type TencentCredential } from '../lib/tc3';

const BASE = { service: 'teo', host: 'teo.tencentcloudapi.com', version: '2022-09-01' };

export interface EdgeOneZone {
	id: string;
	name: string;
}

interface AccelerationDomain {
	DomainName: string;
	DomainStatus: string;
	OriginDetail?: { OriginType?: string; Origin?: string; BackupOrigin?: string; [k: string]: unknown };
	OriginProtocol?: string;
	HttpOriginPort?: number;
	HttpsOriginPort?: number;
	IPv6Status?: string;
}

export async function listZones(cred: TencentCredential): Promise<EdgeOneZone[]> {
	const resp = await callTencent<{ Zones?: { ZoneId: string; ZoneName: string }[] }>(cred, {
		...BASE,
		action: 'DescribeZones',
		payload: { Limit: 100 },
	});
	return (resp.Zones ?? []).map((z) => ({ id: z.ZoneId, name: z.ZoneName }));
}

export async function getDomain(cred: TencentCredential, zoneId: string, domain: string): Promise<AccelerationDomain | null> {
	const resp = await callTencent<{ AccelerationDomains?: AccelerationDomain[] }>(cred, {
		...BASE,
		action: 'DescribeAccelerationDomains',
		payload: { ZoneId: zoneId, Filters: [{ Name: 'domain-name', Values: [domain] }], Limit: 20 },
	});
	return (resp.AccelerationDomains ?? []).find((d) => d.DomainName === domain) ?? null;
}

// originOf 当前源站值; 源站组等非 IP/域名类型返回带前缀的描述, 不会与节点 IP 相等
export function originOf(d: AccelerationDomain): string {
	const od = d.OriginDetail ?? {};
	if (od.OriginType && od.OriginType !== 'IP_DOMAIN') return `${od.OriginType}:${od.Origin ?? ''}`;
	return String(od.Origin ?? '').toLowerCase();
}

export async function setOrigin(cred: TencentCredential, zoneId: string, domain: string, origin: string): Promise<void> {
	const current = await getDomain(cred, zoneId, domain);
	if (!current) throw new Error(`EdgeOne 站点里没有加速域名 ${domain}`);
	const od = current.OriginDetail ?? {};
	const payload: Record<string, unknown> = {
		ZoneId: zoneId,
		DomainName: domain,
		OriginInfo: {
			OriginType: 'IP_DOMAIN',
			Origin: origin,
			...(od.BackupOrigin ? { BackupOrigin: od.BackupOrigin } : {}),
			...(od.HostHeader ? { HostHeader: od.HostHeader } : {}),
			...(od.PrivateAccess ? { PrivateAccess: od.PrivateAccess } : {}),
			...(od.PrivateParameters ? { PrivateParameters: od.PrivateParameters } : {}),
		},
	};
	if (current.OriginProtocol) payload.OriginProtocol = current.OriginProtocol;
	if (current.HttpOriginPort) payload.HttpOriginPort = current.HttpOriginPort;
	if (current.HttpsOriginPort) payload.HttpsOriginPort = current.HttpsOriginPort;
	if (current.IPv6Status) payload.IPv6Status = current.IPv6Status;
	await callTencent(cred, { ...BASE, action: 'ModifyAccelerationDomain', payload });
}

export async function describeRaw(cred: TencentCredential, zoneId: string, domain: string): Promise<unknown> {
	return getDomain(cred, zoneId, domain);
}
