/**
 * DNS 绑定的增删改查、巡检与切换 (运行在 Hub DO 内, 共用其 SQLite)
 */
import type { Env } from '../env';
import { sealSecret } from '../lib/crypto';
import * as cf from '../dns/cloudflare';
import * as dnspod from '../dns/dnspod';
import * as edgeone from '../dns/edgeone';
import {
	Credentials,
	errText,
	inspectBinding,
	parseKind,
	rowToBinding,
	switchBinding,
	switchDnspodOnly,
	switchSucceeded,
	usesDnspodLine,
	usesNodeIP,
	type Binding,
	type BindingCheck,
	type BindingKind,
	type SwitchResult,
} from '../dns/binding';
import { applyDomain, DomainStore, missingFor, normalizeHostname, type DnsDomain, type DomainInput } from './dns_domains';

const LINES = ['默认', '境内', '境外'];

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
	kind?: BindingKind;
	hostname: string; // 所属域名
	enabled?: boolean;
	record_line?: string;
	dnspod_target?: string;
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
	) {
		this.domains = new DomainStore(sql);
	}

	private domains: DomainStore;

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

	// targetOf 绑定切到某节点时写入的值: CF + DNSPod 用回源主机名, EdgeOne / DNSPod 纯 IP 用节点出口 IP
	targetOf(kind: BindingKind, nodeId: string): string {
		if (!usesNodeIP(kind)) return this.originOf(nodeId);
		const r = this.sql.exec('SELECT ipv4, ipv6 FROM nodes WHERE node_id = ?', nodeId).toArray()[0];
		return r ? String(r.ipv4 || r.ipv6 || '').toLowerCase() : '';
	}

	private nodesByTarget(kind: BindingKind): Map<string, string> {
		if (!usesNodeIP(kind)) return this.nodesByOrigin();
		const m = new Map<string, string>();
		for (const r of this.sql.exec('SELECT node_id, ipv4, ipv6 FROM nodes').toArray()) {
			for (const ip of [r.ipv4, r.ipv6]) if (ip) m.set(String(ip).toLowerCase(), String(r.node_id));
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
		const used = this.domains.credentialUsers(id);
		if (used.length) throw new Error(`凭据仍被 ${used.join(', ')} 使用`);
		this.sql.exec('DELETE FROM dns_credentials WHERE id = ?', id);
	}

	// discover 凭据可见的资源, 供表单下拉选择
	async discover(credentialId: number): Promise<{ kind: string; zones?: cf.Zone[]; domains?: string[]; eo_zones?: edgeone.EdgeOneZone[] }> {
		const row = this.sql.exec('SELECT kind FROM dns_credentials WHERE id = ?', credentialId).toArray()[0];
		if (!row) throw new Error('凭据不存在');
		if (row.kind === 'cloudflare') return { kind: 'cloudflare', zones: await cf.listZones(await this.creds().cloudflare(credentialId)) };
		// 腾讯云密钥同时用于 DNSPod 与 EdgeOne; 账号可能只开通其中一个, 各自失败不影响另一项
		const cred = await this.creds().dnspod(credentialId);
		const [domains, eoZones] = await Promise.all([dnspod.listDomains(cred).catch(() => []), edgeone.listZones(cred).catch(() => [])]);
		return { kind: 'dnspod', domains, eo_zones: eoZones };
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

	// ---------- 域名 ----------

	listDomains(): DnsDomain[] {
		return this.domains.list();
	}

	saveDomain(input: DomainInput): number {
		return this.domains.save(input);
	}

	deleteDomain(id: number): void {
		this.domains.delete(id);
	}

	// ---------- 线路 (绑定) ----------

	listBindings(): Binding[] {
		const domains = this.domains.byHostname();
		return this.sql
			.exec('SELECT * FROM dns_bindings ORDER BY hostname')
			.toArray()
			.map((r) => {
				const b = rowToBinding(r);
				return applyDomain(b, domains.get(b.hostname));
			});
	}

	getBinding(id: number): Binding {
		const r = this.sql.exec('SELECT * FROM dns_bindings WHERE id = ?', id).toArray()[0];
		if (!r) throw new Error('线路不存在');
		const b = rowToBinding(r);
		return applyDomain(b, this.domains.get(b.hostname));
	}

	bindingsUsingNode(nodeId: string): string[] {
		return this.listBindings()
			.filter((b) => b.pool.includes(nodeId) || b.active_node === nodeId)
			.map((b) => `${b.hostname} ${b.record_line}`);
	}

	// saveBinding 保存一条线路: 账号配置取自所属域名, 这里只存走法、线路、节点池与开关
	saveBinding(input: BindingInput): number {
		const kind = parseKind(input.kind);
		const domain = this.domains.get(normalizeHostname(input.hostname));
		if (!domain) throw new Error('域名不存在, 先添加域名');
		const missing = missingFor(domain, kind);
		if (missing) throw new Error(missing);
		const hostname = domain.hostname;
		const recordLine = LINES.includes(input.record_line ?? '') ? String(input.record_line) : '默认';
		const dnspodTarget = kind === 'cf_dnspod' ? normalizeHostname(input.dnspod_target) : '';
		if (dnspodTarget && !/^[a-z0-9.-]+\.[a-z]{2,}$/.test(dnspodTarget)) throw new Error('优选域名格式不对');
		if (dnspodTarget === hostname) throw new Error('优选域名不能是访问域名本身');
		this.assertNoConflict(input.id ?? 0, kind, hostname, recordLine);
		// 优选线路没有自己的节点池: 同域名有普通 CF 线路时, 跟随它的 CF 回源 (节点池 / 当前节点 / 自动切换都取它的)
		const leader = dnspodTarget ? this.cfLeader(hostname, input.id ?? 0) : undefined;
		if (leader) {
			input = { ...input, pool: leader.pool, active_node: leader.active_node, auto_failover: leader.auto_failover, auto_failback: leader.auto_failback };
		}
		const pool = [...new Set((input.pool ?? []).map((s) => s.trim()).filter(Boolean))];
		if (pool.length === 0) throw new Error('节点池不能为空');
		for (const id of pool) {
			if (!this.targetOf(kind, id)) throw new Error(usesNodeIP(kind) ? `节点 ${id} 没有上报出口 IP` : `节点 ${id} 未登记回源主机名`);
		}
		// 同域名已有 CF 线路时, CF 回源当前指向它们的当前节点, 新线路沿用
		const sibling = kind === 'cf_dnspod' ? this.listBindings().find((o) => o.kind === 'cf_dnspod' && o.hostname === hostname && o.id !== (input.id ?? 0)) : undefined;
		const active = [input.active_node, sibling?.active_node].find((n) => n && pool.includes(n)) ?? pool[0];
		const eo = kind === 'edgeone';
		const now = Date.now();
		// 账号列以域名为准 (读取时覆盖), 这里照写一份便于直接查表
		const args = [
			kind,
			hostname,
			input.enabled === false ? 0 : 1,
			kind === 'cf_dnspod' ? domain.cf_credential_id : 0,
			eo ? domain.eo_zone_id : kind === 'cf_dnspod' ? domain.saas_zone_id : '',
			eo ? domain.eo_zone_name : kind === 'cf_dnspod' ? domain.saas_zone_name : '',
			domain.tc_credential_id,
			usesDnspodLine(kind) ? domain.dnspod_domain : '',
			domain.record_name,
			recordLine,
			dnspodTarget,
			JSON.stringify(pool),
			active,
			input.auto_failover === false ? 0 : 1,
			input.auto_failback === false ? 0 : 1,
			(input.note ?? '').slice(0, 200),
			now,
		];
		if (input.id) {
			// 改了走法、线路或优选域名, 旧的巡检结果描述的是另一处解析, 清掉等下次检查
			const old = this.getBinding(input.id);
			if (old.kind !== kind || old.record_line !== recordLine || old.dnspod_target !== dnspodTarget) {
				this.sql.exec("UPDATE dns_bindings SET last_check = '' WHERE id = ?", input.id);
			}
			this.sql.exec(
				`UPDATE dns_bindings SET kind=?, hostname=?, enabled=?, cf_credential_id=?, saas_zone_id=?, saas_zone_name=?,
				 dnspod_credential_id=?, dnspod_domain=?, record_name=?, record_line=?, dnspod_target=?, pool=?, active_node=?,
				 auto_failover=?, auto_failback=?, note=?, updated_at=? WHERE id=?`,
				...args,
				input.id,
			);
			this.syncCfGroup(hostname, kind, args);
			return input.id;
		}
		const id = Number(
			this.sql
				.exec(
					`INSERT INTO dns_bindings (kind, hostname, enabled, cf_credential_id, saas_zone_id, saas_zone_name,
					 dnspod_credential_id, dnspod_domain, record_name, record_line, dnspod_target, pool, active_node,
					 auto_failover, auto_failback, note, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
					...args,
				)
				.one().id,
		);
		this.syncCfGroup(hostname, kind, args);
		return id;
	}

	// syncCfGroup CF 线路共用一份回源, 节点池 / 当前节点 / 自动切换开关以最近保存的那条为准同步到同域名其他 CF 线路
	private syncCfGroup(hostname: string, kind: BindingKind, args: unknown[]): void {
		if (kind !== 'cf_dnspod') return;
		const [pool, active, failover, failback] = args.slice(11, 15);
		this.sql.exec(
			"UPDATE dns_bindings SET pool = ?, active_node = ?, auto_failover = ?, auto_failback = ? WHERE hostname = ? AND kind = 'cf_dnspod'",
			pool as string,
			active as string,
			failover as number,
			failback as number,
			hostname,
		);
	}

	// assertNoConflict 一个域名每条线路只有一种走法; EdgeOne 源站每个域名只有一份, 只能给一条线路用
	// (CF 线路可以多条, 共用 CF 回源与节点池, 见 cfGroup)
	private assertNoConflict(id: number, kind: BindingKind, hostname: string, line: string): void {
		for (const o of this.listBindings()) {
			if (o.id === id || o.hostname !== hostname) continue;
			if (o.record_line === line) throw new Error(`${hostname} 的「${line}」线路已配置`);
			if (o.kind === 'edgeone' && kind === 'edgeone') throw new Error(`EdgeOne 源站每个域名只有一份, 已用在「${o.record_line}」线路`);
		}
	}

	deleteBinding(id: number): void {
		this.sql.exec('DELETE FROM dns_bindings WHERE id = ?', id);
	}

	// check 读取两处线上实际值并落 last_check; 由一致变为不一致时记一次漂移事件
	async check(id: number, notifyDrift = false): Promise<BindingCheck> {
		const b = this.getBinding(id);
		const result = await inspectBinding(b, this.creds(), this.targetOf(b.kind, b.active_node), this.nodesByTarget(b.kind));
		const wasConsistent = b.last_check?.consistent ?? true;
		const readFailed = !!(result.cf_error || result.dnspod_error || result.eo_error);
		if (wasConsistent && !result.consistent && !readFailed) {
			this.event(
				'warning',
				'drift',
				b.hostname,
				b.kind === 'edgeone'
					? `[EdgeOne] 期望回源 ${result.expected} (${b.active_node}), 实际 ${result.eo_origin || '-'}`
					: b.kind === 'dnspod_ip'
						? `[${b.record_line} IP] 期望解析到 ${result.expected} (${b.active_node}), 实际 ${result.dnspod_value || '-'}`
						: b.dnspod_target
							? `期望 CF 回源 ${result.expected} (${b.active_node}) + DNSPod CNAME ${b.dnspod_target}, 实际 CF=${result.cf_origin || '-'} DNSPod=${result.dnspod_value || '-'}`
							: `期望指向 ${result.expected} (${b.active_node}), 实际 CF=${result.cf_origin || '-'} DNSPod=${result.dnspod_value || '-'}`,
				notifyDrift,
			);
		}
		this.sql.exec('UPDATE dns_bindings SET last_check = ? WHERE id = ?', JSON.stringify(result), id);
		return result;
	}

	// cfLeader 同域名下的普通 CF 回源线路 (没填优选域名), 优选线路跟随它
	cfLeader(hostname: string, excludeId = 0): Binding | undefined {
		return this.listBindings().find((o) => o.kind === 'cf_dnspod' && !o.dnspod_target && o.hostname === hostname && o.id !== excludeId);
	}

	// cfGroup 同域名下全部 CF 线路 (共用一份 CF SaaS 回源, 因此共用节点池与当前节点); 非 CF 线路只有自己
	private cfGroup(b: Binding): Binding[] {
		if (b.kind !== 'cf_dnspod') return [b];
		// 停用的线路不跟着改 DNSPod 记录
		return [b, ...this.listBindings().filter((o) => o.kind === 'cf_dnspod' && o.enabled && o.hostname === b.hostname && o.id !== b.id)];
	}

	// switchTo 把线路切到 nodeId; 决定流量的那一处改成功即更新 active_node (见 switchSucceeded, 其余失败如实记录)。
	// CF 线路连同同域名的其他 CF 线路一起切: CF 回源改一次, 各 DNSPod 线路各改各的。kind=sync 是把当前节点重新推到线上
	async switchTo(
		id: number,
		nodeId: string,
		kind: 'switch' | 'failover' | 'failback' | 'sync',
		reason: string,
		notifyFailure = true,
	): Promise<SwitchResult & { check: BindingCheck }> {
		const b = this.getBinding(id);
		const origin = this.targetOf(b.kind, nodeId);
		if (!origin) throw new Error(usesNodeIP(b.kind) ? `节点 ${nodeId} 没有上报出口 IP` : `节点 ${nodeId} 未登记回源主机名`);
		const group = this.cfGroup(b);

		let result: SwitchResult;
		try {
			result = await switchBinding(b, this.creds(), origin);
		} catch (e) {
			result = { cf: 'failed', dnspod: 'failed', eo: 'failed', errors: [errText(e)] };
		}
		for (const sib of group.slice(1)) {
			const r = await switchDnspodOnly(sib, this.creds(), origin);
			if (r.dnspod === 'failed' || result.dnspod === 'failed') result.dnspod = 'failed';
			else if (r.dnspod === 'updated') result.dnspod = 'updated';
			result.errors.push(...r.errors.map((e) => `[${sib.record_line}] ${e}`));
		}

		const anyOK = switchSucceeded(b.kind, result);
		const now = Date.now();
		// 手动切换即表达新的首选: 目标挪到池首, 否则自动切回会在 10 分钟后把人工决定撤销
		const pool = kind === 'switch' ? [nodeId, ...b.pool.filter((n) => n !== nodeId)] : b.pool;
		for (const g of group) {
			// last_switch_at 记"最近一次尝试": 全部失败也要推进, 否则自动切回每 20s 重试一次并刷告警
			if (anyOK) this.sql.exec('UPDATE dns_bindings SET active_node = ?, pool = ?, last_switch_at = ? WHERE id = ?', nodeId, JSON.stringify(pool), now, g.id);
			else this.sql.exec('UPDATE dns_bindings SET last_switch_at = ? WHERE id = ?', now, g.id);
		}
		const lines = group.map((g) => g.record_line).join('+');
		const parts = b.kind === 'edgeone' ? `EdgeOne ${result.eo}` : b.kind === 'dnspod_ip' ? `DNSPod ${result.dnspod}` : `CF ${result.cf}, DNSPod ${result.dnspod}`;
		const tag = b.kind === 'edgeone' ? `[${b.record_line} EdgeOne] ` : b.kind === 'dnspod_ip' ? `[${b.record_line} IP] ` : `[${lines} CF] `;
		const arrow = kind === 'sync' ? `同步 ${nodeId}` : `${b.active_node || '-'} → ${nodeId}`;
		const detail = `${tag}${arrow} (${origin}); ${parts}${reason ? `; ${reason}` : ''}`;
		if (result.errors.length) {
			this.event('error', 'switch_failed', b.hostname, `${detail}; ${result.errors.join('; ')}`, notifyFailure);
		} else if (kind === 'sync') {
			this.event('info', 'sync', b.hostname, detail, false);
		} else {
			this.event(kind === 'switch' ? 'info' : 'warning', kind, b.hostname, detail, true);
		}
		for (const g of group.slice(1)) await this.check(g.id).catch(() => undefined);
		const check = await this.check(id);
		return { ...result, check };
	}
}
