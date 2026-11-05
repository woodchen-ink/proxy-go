/**
 * 统计口径的时区
 *
 * 节点一律以 TZ=Asia/Shanghai 运行, 切日 / 切小时都按东八区; 中国大陆无夏令时, 固定偏移即可。
 */
export const TZ_NAME = 'Asia/Shanghai';
export const TZ_OFFSET_SEC = 8 * 3600;

// localDayNumber 自纪元起按东八区计的天序号 (与 Go 侧 localDay 一致)
export function localDayNumber(unixSec: number): number {
	return Math.floor((unixSec + TZ_OFFSET_SEC) / 86400);
}

// localMidnightSec 东八区当天 0 点对应的 unix 秒
export function localMidnightSec(unixSec: number): number {
	return localDayNumber(unixSec) * 86400 - TZ_OFFSET_SEC;
}

// formatLocalDate YYYY-MM-DD (东八区)
export function formatLocalDate(unixSec: number): string {
	return new Date((unixSec + TZ_OFFSET_SEC) * 1000).toISOString().slice(0, 10);
}

export function formatLocalDateTime(unixMs: number): string {
	return new Date(unixMs + TZ_OFFSET_SEC * 1000).toISOString().slice(0, 19).replace('T', ' ');
}
