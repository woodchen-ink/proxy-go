/**
 * 路径配置的权威读写 (D1 config_maps / config_other)
 *
 * 中控是唯一写入方: 节点只读, 经心跳 (或中控下发的 pulse 指令) 热应用。
 * 存储形态与节点 Go 侧 pkg/sync/d1_converter.go 保持一致:
 *   - DefaultTarget / Enabled / ExtensionMap / CacheConfig 有专属列
 *   - 其余字段 (EXTRA_KEYS) 序列化进 extra_config JSON 列, 零值不写
 *   - 非 MAP 的顶层字段逐 key 存 config_other (JSON); `__` 前缀是同步层内部状态
 * 写入在一个 D1 batch (事务) 里完成, 并单调递增 __config_version。
 */
import * as db from '../db';
import type { Env } from '../env';
import { HttpError } from '../lib/http';

const EXTRA_KEYS = ['DefaultTargets', 'RedirectMode', 'CFImageOpt', 'RefererBan', 'RefererRedirect'] as const;
const VERSION_KEY = '__config_version';
const MIRROR_MARKER = 'mirror';

export type PathConfig = Record<string, unknown> & { DefaultTarget?: string; Enabled?: boolean };

export interface ProxyConfig {
	MAP: Record<string, PathConfig>;
	CDN: { Providers: CDNProvider[] };
	[key: string]: unknown;
}

export interface CDNProvider {
	ID: string;
	Name: string;
	Type: string;
	Enabled: boolean;
	Credentials: Record<string, string>;
}

const PATH_DEFAULTS: PathConfig = {
	DefaultTarget: '',
	ExtensionMap: null,
	RedirectMode: false,
	Enabled: true,
	CacheConfig: null,
	RefererBan: null,
	RefererRedirect: null,
	CFImageOpt: false,
};

function parseJSON<T>(s: string | null | undefined, fallback: T): T {
	if (!s) return fallback;
	try {
		return JSON.parse(s) as T;
	} catch {
		return fallback;
	}
}

export async function readConfig(env: Env): Promise<{ config: ProxyConfig; version: number }> {
	const [maps, others] = await Promise.all([db.getConfigMaps(env.DB, false), db.getConfigOther(env.DB)]);
	const MAP: Record<string, PathConfig> = {};
	for (const m of maps) {
		const pc: PathConfig = { ...PATH_DEFAULTS, DefaultTarget: m.default_target, Enabled: Number(m.enabled) === 1 };
		const ext = parseJSON<unknown[] | null>(m.extension_rules, null);
		if (Array.isArray(ext)) pc.ExtensionMap = ext;
		const cc = parseJSON<Record<string, unknown> | null>(m.cache_config, null);
		if (cc) pc.CacheConfig = cc;
		Object.assign(pc, parseJSON<Record<string, unknown>>(m.extra_config, {}));
		MAP[m.path] = pc;
	}

	const config: ProxyConfig = {
		MAP,
		Compression: { Gzip: { Enabled: false, Level: 0 }, Brotli: { Enabled: false, Level: 0 } },
		Security: {},
		Cache: {},
		MirrorCache: {},
		FaviconURL: '',
		CDN: { Providers: [] },
	};
	let version = 0;
	for (const o of others) {
		if (o.key === VERSION_KEY) version = parseInt(o.value, 10) || 0;
		if (o.key.startsWith('__')) continue;
		config[o.key] = parseJSON(o.value, null);
	}
	if (!config.CDN || !Array.isArray(config.CDN.Providers)) config.CDN = { Providers: [] };
	return { config, version };
}

export function validateConfig(cfg: ProxyConfig): void {
	if (!cfg || typeof cfg !== 'object') throw new HttpError(400, '配置不能为空');
	if (!cfg.MAP || typeof cfg.MAP !== 'object') throw new HttpError(400, 'MAP配置不能为空');
	for (const [path, pc] of Object.entries(cfg.MAP)) {
		if (!path) throw new HttpError(400, '路径不能为空');
		if (!path.startsWith('/')) throw new HttpError(400, `路径 ${path} 必须以 / 开头`);
		const target = String(pc?.DefaultTarget ?? '');
		if (!target) throw new HttpError(400, `路径 ${path} 的默认目标不能为空`);
		if (target === MIRROR_MARKER) continue;
		let u: URL;
		try {
			u = new URL(target);
		} catch {
			throw new HttpError(400, `路径 ${path} 的默认目标URL无效: ${target}`);
		}
		if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new HttpError(400, `路径 ${path} 的默认目标URL必须以 http/https 开头: ${target}`);
		if (!u.host) throw new HttpError(400, `路径 ${path} 的默认目标URL缺少主机名: ${target}`);
	}
	validateProviders(cfg.CDN?.Providers ?? []);
}

