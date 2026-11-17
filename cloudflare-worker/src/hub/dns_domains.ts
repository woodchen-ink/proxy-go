/**
 * DNS 域名: 一个访问域名一份账号配置 (CF 凭据 + SaaS 区 / 腾讯云凭据 + DNSPod 域名 + 主机记录 / EdgeOne 站点),
 * 其下每条线路 (dns_bindings 一行) 只选走法与节点池, 账号字段读取时从域名上补齐。
 *
 * dns_bindings 里的 cf_credential_id / saas_zone_id / dnspod_* / record_name 列保留但以域名为准 (applyDomain 覆盖),
 * 旧数据由 seedDomains 在 DO 冷启动时按 hostname 归并成域名行。
 */
import type { Binding } from '../dns/binding';

export interface DnsDomain {
	id: number;
	hostname: string;
	cf_credential_id: number;
	saas_zone_id: string;
	saas_zone_name: string;
	tc_credential_id: number; // 腾讯云凭据, DNSPod 与 EdgeOne 共用
	dnspod_domain: string;
	record_name: string;
	eo_zone_id: string;
	eo_zone_name: string;
	note: string;
	updated_at: number;
}

export interface DomainInput {
	id?: number;
	hostname: string;
	cf_credential_id?: number;
	saas_zone_id?: string;
	saas_zone_name?: string;
	tc_credential_id?: number;
	dnspod_domain?: string;
	record_name?: string;
	eo_zone_id?: string;
	eo_zone_name?: string;
	note?: string;
}

export const DOMAINS_DDL = `CREATE TABLE IF NOT EXISTS dns_domains (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	hostname TEXT NOT NULL UNIQUE,
	cf_credential_id INTEGER NOT NULL DEFAULT 0,
	saas_zone_id TEXT NOT NULL DEFAULT '',
	saas_zone_name TEXT NOT NULL DEFAULT '',
	tc_credential_id INTEGER NOT NULL DEFAULT 0,
	dnspod_domain TEXT NOT NULL DEFAULT '',
	record_name TEXT NOT NULL DEFAULT '@',
	eo_zone_id TEXT NOT NULL DEFAULT '',
	eo_zone_name TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL
)`;

const HOSTNAME_RE = /^[a-z0-9.-]+\.[a-z]{2,}$/;

export function normalizeHostname(v: unknown): string {
	return String(v ?? '')
		.trim()
		.toLowerCase()
		.replace(/\.$/, '');
}

export function rowToDomain(r: Record<string, SqlStorageValue>): DnsDomain {
	return {
		id: Number(r.id),
		hostname: String(r.hostname),
		cf_credential_id: Number(r.cf_credential_id),
		saas_zone_id: String(r.saas_zone_id),
		saas_zone_name: String(r.saas_zone_name),
		tc_credential_id: Number(r.tc_credential_id),
		dnspod_domain: String(r.dnspod_domain),
		record_name: String(r.record_name),
		eo_zone_id: String(r.eo_zone_id),
		eo_zone_name: String(r.eo_zone_name),
		note: String(r.note),
		updated_at: Number(r.updated_at),
	};
}

// applyDomain 用域名上的账号配置补齐线路; EdgeOne 线路的站点 ID 沿用 saas_zone_id 字段承载
export function applyDomain(b: Binding, d: DnsDomain | undefined): Binding {
	if (!d) return b;
	const eo = b.kind === 'edgeone';
	return {
		...b,
		cf_credential_id: d.cf_credential_id,
		saas_zone_id: eo ? d.eo_zone_id : d.saas_zone_id,
		saas_zone_name: eo ? d.eo_zone_name : d.saas_zone_name,
		dnspod_credential_id: d.tc_credential_id,
		dnspod_domain: d.dnspod_domain,
		record_name: d.record_name,
	};
}

// missingFor 该域名缺哪些配置, 导致这种走法用不了; 空串表示可用
export function missingFor(d: DnsDomain, kind: Binding['kind']): string {
	if (!d.tc_credential_id) return '域名未配置腾讯云凭据';
	if (kind === 'edgeone') return d.eo_zone_id ? '' : '域名未配置 EdgeOne 站点';
	if (!d.dnspod_domain) return '域名未配置 DNSPod 域名';
	if (kind === 'cf_dnspod' && (!d.cf_credential_id || !d.saas_zone_id)) return '域名未配置 CF 凭据或 SaaS 区';
	return '';
}

export class DomainStore {
	constructor(private sql: SqlStorage) {}

	list(): DnsDomain[] {
		return this.sql.exec('SELECT * FROM dns_domains ORDER BY hostname').toArray().map(rowToDomain);
	}

	byHostname(): Map<string, DnsDomain> {
		return new Map(this.list().map((d) => [d.hostname, d]));
	}

	get(hostname: string): DnsDomain | undefined {
		const r = this.sql.exec('SELECT * FROM dns_domains WHERE hostname = ?', hostname).toArray()[0];
		return r ? rowToDomain(r) : undefined;
	}

