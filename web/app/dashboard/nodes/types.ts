// 节点页的数据契约 (对应 cloudflare-worker/src/hub/rollout.ts / release_store.ts)

export interface NodeInfo {
  node_id: string
  ipv4: string
  ipv6: string
  version: string
  origin_host: string
  note: string
  state: "up" | "down"
  state_since: number
  last_seen: number
  online: boolean
  dokploy_server_id: number
  dokploy_app_id: string
  dokploy_app_type: "application" | "compose"
  probe: { ok: number; checked_at: number; error: string } | null
  live: {
    requests_per_second: number
    current_bandwidth_bps: number
    active_requests: number
    uptime_sec: number
    config_version: number
  } | null
}

export interface Release {
  version: string
  build_time: number
  title: string
  assets: Record<string, { sha256: string; size: number }>
  created_at: number
}

export type RolloutKind = "binary" | "container"

export interface RolloutStep {
  node_id: string
  status: "pending" | "running" | "done" | "skipped" | "failed"
  started_at: number
  finished_at: number
  message: string
}

export interface Rollout {
  id: number
  kind: RolloutKind
  version: string
  status: "running" | "done" | "failed" | "cancelled"
  steps: RolloutStep[]
  source: string
  message: string
  created_at: number
  updated_at: number
}

export interface DokployServer {
  id: number
  name: string
  url: string
  updated_at: number
}

export interface DokployApp {
  id: string
  type: "application" | "compose"
  name: string
  project: string
}

export interface ReleaseOverview {
  releases: Release[]
  rollouts: Rollout[]
  auto_binary: boolean
  dokploy_servers: DokployServer[]
}

// postJSON 发 JSON 并统一取错误信息; 成功返回响应体
export async function postJSON<T = unknown>(
  fetcher: (url: string, init?: RequestInit) => Promise<Response>,
  url: string,
  body: unknown,
  method = "POST",
): Promise<T> {
  const res = await fetcher(url, {
    method,
    headers: { "Content-Type": "application/json" },
    body: method === "DELETE" ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
  return data as T
}
