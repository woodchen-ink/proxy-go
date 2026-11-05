/**
 * 管理端登录 (CZL Connect OAuth, confidential client)
 *
 * 会话是 HMAC 签名的无状态 cookie `proxy_go_token` (HttpOnly + Secure + SameSite=Lax, Path=/admin, 30 天),
 * 每次请求只验签不查库。代价是无法服务端吊销单个会话; 需要全体下线时轮换 SESSION_SECRET。
 * CSRF: GET/HEAD/OPTIONS 之外的请求做同源校验 (Sec-Fetch-Site 优先, 缺失时比对 Origin 与 Host)。
 */
import type { Env } from '../env';
import { randomToken, signToken, verifyToken } from '../lib/crypto';
import { fail, json } from '../lib/http';

const SESSION_COOKIE = 'proxy_go_token';
const STATE_COOKIE = 'proxy_go_oauth_state';
const SESSION_TTL_SEC = 30 * 86400;
const AUTHORIZE_URL = 'https://connect.czl.net/oauth2/authorize';
const TOKEN_URL = 'https://connect.czl.net/api/oauth2/token';
const USERINFO_URL = 'https://connect.czl.net/api/oauth2/userinfo';

interface Session {
	u: string;
	exp: number;
}

function readCookie(request: Request, name: string): string {
	const header = request.headers.get('Cookie') ?? '';
	for (const part of header.split(';')) {
		const [k, ...v] = part.trim().split('=');
		if (k === name) return v.join('=');
	}
	return '';
}

function cookie(name: string, value: string, maxAge: number, path = '/admin'): string {
	return `${name}=${value}; Path=${path}; Max-Age=${maxAge}; HttpOnly; Secure; SameSite=Lax`;
}

function callbackURL(request: Request): string {
	return `${new URL(request.url).origin}/admin/api/oauth/callback`;
}

export async function currentUser(request: Request, env: Env): Promise<string | null> {
	if (!env.SESSION_SECRET) return null;
	const token = readCookie(request, SESSION_COOKIE);
	if (!token) return null;
	const s = await verifyToken<Session>(env.SESSION_SECRET, token);
	if (!s || s.exp < Date.now() / 1000) return null;
	return s.u;
}

export function isSameOrigin(request: Request): boolean {
	const site = request.headers.get('Sec-Fetch-Site');
	if (site) return site === 'same-origin' || site === 'none';
	const origin = request.headers.get('Origin');
	if (!origin) return true; // 非浏览器客户端, 不构成 CSRF 面
	try {
		return new URL(origin).host === new URL(request.url).host;
	} catch {
		return false;
	}
}

// handleAuthRoutes 登录相关的免鉴权路由; 不匹配返回 null
export async function handleAuthRoutes(request: Request, env: Env, path: string): Promise<Response | null> {
	if (path === '/admin/api/auth' && request.method === 'GET') {
		if (!env.OAUTH_CLIENT_ID || !env.OAUTH_CLIENT_SECRET || !env.SESSION_SECRET) return fail(500, 'OAuth 未配置');
		const state = randomToken(24);
		const url = new URL(AUTHORIZE_URL);
		url.search = new URLSearchParams({
			response_type: 'code',
			client_id: env.OAUTH_CLIENT_ID,
			redirect_uri: callbackURL(request),
			scope: 'read write',
			state,
		}).toString();
		return new Response(null, {
			status: 302,
			headers: { Location: url.toString(), 'Set-Cookie': cookie(STATE_COOKIE, state, 600, '/admin/api/oauth') },
		});
	}

	if (path === '/admin/api/oauth/callback' && request.method === 'GET') {
		return oauthCallback(request, env);
	}

	if (path === '/admin/api/check-auth' && request.method === 'GET') {
		const user = await currentUser(request, env);
		return user ? json({ authenticated: true, user }) : fail(401, 'Authentication required');
	}

	if (path === '/admin/api/logout' && request.method === 'POST') {
		if (!isSameOrigin(request)) return fail(403, 'cross-site request rejected');
		return json({ message: '已退出登录' }, 200, { 'Set-Cookie': cookie(SESSION_COOKIE, '', 0) });
	}
	return null;
}

async function oauthCallback(request: Request, env: Env): Promise<Response> {
	const url = new URL(request.url);
	const code = url.searchParams.get('code') ?? '';
	const state = url.searchParams.get('state') ?? '';
	const expected = readCookie(request, STATE_COOKIE);
	if (!state || !expected || state !== expected) return fail(400, 'Invalid state');
	if (!code) return fail(400, 'Missing code parameter');
	if (!env.OAUTH_CLIENT_ID || !env.OAUTH_CLIENT_SECRET || !env.SESSION_SECRET) return fail(500, 'OAuth 未配置');

	const tokenResp = await fetch(TOKEN_URL, {
		method: 'POST',
		headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
		body: new URLSearchParams({
			grant_type: 'authorization_code',
			code,
			redirect_uri: callbackURL(request),
			client_id: env.OAUTH_CLIENT_ID,
			client_secret: env.OAUTH_CLIENT_SECRET,
		}),
	});
	if (!tokenResp.ok) {
		console.error('oauth token exchange failed', tokenResp.status, await tokenResp.text());
		return fail(502, 'OAuth 换取令牌失败');
	}
	const token = (await tokenResp.json()) as { access_token?: string };
	if (!token.access_token) return fail(502, 'OAuth 返回空令牌');

	const infoResp = await fetch(USERINFO_URL, { headers: { Authorization: `Bearer ${token.access_token}` } });
	if (!infoResp.ok) return fail(502, '获取用户信息失败');
	const info = (await infoResp.json()) as Record<string, unknown>;
	const username =
		pick(info.username) || pick(info.preferred_username) || pick(info.sub) || (pick(info.email) ? pick(info.email).split('@')[0] : '');
	if (!username) return fail(502, '用户信息缺少用户名');

	const session = await signToken(env.SESSION_SECRET, { u: username, exp: Math.floor(Date.now() / 1000) + SESSION_TTL_SEC });
	const headers = new Headers({ Location: '/admin/dashboard/' });
	headers.append('Set-Cookie', cookie(SESSION_COOKIE, session, SESSION_TTL_SEC));
	headers.append('Set-Cookie', cookie(STATE_COOKIE, '', 0, '/admin/api/oauth'));
	console.log(`admin login: ${username}`);
	return new Response(null, { status: 303, headers });
}

function pick(v: unknown): string {
	return typeof v === 'string' ? v : '';
}
