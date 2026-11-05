/**
 * 发布相关的存储: 节点程序版本 (CI 登记) 与 Dokploy 实例 (加密存 API Key)
 */
import { openSecret, sealSecret } from '../lib/crypto';
import { listApps, type DokployApp, type DokployServer } from '../deploy/dokploy';

const KEEP_RELEASES = 20; // 与 CI 在 GitHub 上保留的 build-* Release 数一致, 多留的版本已无法下载

export interface ReleaseAsset {
	url: string;
	sha256: string;
	size: number;
}

export interface Release {
	version: string;
	build_time: number;
	title: string;
	assets: Record<string, ReleaseAsset>;
	created_at: number;
}

export interface DokployServerInput {
	id?: number;
	name: string;
	url: string;
	apiKey?: string;
}

const ARCHES = new Set(['amd64', 'arm64']);
const ASSET_URL = /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/releases\/download\/[\w.-]+\/[\w.-]+$/;

export class ReleaseStore {
	constructor(
		private sql: SqlStorage,
		private credKey: string | undefined,
	) {}

	// ---------- 版本 ----------

	register(input: Release): Release {
		const version = String(input.version ?? '').trim();
		if (!/^[\w.-]{1,40}$/.test(version)) throw new Error('version 格式不对');
		const assets: Record<string, ReleaseAsset> = {};
		for (const [arch, a] of Object.entries(input.assets ?? {})) {
			if (!ARCHES.has(arch)) continue;
			// 下载地址只认 GitHub Release, 中控代理下载时不会被引去任意地址
			if (!ASSET_URL.test(String(a?.url ?? ''))) throw new Error(`${arch} 下载地址必须是 GitHub Release 资源`);
			if (!/^[0-9a-f]{64}$/i.test(String(a?.sha256 ?? ''))) throw new Error(`${arch} sha256 格式不对`);
			assets[arch] = { url: a.url, sha256: a.sha256.toLowerCase(), size: Number(a.size) || 0 };
		}
		if (Object.keys(assets).length === 0) throw new Error('没有可用的二进制');
		const now = Date.now();
		const rel: Release = { version, build_time: Number(input.build_time) || 0, title: String(input.title ?? '').slice(0, 200), assets, created_at: now };
		this.sql.exec(
			'INSERT OR REPLACE INTO releases (version, build_time, title, assets, created_at) VALUES (?, ?, ?, ?, ?)',
			rel.version,
			rel.build_time,
			rel.title,
			JSON.stringify(rel.assets),
			now,
		);
		this.sql.exec(
			'DELETE FROM releases WHERE version NOT IN (SELECT version FROM releases ORDER BY build_time DESC, created_at DESC LIMIT ?)',
			KEEP_RELEASES,
		);
		return rel;
	}

	list(limit = 20): Release[] {
		return this.sql
			.exec('SELECT * FROM releases ORDER BY build_time DESC, created_at DESC LIMIT ?', limit)
			.toArray()
			.map(toRelease);
	}

	get(version: string): Release | null {
		const r = this.sql.exec('SELECT * FROM releases WHERE version = ?', version).toArray()[0];
		return r ? toRelease(r) : null;
	}

	// ---------- Dokploy 实例 ----------

	listServers(): { id: number; name: string; url: string; updated_at: number }[] {
		return this.sql.exec('SELECT id, name, url, updated_at FROM dokploy_servers ORDER BY id').toArray() as never;
	}

	async saveServer(input: DokployServerInput): Promise<number> {
		if (!this.credKey) throw new Error('中控未配置 CRED_KEY');
		const name = String(input.name ?? '').trim();
		const url = String(input.url ?? '').trim().replace(/\/+$/, '');
		if (!name) throw new Error('名称不能为空');
		if (!/^https:\/\/[^\s/]+/.test(url)) throw new Error('地址需以 https:// 开头 (API Key 不走明文)');
		const key = input.apiKey?.trim();
		const now = Date.now();
		if (input.id) {
			if (!this.sql.exec('SELECT id FROM dokploy_servers WHERE id = ?', input.id).toArray().length) throw new Error('Dokploy 实例不存在');
			if (key) {
				this.sql.exec(
					'UPDATE dokploy_servers SET name = ?, url = ?, secret = ?, updated_at = ? WHERE id = ?',
					name,
					url,
					await sealSecret(this.credKey, key),
					now,
					input.id,
				);
			} else {
				this.sql.exec('UPDATE dokploy_servers SET name = ?, url = ?, updated_at = ? WHERE id = ?', name, url, now, input.id);
			}
			return input.id;
		}
		if (!key) throw new Error('缺少 API Key');
		return Number(
			this.sql
				.exec(
					'INSERT INTO dokploy_servers (name, url, secret, created_at, updated_at) VALUES (?, ?, ?, ?, ?) RETURNING id',
					name,
					url,
					await sealSecret(this.credKey, key),
					now,
					now,
				)
				.one().id,
		);
	}

	deleteServer(id: number): void {
		const used = this.sql.exec('SELECT node_id FROM nodes WHERE dokploy_server_id = ?', id).toArray();
		if (used.length) throw new Error(`仍有节点使用该实例: ${used.map((r) => r.node_id).join(', ')}`);
		this.sql.exec('DELETE FROM dokploy_servers WHERE id = ?', id);
	}

	async server(id: number): Promise<DokployServer> {
		if (!this.credKey) throw new Error('中控未配置 CRED_KEY, 无法解密 Dokploy API Key');
		const r = this.sql.exec('SELECT url, secret FROM dokploy_servers WHERE id = ?', id).toArray()[0];
		if (!r) throw new Error(`Dokploy 实例 #${id} 不存在`);
		return { url: String(r.url), apiKey: await openSecret(this.credKey, String(r.secret)) };
	}

	async apps(id: number): Promise<DokployApp[]> {
		return listApps(await this.server(id));
	}
}

function toRelease(r: Record<string, SqlStorageValue>): Release {
	return {
		version: String(r.version),
		build_time: Number(r.build_time),
		title: String(r.title),
		assets: JSON.parse(String(r.assets)) as Record<string, ReleaseAsset>,
		created_at: Number(r.created_at),
	};
}
