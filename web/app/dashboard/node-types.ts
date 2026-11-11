// 节点维度接口 (/admin/api/nodes/overview, /admin/api/nodes/series) 的返回形状

export interface NodeTotals {
  requests: number
  bytes: number
  origin_bytes: number
  errors: number
  cache_hits: number
  cache_misses: number
  latency_ns: number
  big_bytes: number
  big_ns: number
  big_count: number
  speed: number[]
  hit_rate: number
  error_rate: number
  avg_latency_ms: number
  avg_speed_bps: number
  avg_bandwidth_bps: number
}

export interface NodeOverviewRow {
  node_id: string
  online: boolean
  state: "up" | "down"
  version: string
  hostname: string
  note: string
  last_seen: number
  live: {
    bandwidth_bps: number
    requests_per_second: number
    active_requests: number
    uptime_sec: number
    memory_bytes: number
    has_series: boolean
  } | null
  last_hour: NodeTotals
  today: NodeTotals
  last24h: NodeTotals
}

export interface NodesOverview {
  generated_at: number
  speed_labels: string[]
  nodes: NodeOverviewRow[]
  cluster: {
    bandwidth_bps: number
    requests_per_second: number
    last_hour: NodeTotals
    today: NodeTotals
    last24h: NodeTotals
  }
}

export type SeriesRange = "1h" | "24h" | "30d"

export interface NodeSeriesPoint {
  ts: number
  requests: number
  bytes: number
  origin_bytes: number
  errors: number
  cache_hits: number
  cache_misses: number
  bandwidth_bps: number
  avg_speed_bps: number
}

export interface NodesSeries {
  range: SeriesRange
  step: number
  buckets: number[]
  labels: (string | number)[]
  speed_labels: string[]
  node_ids: string[]
  series: Record<string, NodeSeriesPoint[]>
  totals: Record<string, NodeTotals>
}

// nodeColor 节点在图表里的固定色: 按节点 id 排序后的序号取 chart 色板, 同一节点在各图同色
export function nodeColor(index: number): string {
  return `hsl(var(--chart-${(index % 6) + 1}))`
}
