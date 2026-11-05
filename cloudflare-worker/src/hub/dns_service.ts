/**
 * DNS 绑定的增删改查、巡检与切换 (运行在 Hub DO 内, 共用其 SQLite)
 */
import type { Env } from '../env';
import { sealSecret } from '../lib/crypto';
import * as cf from '../dns/cloudflare';
import * as dnspod from '../dns/dnspod';
import {
	Credentials,
	errText,
	inspectBinding,
	rowToBinding,
	switchBinding,
	type Binding,
	type BindingCheck,
	type SwitchResult,
} from '../dns/binding';

export type EventFn = (level: 'info' | 'warning' | 'error', kind: string, subject: string, message: string, notify?: boolean) => void;

export interface CredentialInput {
	id?: number;
	kind: 'cloudflare' | 'dnspod';
	name: string;
	apiToken?: string;
	secretId?: string;
	secretKey?: string;
}

export interface BindingInput {
	id?: number;
	hostname: string;
	enabled?: boolean;
	cf_credential_id: number;
	saas_zone_id: string;
	saas_zone_name?: string;
	dnspod_credential_id: number;
	dnspod_domain: string;
	record_name?: string;
	record_line?: string;
	pool: string[];
	active_node?: string;
	auto_failover?: boolean;
	auto_failback?: boolean;
	note?: string;
}

export class DnsService {
	constructor(
		private sql: SqlStorage,
		private env: Env,
		private event: EventFn,
	) {}

	creds(): Credentials {
		return new Credentials(this.sql, this.env.CRED_KEY);
	}

	// ---------- 节点回源主机名 ----------

