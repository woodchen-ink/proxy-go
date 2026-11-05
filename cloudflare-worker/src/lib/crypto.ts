/**
 * 会话签名与凭据加密
 *
 * 会话: 无状态 HMAC 签名 cookie (`<payload>.<sig>`), 管理端每次请求不必打 DO / D1。
 * 凭据: DNS 平台密钥以 AES-GCM 加密后落 DO SQLite, 密钥是 Worker secret CRED_KEY;
 * 数据库副本泄露不等于密钥泄露。
 */

const enc = new TextEncoder();
const dec = new TextDecoder();

function b64url(buf: ArrayBuffer | Uint8Array): string {
	const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
	let s = '';
	for (const b of bytes) s += String.fromCharCode(b);
	return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function fromB64url(s: string): Uint8Array {
	let b64 = s.replace(/-/g, '+').replace(/_/g, '/');
	while (b64.length % 4) b64 += '=';
	const bin = atob(b64);
	const out = new Uint8Array(bin.length);
	for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
	return out;
}

async function hmacKey(secret: string): Promise<CryptoKey> {
	return crypto.subtle.importKey('raw', enc.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign', 'verify']);
}

// signToken 生成 `<base64url(json)>.<base64url(hmac)>`
export async function signToken(secret: string, payload: Record<string, unknown>): Promise<string> {
	const body = b64url(enc.encode(JSON.stringify(payload)));
	const sig = await crypto.subtle.sign('HMAC', await hmacKey(secret), enc.encode(body));
	return `${body}.${b64url(sig)}`;
}

// verifyToken 验签并返回 payload; 签名不符或格式错误返回 null (不区分原因, 不给试探者提示)
export async function verifyToken<T>(secret: string, token: string): Promise<T | null> {
	const dot = token.indexOf('.');
	if (dot <= 0) return null;
	const body = token.slice(0, dot);
	const sig = token.slice(dot + 1);
	try {
		const ok = await crypto.subtle.verify('HMAC', await hmacKey(secret), fromB64url(sig), enc.encode(body));
		if (!ok) return null;
		return JSON.parse(dec.decode(fromB64url(body))) as T;
	} catch {
		return null;
	}
}

export function randomToken(bytes = 32): string {
	return b64url(crypto.getRandomValues(new Uint8Array(bytes)));
}

async function aesKey(keyB64: string): Promise<CryptoKey> {
	const raw = Uint8Array.from(atob(keyB64), (c) => c.charCodeAt(0));
	if (raw.length !== 32) throw new Error('CRED_KEY 必须是 32 字节的 base64');
	return crypto.subtle.importKey('raw', raw, 'AES-GCM', false, ['encrypt', 'decrypt']);
}

// sealSecret 加密为 `v1.<iv>.<ciphertext>`
export async function sealSecret(keyB64: string, plain: string): Promise<string> {
	const iv = crypto.getRandomValues(new Uint8Array(12));
	const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await aesKey(keyB64), enc.encode(plain));
	return `v1.${b64url(iv)}.${b64url(ct)}`;
}

export async function openSecret(keyB64: string, sealed: string): Promise<string> {
	const [ver, iv, ct] = sealed.split('.');
	if (ver !== 'v1' || !iv || !ct) throw new Error('凭据密文格式不认识');
	const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: fromB64url(iv) }, await aesKey(keyB64), fromB64url(ct));
	return dec.decode(pt);
}
