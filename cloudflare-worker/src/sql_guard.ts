/**
 * /admin/sql/* 的语句白名单
 *
 * 这两个 endpoint 原本能用 API_TOKEN 执行任意 SQL; 而每个节点都持有 API_TOKEN,
 * 任一节点泄露 token 就能清空配置表或塞一条恶意回源。节点实际只用到三类语句, 只放行这些:
 *   - 表结构迁移 (pkg/sync/migrations.go): CREATE TABLE / CREATE INDEX / ALTER TABLE ADD COLUMN /
 *     记录迁移 id / latency_distribution 种子行; DROP TABLE 只在表不存在或为空时放行 (见 checkDrop)
 *   - 节点维护: 删自己的 path_stats 行、清理过期的重置 marker
 *   - 查询: 已应用的迁移 id、重置 marker
 * 新增迁移若用到别的语句形态, 要同时在这里放开。
 */

const norm = (sql: string) => sql.replace(/\s+/g, ' ').trim().replace(/;$/, '').trim();

const IDENT = '[A-Za-z_][A-Za-z0-9_]*';

// DDL: 结构上只能新增, 不能改写或删除已有数据
const DDL_PATTERNS = [
	new RegExp(`^CREATE TABLE (IF NOT EXISTS )?${IDENT} \\(.*\\)$`, 'i'),
	new RegExp(`^CREATE (UNIQUE )?INDEX (IF NOT EXISTS )?${IDENT} ON ${IDENT}\\s?\\([^;]*\\)$`, 'i'),
	new RegExp(`^ALTER TABLE ${IDENT} ADD (COLUMN )?${IDENT} [^;]*$`, 'i'),
];

// DROP_PATTERN 迁移 0006 重建 path_stats 用; 是否放行另由 checkDrop 判断表内是否有数据
const DROP_PATTERN = new RegExp(`^DROP TABLE IF EXISTS (${IDENT})$`, 'i');

// 逐字匹配 (归一空白后) 的写语句
const EXACT_WRITES = new Set(
	[
		'INSERT INTO app_migrations (id, applied_at) VALUES (?, ?)',
		'DELETE FROM path_stats WHERE node_id = ?',
		'DELETE FROM path_stats WHERE node_id = ? AND (path = ? OR path LIKE ?)',
		'DELETE FROM stats_resets WHERE reset_at < ?',
	].map(norm),
);

// latency_distribution 种子行 (迁移 0002), 只允许这张表的 INSERT OR IGNORE
const SEED_PATTERN = /^INSERT OR IGNORE INTO latency_distribution \(bucket, count, updated_at\) VALUES \('[A-Za-z0-9-]+', 0, 0\)$/i;

const EXACT_QUERIES = new Set(
	[
		'SELECT id FROM app_migrations',
		'SELECT id, path, reset_at FROM stats_resets WHERE reset_at > ? ORDER BY reset_at ASC LIMIT 50',
		'SELECT MAX(reset_at) AS latest FROM stats_resets',
	].map(norm),
);

export type GuardResult = { ok: true; dropTable?: string } | { ok: false; reason: string };

// checkWrite 判断 /admin/sql/batch 的单条语句; DROP 需要调用方再用 checkDrop 查表
export function checkWrite(sql: string): GuardResult {
	if (typeof sql !== 'string' || !sql.trim()) return { ok: false, reason: 'empty statement' };
	const s = norm(sql);
	if (s.includes(';')) return { ok: false, reason: 'multiple statements not allowed' };
	if (EXACT_WRITES.has(s) || SEED_PATTERN.test(s) || DDL_PATTERNS.some((re) => re.test(s))) return { ok: true };
	const drop = DROP_PATTERN.exec(s);
	if (drop) return { ok: true, dropTable: drop[1] };
	return { ok: false, reason: 'statement not allowed' };
}

export function checkQuery(sql: string): GuardResult {
	if (typeof sql !== 'string') return { ok: false, reason: 'sql string required' };
	return EXACT_QUERIES.has(norm(sql)) ? { ok: true } : { ok: false, reason: 'query not allowed' };
}

// checkDrop 只允许删除不存在或为空的表 (新库上跑迁移 0006 时 path_stats 刚建好、必为空)
export async function checkDrop(db: D1Database, table: string): Promise<GuardResult> {
	const exists = await db.prepare("SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?").bind(table).first();
	if (!exists) return { ok: true };
	const row = await db.prepare(`SELECT 1 AS x FROM "${table}" LIMIT 1`).first();
	return row ? { ok: false, reason: `refusing to drop non-empty table ${table}` } : { ok: true };
}
