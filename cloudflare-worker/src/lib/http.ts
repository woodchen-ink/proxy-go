/** JSON 响应与请求体读取的小工具 */

export function json(data: unknown, status = 200, headers: Record<string, string> = {}): Response {
	return new Response(JSON.stringify(data), {
		status,
		headers: { 'Content-Type': 'application/json; charset=utf-8', ...headers },
	});
}

// fail 管理端错误响应: 数字码 + 可读消息, 前端按 message 直接展示
export function fail(status: number, message: string, extra: Record<string, unknown> = {}): Response {
	return json({ error: message, code: status, ...extra }, status);
}

export class HttpError extends Error {
	constructor(
		public status: number,
		message: string,
	) {
		super(message);
	}
}

// readJSON 读取请求体; 解析失败统一抛 400
export async function readJSON<T>(request: Request): Promise<T> {
	try {
		return (await request.json()) as T;
	} catch {
		throw new HttpError(400, '请求体不是合法 JSON');
	}
}

// timingSafeEqual 常量时间比较, 防令牌按位试探
export function timingSafeEqual(a: string, b: string): boolean {
	const enc = new TextEncoder();
	const x = enc.encode(a);
	const y = enc.encode(b);
	if (x.length !== y.length) return false;
	let diff = 0;
	for (let i = 0; i < x.length; i++) diff |= x[i] ^ y[i];
	return diff === 0;
}

// bearerMatches 校验 Authorization: Bearer <token>; 未配置期望值时一律拒绝
export function bearerMatches(request: Request, expected: string | undefined): boolean {
	if (!expected) return false;
	const header = request.headers.get('Authorization')?.trim() ?? '';
	if (!header.startsWith('Bearer ')) return false;
	return timingSafeEqual(header.slice(7).trim(), expected);
}