export function validateProviders(providers: CDNProvider[]): void {
	if (!Array.isArray(providers)) throw new HttpError(400, 'Providers 必须是数组');
	const ids = new Set<string>();
	let enabled = 0;
	for (const p of providers) {
		if (!p.ID?.trim()) throw new HttpError(400, 'provider ID 不能为空');
		if (!p.Name?.trim()) throw new HttpError(400, 'provider 名称不能为空');
		if (ids.has(p.ID)) throw new HttpError(400, `provider ID 重复: ${p.ID}`);
		ids.add(p.ID);
		p.Credentials = p.Credentials ?? {};
		const need = p.Type === 'cloudflare' ? ['apiToken', 'zoneId'] : p.Type === 'edgeone' ? ['secretId', 'secretKey', 'zoneId'] : null;
		if (!need) throw new HttpError(400, `unsupported CDN provider type: ${p.Type}`);
		for (const k of need) {
			if (!String(p.Credentials[k] ?? '').trim()) throw new HttpError(400, `${p.Type === 'cloudflare' ? 'Cloudflare' : 'EdgeOne'} provider 缺少 ${k}`);
		}
		if (p.Enabled) enabled++;
	}
	if (enabled > 1) throw new HttpError(400, '仅允许启用 1 个 CDN provider');
}

function isZero(v: unknown): boolean {
	if (v === null || v === undefined || v === false || v === '') return true;
	if (Array.isArray(v)) return v.length === 0;
	if (typeof v === 'object') return Object.keys(v as object).length === 0;
	return false;
}

// writeConfig 全量写入 (删除 D1 中已不存在的路径), 返回新版本号
export async function writeConfig(env: Env, cfg: ProxyConfig): Promise<number> {
	validateConfig(cfg);
	const now = Date.now();
	const stmts: D1PreparedStatement[] = [];
	// json_each 只占一个绑定参数 (D1 单语句最多 100 个绑定参数)
	stmts.push(env.DB.prepare('DELETE FROM config_maps WHERE path NOT IN (SELECT value FROM json_each(?1))').bind(JSON.stringify(Object.keys(cfg.MAP))));

	for (const [path, pc] of Object.entries(cfg.MAP)) {
		const extra: Record<string, unknown> = {};
		for (const k of EXTRA_KEYS) if (!isZero(pc[k])) extra[k] = pc[k];
		stmts.push(
			env.DB.prepare(
				`INSERT INTO config_maps (path, default_target, enabled, extension_rules, cache_config, extra_config, created_at, updated_at)
				 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?7)
				 ON CONFLICT(path) DO UPDATE SET default_target = excluded.default_target, enabled = excluded.enabled,
				   extension_rules = excluded.extension_rules, cache_config = excluded.cache_config,
				   extra_config = excluded.extra_config, updated_at = excluded.updated_at`,
			).bind(
				path,
				String(pc.DefaultTarget ?? ''),
				pc.Enabled === false ? 0 : 1,
				pc.ExtensionMap ? JSON.stringify(pc.ExtensionMap) : null,
				pc.CacheConfig ? JSON.stringify(pc.CacheConfig) : null,
				Object.keys(extra).length ? JSON.stringify(extra) : null,
				now,
			),
		);
	}

	const upsertOther = (key: string, value: string) =>
		env.DB.prepare(
			`INSERT INTO config_other (key, value, updated_at) VALUES (?1, ?2, ?3)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		).bind(key, value, now);

	for (const [key, value] of Object.entries(cfg)) {
		if (key === 'MAP' || key.startsWith('__')) continue;
		stmts.push(upsertOther(key, JSON.stringify(value)));
	}
	// 版本号在事务内基于当前值递增, 并发保存也保持单调 (后提交的一定版本更大, 节点不会漏拉)
	stmts.push(
		env.DB.prepare(
			`INSERT INTO config_other (key, value, updated_at)
			 VALUES (?1, CAST(MAX(?2, COALESCE((SELECT CAST(value AS INTEGER) FROM config_other WHERE key = ?1), 0) + 1) AS TEXT), ?2)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		).bind(VERSION_KEY, now),
	);
	stmts.push(env.DB.prepare('SELECT value FROM config_other WHERE key = ?1').bind(VERSION_KEY));

	const results = await env.DB.batch(stmts);
	const row = results[results.length - 1].results?.[0] as { value?: string } | undefined;
	return parseInt(row?.value ?? '0', 10) || now;
}