	save(input: DomainInput): number {
		const hostname = normalizeHostname(input.hostname);
		if (!HOSTNAME_RE.test(hostname)) throw new Error('访问域名格式不对');
		const dup = this.sql.exec('SELECT id FROM dns_domains WHERE hostname = ?', hostname).toArray()[0];
		if (dup && Number(dup.id) !== (input.id ?? 0)) throw new Error(`${hostname} 已存在`);
		const cfCred = Number(input.cf_credential_id) || 0;
		const tcCred = Number(input.tc_credential_id) || 0;
		const args = [
			hostname,
			cfCred,
			cfCred ? (input.saas_zone_id ?? '').trim() : '',
			cfCred ? (input.saas_zone_name ?? '').trim() : '',
			tcCred,
			tcCred ? normalizeHostname(input.dnspod_domain) : '',
			(input.record_name ?? '@').trim() || '@',
			tcCred ? (input.eo_zone_id ?? '').trim() : '',
			tcCred ? (input.eo_zone_name ?? '').trim() : '',
			(input.note ?? '').slice(0, 200),
			Date.now(),
		];
		if (input.id) {
			const old = this.sql.exec('SELECT hostname FROM dns_domains WHERE id = ?', input.id).toArray()[0];
			if (!old) throw new Error('域名不存在');
			this.sql.exec(
				`UPDATE dns_domains SET hostname=?, cf_credential_id=?, saas_zone_id=?, saas_zone_name=?, tc_credential_id=?,
				 dnspod_domain=?, record_name=?, eo_zone_id=?, eo_zone_name=?, note=?, updated_at=? WHERE id=?`,
				...args,
				input.id,
			);
			// 账号配置变了, 各线路旧的巡检结果描述的是另一处解析, 清掉等下次检查
			this.sql.exec("UPDATE dns_bindings SET hostname = ?, last_check = '' WHERE hostname = ?", hostname, String(old.hostname));
			return input.id;
		}
		return Number(
			this.sql
				.exec(
					`INSERT INTO dns_domains (hostname, cf_credential_id, saas_zone_id, saas_zone_name, tc_credential_id,
					 dnspod_domain, record_name, eo_zone_id, eo_zone_name, note, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
					...args,
				)
				.one().id,
		);
	}

	// delete 连同其下全部线路一起删 (只删中控记录, 不改线上解析)
	delete(id: number): void {
		const r = this.sql.exec('SELECT hostname FROM dns_domains WHERE id = ?', id).toArray()[0];
		if (!r) return;
		this.sql.exec('DELETE FROM dns_bindings WHERE hostname = ?', String(r.hostname));
		this.sql.exec('DELETE FROM dns_domains WHERE id = ?', id);
	}

	credentialUsers(credentialId: number): string[] {
		return this.sql
			.exec('SELECT hostname FROM dns_domains WHERE cf_credential_id = ? OR tc_credential_id = ?', credentialId, credentialId)
			.toArray()
			.map((r) => String(r.hostname));
	}
}

// seedDomains 旧数据迁移: 每个还没有域名行的 hostname, 从其线路上收集账号配置建一行。
// 同一 hostname 下 EdgeOne 线路与别的线路同占「默认」时, 把 EdgeOne 挪到「境内」(它本来就只承接境内)。
export function seedDomains(sql: SqlStorage): void {
	const rows = sql.exec('SELECT * FROM dns_bindings WHERE hostname NOT IN (SELECT hostname FROM dns_domains)').toArray();
	const byHost = new Map<string, Record<string, SqlStorageValue>[]>();
	for (const r of rows) byHost.set(String(r.hostname), [...(byHost.get(String(r.hostname)) ?? []), r]);
	for (const [hostname, list] of byHost) {
		const cf = list.find((r) => r.kind === 'cf_dnspod');
		const eo = list.find((r) => r.kind === 'edgeone');
		const pod = cf ?? list.find((r) => r.kind === 'dnspod_ip');
		const tc = pod ?? eo;
		sql.exec(
			`INSERT INTO dns_domains (hostname, cf_credential_id, saas_zone_id, saas_zone_name, tc_credential_id,
			 dnspod_domain, record_name, eo_zone_id, eo_zone_name, note, updated_at) VALUES (?,?,?,?,?,?,?,?,?,'',?)`,
			hostname,
			cf ? Number(cf.cf_credential_id) : 0,
			cf ? String(cf.saas_zone_id) : '',
			cf ? String(cf.saas_zone_name) : '',
			tc ? Number(tc.dnspod_credential_id) : 0,
			pod ? String(pod.dnspod_domain) : '',
			pod ? String(pod.record_name) : '@',
			eo ? String(eo.saas_zone_id) : '',
			eo ? String(eo.saas_zone_name) : '',
			Date.now(),
		);
		const taken = (line: string) => list.some((r) => r !== eo && r.record_line === line);
		if (eo && eo.record_line === '默认' && taken('默认') && !taken('境内')) {
			sql.exec("UPDATE dns_bindings SET record_line = '境内' WHERE id = ?", Number(eo.id));
		}
	}
}
