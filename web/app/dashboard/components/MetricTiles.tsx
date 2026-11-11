"use client"

import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatBps, formatBytes, formatCount, formatMs, formatPct } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { NodesOverview } from "../node-types"

interface MetricsLite {
  total_requests: number
  status_code_stats: Record<string, number>
}

// 5xx 占比超过这个值才上危险色; 4xx 多为 404 / 防盗链 403, 属正常业务, 不计入告警
const ERROR_ALERT_RATE = 0.01

function Tile({
  label,
  value,
  sub,
  tone,
  loading,
}: {
  label: string
  value: string
  sub?: string
  tone?: "danger"
  loading?: boolean
}) {
  return (
    <Card className="px-5 py-4">
      <div className="text-sm text-foreground-muted">{label}</div>
      {loading ? (
        <Skeleton className="mt-2 h-8 w-28" />
      ) : (
        <div className={cn("mt-1 text-3xl font-semibold tracking-tight tabular-nums", tone === "danger" && "text-destructive")}>{value}</div>
      )}
      {sub && !loading && <div className="mt-1 text-sm text-foreground-subtle tabular-nums">{sub}</div>}
    </Card>
  )
}

function Mini({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-sm text-foreground-muted">{label}</div>
      <div className="mt-0.5 truncate text-lg font-semibold tabular-nums">{value}</div>
    </div>
  )
}

// MetricTiles 集群关键指标: 首行四个大数 (今日流量 / 实时带宽 / 命中率 / 5xx), 次行为辅助指标;
// 进程级指标 (运行时间 / goroutine / 内存) 是单节点排障用的, 放在节点页, 不占集群首屏
export default function MetricTiles({ m, overview }: { m: MetricsLite | null; overview: NodesOverview | null }) {
  const today = overview?.cluster.today
  const codes = m?.status_code_stats ?? {}
  let total = 0
  let c4 = 0
  let c5 = 0
  for (const [code, count] of Object.entries(codes)) {
    const c = parseInt(code, 10)
    total += count
    if (c >= 500) c5 += count
    else if (c >= 400) c4 += count
  }
  const rate5 = total > 0 ? c5 / total : 0
  const online = overview?.nodes.filter((n) => n.online).length ?? 0
  const nodeCount = overview?.nodes.length ?? 0

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Tile
          label="今日下发流量"
          value={today ? formatBytes(today.bytes) : "-"}
          sub={today ? `回源 ${formatBytes(today.origin_bytes)} · ${formatCount(today.requests)} 次请求` : undefined}
          loading={!overview}
        />
        <Tile
          label="实时带宽"
          value={overview ? formatBps(overview.cluster.bandwidth_bps) : "-"}
          sub={overview ? `${overview.cluster.requests_per_second.toFixed(1)} QPS` : undefined}
          loading={!overview}
        />
        <Tile
          label="今日缓存命中率"
          value={today ? formatPct(today.hit_rate) : "-"}
          sub={today ? `命中 ${formatCount(today.cache_hits)} / 未命中 ${formatCount(today.cache_misses)}` : undefined}
          loading={!overview}
        />
        <Tile
          label="5xx 错误率"
          value={formatPct(rate5, 2)}
          sub={`${formatCount(c5)} 次 5xx · 4xx ${formatPct(total > 0 ? c4 / total : 0)} (累计)`}
          tone={rate5 >= ERROR_ALERT_RATE ? "danger" : undefined}
          loading={!m}
        />
      </div>
      <Card className="grid grid-cols-2 gap-4 px-5 py-3 sm:grid-cols-4">
        <Mini label="在线节点" value={overview ? `${online} / ${nodeCount}` : "-"} />
        <Mini label="今日平均延迟" value={today ? formatMs(today.avg_latency_ms) : "-"} />
        <Mini label="今日下载均速" value={today && today.big_count ? formatBps(today.avg_speed_bps) : "-"} />
        <Mini label="累计请求" value={m ? formatCount(m.total_requests) : "-"} />
      </Card>
    </div>
  )
}
