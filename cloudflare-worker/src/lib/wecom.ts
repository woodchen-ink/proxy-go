/**
 * 企业微信群机器人通知 (旁路)
 *
 * 未配置 WECOM_WEBHOOK_URL 即停用; 发送失败只记日志, 不影响调用方。
 * 该 webhook 与其他 CZL 项目共享 20 条/分钟限额, 调用方负责"同一事件只推一次"。
 */
import { formatLocalDateTime } from './time';

export type NoticeLevel = 'info' | 'warning';

export async function notifyWecom(webhook: string | undefined, level: NoticeLevel, title: string, lines: string[]): Promise<void> {
	if (!webhook) return;
	const content = [
		`### <font color="${level}">${title}</font>`,
		...lines.filter(Boolean).map((l) => `> ${l}`),
		`> <font color="comment">${formatLocalDateTime(Date.now())} · proxy-go 中控</font>`,
	].join('\n');
	try {
		const resp = await fetch(webhook, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ msgtype: 'markdown', markdown: { content } }),
		});
		const body = (await resp.json().catch(() => ({}))) as { errcode?: number; errmsg?: string };
		if (body.errcode) console.error('wecom notify rejected:', body.errcode, body.errmsg);
	} catch (e) {
		console.error('wecom notify failed:', e);
	}
}
