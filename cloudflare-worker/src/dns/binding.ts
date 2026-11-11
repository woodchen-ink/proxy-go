/**
 * DNS 绑定: 一个访问域名在「CF SaaS 回源 + DNSPod 线路记录」两处指向同一个节点
 *
 * 切换节点就是把两处同时改成目标节点的回源主机名 (如 rn-22dc3.20200511.xyz):
 *   1. SaaS 区自定义主机名的 custom_origin_server
 *   2. DNSPod 上该域名指定线路 (默认「默认」) 的 CNAME
 * kind=edgeone 时改的是 EdgeOne 加速域名的源站 (OriginInfo.Origin = 节点 IP), 凭据复用腾讯云 API 密钥,
 * EdgeOne 站点 ID 存在 saas_zone_id 列。kind=dnspod_ip 时只改 DNSPod 指定线路的记录, 直接解析到节点 IP (A / AAAA),
 * 不经 CF / EdgeOne。各类绑定互相独立, 同一访问域名可以各有一条 (dnspod_ip 按线路各一条)。
 *
 * 判定层区分"确实不一致"与"根本没读到": 读取失败记在 error 字段, 不当成不一致去自动修。
 */
import { openSecret } from '../lib/crypto';
import type { TencentCredential } from '../lib/tc3';
import * as cf from './cloudflare';
import * as dnspod from './dnspod';
import * as edgeone from './edgeone';

export type BindingKind = 'cf_dnspod' | 'edgeone' | 'dnspod_ip';

const KINDS: BindingKind[] = ['cf_dnspod', 'edgeone', 'dnspod_ip'];

export function parseKind(v: unknown): BindingKind {
	return KINDS.includes(v as BindingKind) ? (v as BindingKind) : 'cf_dnspod';
}

// usesNodeIP 该类绑定写入的是节点出口 IP (否则是回源主机名)
export function usesNodeIP(kind: BindingKind): boolean {
	return kind !== 'cf_dnspod';
}

// usesDnspodLine 该类绑定会改 DNSPod 线路记录
export function usesDnspodLine(kind: BindingKind): boolean {
	return kind !== 'edgeone';
}

export interface Binding {
	id: number;
	kind: BindingKind;
	hostname: string;
	enabled: boolean;
	cf_credential_id: number;
	saas_zone_id: string;
	saas_zone_name: string;
	dnspod_credential_id: number;
	dnspod_domain: string;
	record_name: string;
	record_line: string;
	pool: string[]; // node_id 有序列表, pool[0] 为首选节点
	active_node: string;
	auto_failover: boolean;
	auto_failback: boolean;
	last_switch_at: number;
	last_check: BindingCheck | null;
	note: string;
	updated_at: number;
}

export interface BindingCheck {
	at: number;
	expected: string; // active_node 的回源主机名 (IP 类绑定为节点出口 IP)
	cf_origin: string;
	cf_status: string;
	cf_error: string;
	dnspod_value: string;
	dnspod_record_id: number;
	dnspod_error: string;
	eo_origin: string;
	eo_status: string;
	eo_error: string;
	consistent: boolean; // 两处都读到且都等于 expected
	actual_node: string; // 两处实际指向的节点 (一致时), 便于识别线下被人改过
}

export interface CredentialRow {
	id: number;
	kind: 'cloudflare' | 'dnspod';
	name: string;
	secret: string;
}

export interface CloudflareSecret {
	apiToken: string;
}

export function rowToBinding(r: Record<string, SqlStorageValue>): Binding {
	return {
		id: Number(r.id),
		kind: parseKind(r.kind),
		hostname: String(r.hostname),
		enabled: Number(r.enabled) === 1,
		cf_credential_id: Number(r.cf_credential_id),
		saas_zone_id: String(r.saas_zone_id),
		saas_zone_name: String(r.saas_zone_name),
		dnspod_credential_id: Number(r.dnspod_credential_id),
		dnspod_domain: String(r.dnspod_domain),
		record_name: String(r.record_name),
		record_line: String(r.record_line),
		pool: safeJSON<string[]>(String(r.pool), []),
		active_node: String(r.active_node),
		auto_failover: Number(r.auto_failover) === 1,
		auto_failback: Number(r.auto_failback) === 1,
		last_switch_at: Number(r.last_switch_at),
		last_check: safeJSON<BindingCheck | null>(String(r.last_check || ''), null),
		note: String(r.note),
		updated_at: Number(r.updated_at),
	};
}