	originOf = (nodeId: string): string => {
		const r = this.sql.exec('SELECT origin_host FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		return r ? String(r.origin_host) : '';
	};

	nodesByOrigin(): Map<string, string> {
		const m = new Map<string, string>();
		for (const r of this.sql.exec("SELECT node_id, origin_host FROM nodes WHERE origin_host != ''").toArray()) {
			m.set(String(r.origin_host), String(r.node_id));
		}
		return m;
	}

	// ---------- 凭据 ----------

	listCredentials(): unknown[] {
		return this.sql.exec('SELECT id, kind, name, created_at, updated_at FROM dns_credentials ORDER BY id').toArray();
	}

	async saveCredential(input: CredentialInput): Promise<number> {
		if (!this.env.CRED_KEY) throw new Error('中控未配置 CRED_KEY');
		if (input.kind !== 'cloudflare' && input.kind !== 'dnspod') throw new Error('kind 只能是 cloudflare / dnspod');
		const name = (input.name ?? '').trim();
		if (!name) throw new Error('名称不能为空');

		let secret: string | null = null;
		if (input.kind === 'cloudflare' && input.apiToken?.trim()) {
			secret = JSON.stringify({ apiToken: input.apiToken.trim() });
		} else if (input.kind === 'dnspod' && input.secretId?.trim() && input.secretKey?.trim()) {
			secret = JSON.stringify({ secretId: input.secretId.trim(), secretKey: input.secretKey.trim() });
		}

		const now = Date.now();
		if (input.id) {
			const row = this.sql.exec('SELECT kind FROM dns_credentials WHERE id = ?', input.id).toArray()[0];
			if (!row) throw new Error('凭据不存在');
			if (row.kind !== input.kind) throw new Error('不能修改凭据类型');
			if (secret) {
				this.sql.exec('UPDATE dns_credentials SET name = ?, secret = ?, updated_at = ? WHERE id = ?', name, await sealSecret(this.env.CRED_KEY, secret), now, input.id);
			} else {
				this.sql.exec('UPDATE dns_credentials SET name = ?, updated_at = ? WHERE id = ?', name, now, input.id);
			}
			return input.id;
		}
		if (!secret) throw new Error(input.kind === 'cloudflare' ? '缺少 API Token' : '缺少 SecretId / SecretKey');
		return Number(
			this.sql
				.exec(
					'INSERT INTO dns_credentials (kind, name, secret, created_at, updated_at) VALUES (?, ?, ?, ?, ?) RETURNING id',
					input.kind,
					name,
					await sealSecret(this.env.CRED_KEY, secret),
					now,
					now,
				)
				.one().id,
		);
	}

	deleteCredential(id: number): void {
		const used = this.sql.exec('SELECT hostname FROM dns_bindings WHERE cf_credential_id = ? OR dnspod_credential_id = ?', id, id).toArray();
		if (used.length) throw new Error(`凭据仍被 ${used.map((r) => r.hostname).join(', ')} 使用`);
		this.sql.exec('DELETE FROM dns_credentials WHERE id = ?', id);
	}

	// discover 凭据可见的资源, 供表单下拉选择
	async discover(credentialId: number): Promise<{ kind: string; zones?: cf.Zone[]; domains?: string[] }> {
		const row = this.sql.exec('SELECT kind FROM dns_credentials WHERE id = ?', credentialId).toArray()[0];
		if (!row) throw new Error('凭据不存在');
		if (row.kind === 'cloudflare') return { kind: 'cloudflare', zones: await cf.listZones(await this.creds().cloudflare(credentialId)) };
		return { kind: 'dnspod', domains: await dnspod.listDomains(await this.creds().dnspod(credentialId)) };
	}

	// suggestOrigins 按节点出口 IP 在 SaaS 区里找对应的橙云记录, 给"登记回源主机名"做预填
	async suggestOrigins(credentialId: number, zoneId: string): Promise<Record<string, string[]>> {
		const records = await cf.listAddressRecords(await this.creds().cloudflare(credentialId), zoneId);
		const out: Record<string, string[]> = {};
		for (const n of this.sql.exec('SELECT node_id, ipv4, ipv6 FROM nodes').toArray()) {
			const ips = [String(n.ipv4), String(n.ipv6)].filter(Boolean);
			const hits = records.filter((r) => r.proxied && ips.includes(r.content)).map((r) => r.name.toLowerCase());
			if (hits.length) out[String(n.node_id)] = [...new Set(hits)];
		}
		return out;
	}

	// ---------- 绑定 ----------

	listBindings(): Binding[] {
		return this.sql.exec('SELECT * FROM dns_bindings ORDER BY hostname').toArray().map(rowToBinding);
	}

	getBinding(id: number): Binding {
		const r = this.sql.exec('SELECT * FROM dns_bindings WHERE id = ?', id).toArray()[0];
		if (!r) throw new Error('绑定不存在');
		return rowToBinding(r);
	}

	bindingsUsingNode(nodeId: string): string[] {
		return this.listBindings()
			.filter((b) => b.pool.includes(nodeId) || b.active_node === nodeId)
			.map((b) => b.hostname);
	}

	saveBinding(input: BindingInput): number {
		const hostname = (input.hostname ?? '').trim().toLowerCase().replace(/\.$/, '');
		if (!/^[a-z0-9.-]+\.[a-z]{2,}$/.test(hostname)) throw new Error('访问域名格式不对');
		if (!input.cf_credential_id || !input.saas_zone_id) throw new Error('缺少 CF 凭据或 SaaS 区');
		if (!input.dnspod_credential_id || !input.dnspod_domain) throw new Error('缺少 DNSPod 凭据或域名');
		const pool = [...new Set((input.pool ?? []).map((s) => s.trim()).filter(Boolean))];
		if (pool.length === 0) throw new Error('节点池不能为空');
		for (const id of pool) {
			if (!this.originOf(id)) throw new Error(`节点 ${id} 未登记回源主机名`);
		}
		const active = input.active_node && pool.includes(input.active_node) ? input.active_node : pool[0];
		const now = Date.now();
		const args = [
			hostname,
			input.enabled === false ? 0 : 1,
			input.cf_credential_id,
			input.saas_zone_id,
			input.saas_zone_name ?? '',
			input.dnspod_credential_id,
			input.dnspod_domain.trim().toLowerCase(),
			(input.record_name ?? '@').trim() || '@',
			(input.record_line ?? '默认').trim() || '默认',
			JSON.stringify(pool),
			active,
			input.auto_failover === false ? 0 : 1,
			input.auto_failback === false ? 0 : 1,
			(input.note ?? '').slice(0, 200),
			now,
		];
		if (input.id) {
			this.getBinding(input.id);
			this.sql.exec(
				`UPDATE dns_bindings SET hostname=?, enabled=?, cf_credential_id=?, saas_zone_id=?, saas_zone_name=?,
				 dnspod_credential_id=?, dnspod_domain=?, record_name=?, record_line=?, pool=?, active_node=?,
				 auto_failover=?, auto_failback=?, note=?, updated_at=? WHERE id=?`,
				...args,
				input.id,
			);
			return input.id;
		}
		return Number(
			this.sql
				.exec(
					`INSERT INTO dns_bindings (hostname, enabled, cf_credential_id, saas_zone_id, saas_zone_name,
					 dnspod_credential_id, dnspod_domain, record_name, record_line, pool, active_node,
					 auto_failover, auto_failback, note, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
					...args,
				)
				.one().id,
		);
	}

	deleteBinding(id: number): void {
		this.sql.exec('DELETE FROM dns_bindings WHERE id = ?', id);
	}

	// check 读取两处线上实际值并落 last_check; 由一致变为不一致时记一次漂移事件
	async check(id: number, notifyDrift = false): Promise<BindingCheck> {
		const b = this.getBinding(id);
		const result = await inspectBinding(b, this.creds(), this.originOf, this.nodesByOrigin());
		const wasConsistent = b.last_check?.consistent ?? true;
		const readFailed = !!(result.cf_error || result.dnspod_error);
		if (wasConsistent && !result.consistent && !readFailed) {
			this.event(
				'warning',
				'drift',
				b.hostname,
				`期望指向 ${result.expected} (${b.active_node}), 实际 CF=${result.cf_origin || '-'} DNSPod=${result.dnspod_value || '-'}`,
				notifyDrift,
			);
		}
		this.sql.exec('UPDATE dns_bindings SET last_check = ? WHERE id = ?', JSON.stringify(result), id);
		return result;
	}

	// switchTo 把绑定切到 nodeId; 至少一处改成功即更新 active_node (结果如实记录, 巡检会标出半切状态)
	async switchTo(id: number, nodeId: string, kind: 'switch' | 'failover' | 'failback', reason: string): Promise<SwitchResult & { check: BindingCheck }> {
		const b = this.getBinding(id);
		const origin = this.originOf(nodeId);
		if (!origin) throw new Error(`节点 ${nodeId} 未登记回源主机名`);

		let result: SwitchResult;
		try {
			result = await switchBinding(b, this.creds(), origin);
		} catch (e) {
			result = { cf: 'failed', dnspod: 'failed', errors: [errText(e)] };
		}

		const anyOK = result.cf !== 'failed' || result.dnspod !== 'failed';
		// last_switch_at 记"最近一次尝试": 全部失败也要推进, 否则自动切回每 20s 重试一次并刷告警
		this.sql.exec('UPDATE dns_bindings SET last_switch_at = ? WHERE id = ?', Date.now(), id);
		if (anyOK) {
			// 手动切换即表达新的首选: 目标挪到池首, 否则自动切回会在 10 分钟后把人工决定撤销
			const pool = kind === 'switch' ? [nodeId, ...b.pool.filter((n) => n !== nodeId)] : b.pool;
			this.sql.exec(
				'UPDATE dns_bindings SET active_node = ?, pool = ?, last_switch_at = ? WHERE id = ?',
				nodeId,
				JSON.stringify(pool),
				Date.now(),
				id,
			);
		}
		const detail = `${b.active_node || '-'} → ${nodeId} (${origin}); CF ${result.cf}, DNSPod ${result.dnspod}${reason ? `; ${reason}` : ''}`;
		if (result.errors.length) {
			this.event('error', 'switch_failed', b.hostname, `${detail}; ${result.errors.join('; ')}`, true);
		} else {
			this.event(kind === 'switch' ? 'info' : 'warning', kind, b.hostname, detail, true);
		}
		const check = await this.check(id);
		return { ...result, check };
	}
}
