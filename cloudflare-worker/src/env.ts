/**
 * Worker 绑定与密钥
 *
 * 密钥一律走 `wrangler secret put`, 不写进 wrangler.toml:
 *   API_TOKEN                 节点 ↔ 中控的共享令牌 (旧 REST 协议与 WS 通道共用)
 *   CACHE_CLEAR_REMOTE_TOKEN  公开清缓存接口令牌 (与节点时代同值, 第三方只换域名)
 *   OAUTH_CLIENT_ID / OAUTH_CLIENT_SECRET  CZL Connect OAuth 应用
 *   SESSION_SECRET            管理端会话 cookie 的 HMAC 密钥
 *   CRED_KEY                  DNS 平台凭据的 AES-GCM 密钥 (base64, 32 字节)
 *   WECOM_WEBHOOK_URL         企业微信群机器人 (可选, 缺省即不通知)
 */
import type { HubAPI } from './hub/hub';

export interface Env {
	DB: D1Database;
	HUB: DurableObjectNamespace;
	ASSETS: Fetcher;
	API_TOKEN?: string;
	CACHE_CLEAR_REMOTE_TOKEN?: string;
	OAUTH_CLIENT_ID?: string;
	OAUTH_CLIENT_SECRET?: string;
	SESSION_SECRET?: string;
	CRED_KEY?: string;
	WECOM_WEBHOOK_URL?: string;
}

// hubStub 中控全局唯一的 Durable Object 实例
// 显式声明 RPC 形状: workers-types 的 Rpc 推断会把含 unknown 的返回值收窄成 never
export function hubStub(env: Env): HubAPI & { fetch(request: Request): Promise<Response> } {
	return env.HUB.get(env.HUB.idFromName('hub')) as unknown as HubAPI & { fetch(request: Request): Promise<Response> };
}