function safeJSON<T>(s: string, fallback: T): T {
	try {
		return s ? (JSON.parse(s) as T) : fallback;
	} catch {
		return fallback;
	}
}

// Credentials 解密后的平台凭据, 由调用方按 id 取
export class Credentials {
	constructor(
		private sql: SqlStorage,
		private key: string | undefined,
	) {}

	private async load(id: number, kind: string): Promise<string> {
		if (!this.key) throw new Error('中控未配置 CRED_KEY, 无法解密平台凭据');
		const row = this.sql.exec('SELECT kind, secret FROM dns_credentials WHERE id = ?', id).toArray()[0];
		if (!row) throw new Error(`凭据 #${id} 不存在`);
		if (row.kind !== kind) throw new Error(`凭据 #${id} 不是 ${kind} 类型`);
		return openSecret(this.key, String(row.secret));
	}

	async cloudflare(id: number): Promise<string> {
		const s = JSON.parse(await this.load(id, 'cloudflare')) as CloudflareSecret;
		return s.apiToken;
	}

	async dnspod(id: number): Promise<TencentCredential> {
		return JSON.parse(await this.load(id, 'dnspod')) as TencentCredential;
	}
}

// originOf 节点 → 回源主机名 (小写去尾点); 没登记的节点返回空串
export type OriginLookup = (nodeId: string) => string;

function findLineRecord(records: dnspod.DnspodRecord[], b: Binding): dnspod.DnspodRecord[] {
	return records.filter((r) => r.name === b.record_name && r.line === b.record_line && (r.type === 'CNAME' || r.type === 'A' || r.type === 'AAAA'));
}

function emptyCheck(expected: string): BindingCheck {
	return {
		at: Date.now(),
		expected,
		cf_origin: '',
		cf_status: '',
		cf_error: '',
		dnspod_value: '',
		dnspod_record_id: 0,
		dnspod_error: '',
		eo_origin: '',
		eo_status: '',
		eo_error: '',
		consistent: false,
		actual_node: '',
	};
}

// readDnspodLine 读 DNSPod 线路记录, 结果写进 check.dnspod_*; 线路上必须恰好一条记录
async function readDnspodLine(b: Binding, creds: Credentials, check: BindingCheck): Promise<void> {
	try {
		const cred = await creds.dnspod(b.dnspod_credential_id);
		const recs = findLineRecord(await dnspod.listRecords(cred, b.dnspod_domain, b.record_name), b);
		if (recs.length === 0) check.dnspod_error = `线路「${b.record_line}」没有记录`;
		else if (recs.length > 1) check.dnspod_error = `线路「${b.record_line}」有 ${recs.length} 条记录, 说不清该改哪条`;
		else {
			check.dnspod_value = dnspod.normalizeHost(recs[0].value);
			check.dnspod_record_id = recs[0].id;
		}
	} catch (e) {
		check.dnspod_error = errText(e);
	}
}

export async function inspectBinding(b: Binding, creds: Credentials, expected: string, nodesByTarget: Map<string, string>): Promise<BindingCheck> {
	if (b.kind === 'edgeone') return inspectEdgeOne(b, creds, expected, nodesByTarget);
	const check = emptyCheck(expected);
	if (b.kind === 'dnspod_ip') {
		await readDnspodLine(b, creds, check);
		check.consistent = !check.dnspod_error && !!expected && check.dnspod_value === expected;
		if (!check.dnspod_error) check.actual_node = nodesByTarget.get(check.dnspod_value) ?? '';
		return check;
	}

	try {
		const token = await creds.cloudflare(b.cf_credential_id);
		const ch = await cf.findCustomHostname(token, b.saas_zone_id, b.hostname);
		if (!ch) check.cf_error = 'SaaS 区里没有这个自定义主机名';
		else {
			check.cf_origin = dnspod.normalizeHost(ch.custom_origin_server ?? '');
			check.cf_status = ch.status;
		}
	} catch (e) {
		check.cf_error = errText(e);
	}

	await readDnspodLine(b, creds, check);

	const readOK = !check.cf_error && !check.dnspod_error;
	check.consistent = readOK && !!expected && check.cf_origin === expected && check.dnspod_value === expected;
	if (readOK && check.cf_origin === check.dnspod_value) check.actual_node = nodesByTarget.get(check.cf_origin) ?? '';
	return check;
}

