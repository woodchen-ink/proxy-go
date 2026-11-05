/**
 * 腾讯云 API 3.0 (TC3-HMAC-SHA256) 签名与调用
 *
 * EdgeOne 清缓存与 DNSPod 解析共用。签名算法与节点时代 Go 版 signTencentCloudV3 一致:
 * POST / , 签名头只含 content-type 与 host。
 */

const enc = new TextEncoder();

async function sha256Hex(data: string): Promise<string> {
	return toHex(await crypto.subtle.digest('SHA-256', enc.encode(data)));
}

async function hmac(key: ArrayBuffer | Uint8Array, data: string): Promise<ArrayBuffer> {
	const k = await crypto.subtle.importKey('raw', key, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
	return crypto.subtle.sign('HMAC', k, enc.encode(data));
}

function toHex(buf: ArrayBuffer): string {
	return [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

export interface TencentCredential {
	secretId: string;
	secretKey: string;
}

export interface TencentCall {
	service: string; // teo / dnspod
	host: string; // teo.tencentcloudapi.com
	action: string;
	version: string;
	region?: string;
	payload: Record<string, unknown>;
}

export class TencentError extends Error {
	constructor(
		public code: string,
		message: string,
	) {
		super(`${code}: ${message}`);
	}
}

// callTencent 发起一次签名调用; 业务错误 (Response.Error) 抛 TencentError
export async function callTencent<T = Record<string, unknown>>(cred: TencentCredential, call: TencentCall): Promise<T> {
	const body = JSON.stringify(call.payload);
	const timestamp = Math.floor(Date.now() / 1000);
	const date = new Date(timestamp * 1000).toISOString().slice(0, 10);

	const canonicalHeaders = `content-type:application/json\nhost:${call.host}\n`;
	const signedHeaders = 'content-type;host';
	const canonicalRequest = ['POST', '/', '', canonicalHeaders, signedHeaders, await sha256Hex(body)].join('\n');
	const scope = `${date}/${call.service}/tc3_request`;
	const stringToSign = ['TC3-HMAC-SHA256', String(timestamp), scope, await sha256Hex(canonicalRequest)].join('\n');

	const secretDate = await hmac(enc.encode('TC3' + cred.secretKey), date);
	const secretService = await hmac(secretDate, call.service);
	const secretSigning = await hmac(secretService, 'tc3_request');
	const signature = toHex(await hmac(secretSigning, stringToSign));

	const headers: Record<string, string> = {
		'Content-Type': 'application/json',
		Host: call.host,
		'X-TC-Action': call.action,
		'X-TC-Version': call.version,
		'X-TC-Timestamp': String(timestamp),
		Authorization: `TC3-HMAC-SHA256 Credential=${cred.secretId}/${scope}, SignedHeaders=${signedHeaders}, Signature=${signature}`,
	};
	if (call.region) headers['X-TC-Region'] = call.region;

	const resp = await fetch(`https://${call.host}`, { method: 'POST', headers, body });
	const text = await resp.text();
	let parsed: { Response?: T & { Error?: { Code: string; Message: string } } };
	try {
		parsed = JSON.parse(text);
	} catch {
		throw new TencentError(`HTTP${resp.status}`, text.slice(0, 200));
	}
	const r = parsed.Response;
	if (!r) throw new TencentError(`HTTP${resp.status}`, 'empty response');
	if (r.Error) throw new TencentError(r.Error.Code, r.Error.Message);
	return r;
}
