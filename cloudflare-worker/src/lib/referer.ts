/**
 * Referer host 归一化与后缀匹配, 语义与节点 Referer 黑名单一致:
 * 条目 `x.com` 命中 `x.com` 与 `a.x.com`, 不命中 `evilx.com`
 */

export function normalizeRefererHost(v: unknown): string {
	return String(v ?? '')
		.trim()
		.toLowerCase()
		.replace(/^https?:\/\//, '')
		.replace(/[/:].*$/, '')
		.replace(/\.$/, '');
}

export function hostMatches(host: string, entries: Iterable<string>): boolean {
	for (const e of entries) {
		if (!e) continue;
		if (host === e || host.endsWith(`.${e}`)) return true;
	}
	return false;
}