async function inspectEdgeOne(b: Binding, creds: Credentials, expected: string, nodesByIP: Map<string, string>): Promise<BindingCheck> {
	const check = emptyCheck(expected);
	try {
		const d = await edgeone.getDomain(await creds.dnspod(b.dnspod_credential_id), b.saas_zone_id, b.hostname);
		if (!d) check.eo_error = 'EdgeOne 站点里没有这个加速域名';
		else {
			check.eo_origin = edgeone.originOf(d);
			check.eo_status = d.DomainStatus;
		}
	} catch (e) {
		check.eo_error = errText(e);
	}
	check.consistent = !check.eo_error && !!expected && check.eo_origin === expected;
	if (!check.eo_error) check.actual_node = nodesByIP.get(check.eo_origin) ?? '';
	return check;
}

export interface SwitchResult {
	cf: 'updated' | 'unchanged' | 'failed' | 'n/a';
	dnspod: 'updated' | 'unchanged' | 'failed' | 'n/a';
	eo: 'updated' | 'unchanged' | 'failed' | 'n/a';
	errors: string[];
}

// switchBinding 把各处都改成 origin; 每处独立执行, 一处失败不阻止另一处 (半切总比不切好, 结果如实回报)
export async function switchBinding(b: Binding, creds: Credentials, origin: string): Promise<SwitchResult> {
	if (b.kind === 'edgeone') {
		const result: SwitchResult = { cf: 'n/a', dnspod: 'n/a', eo: 'unchanged', errors: [] };
		try {
			const cred = await creds.dnspod(b.dnspod_credential_id);
			const d = await edgeone.getDomain(cred, b.saas_zone_id, b.hostname);
			if (!d) throw new Error('EdgeOne 站点里没有这个加速域名');
			if (edgeone.originOf(d) !== origin) {
				await edgeone.setOrigin(cred, b.saas_zone_id, b.hostname, origin);
				result.eo = 'updated';
			}
		} catch (e) {
			result.eo = 'failed';
			result.errors.push(`EdgeOne: ${errText(e)}`);
		}
		return result;
	}
	if (b.kind === 'dnspod_ip') {
		const result: SwitchResult = { cf: 'n/a', dnspod: 'unchanged', eo: 'n/a', errors: [] };
		await switchDnspodLine(b, creds, origin.includes(':') ? 'AAAA' : 'A', origin, result);
		return result;
	}
	const result: SwitchResult = { cf: 'unchanged', dnspod: 'unchanged', eo: 'n/a', errors: [] };

	try {
		const token = await creds.cloudflare(b.cf_credential_id);
		const ch = await cf.findCustomHostname(token, b.saas_zone_id, b.hostname);
		if (!ch) throw new Error('SaaS 区里没有这个自定义主机名');
		if (dnspod.normalizeHost(ch.custom_origin_server ?? '') !== origin) {
			await cf.updateCustomOrigin(token, b.saas_zone_id, ch.id, origin);
			result.cf = 'updated';
		}
	} catch (e) {
		result.cf = 'failed';
		result.errors.push(`CF 回源: ${errText(e)}`);
	}

	await switchDnspodLine(b, creds, 'CNAME', origin, result);
	return result;
}

// switchDnspodLine 把线路上唯一那条记录就地改成 type + value (类型不同也直接改, 如 CNAME → A)
async function switchDnspodLine(b: Binding, creds: Credentials, type: 'CNAME' | 'A' | 'AAAA', value: string, result: SwitchResult): Promise<void> {
	try {
		const cred = await creds.dnspod(b.dnspod_credential_id);
		const recs = findLineRecord(await dnspod.listRecords(cred, b.dnspod_domain, b.record_name), b);
		if (recs.length !== 1) throw new Error(recs.length === 0 ? `线路「${b.record_line}」没有记录` : `线路「${b.record_line}」有多条记录`);
		const rec = recs[0];
		if (dnspod.normalizeHost(rec.value) !== value || rec.type !== type) {
			await dnspod.modifyRecord(cred, b.dnspod_domain, { id: rec.id, name: rec.name, type, line: rec.line, value, ttl: rec.ttl });
			result.dnspod = 'updated';
		}
	} catch (e) {
		result.dnspod = 'failed';
		result.errors.push(`DNSPod: ${errText(e)}`);
	}
}

export function errText(e: unknown): string {
	return e instanceof Error ? e.message : String(e);
}
