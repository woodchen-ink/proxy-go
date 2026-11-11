"use client"

import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { formatAge, formatBps, formatBytes, formatCount, formatMs, formatPct } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { NodeOverviewRow, NodesOverview, NodeTotals } from "../node-types"
import { nodeColor } from "../node-types"

type Window = "today" | "last24h"

interface NodeCompareTableProps {
  data: NodesOverview | null
  error: string | null
  window: Window
  onWindowChange: (w: Window) => void
  onSelect: (nodeId: string) => void
}

const WINDOWS: { value: Window; label: string }[] = [
  { value: "today", label: "今日" },
  { value: "last24h", label: "近 24 小时" },
]

// SpeedCell 下载均速; 没有 ≥256KB 的响应时没有样本, 显示横杠而不是 0
function SpeedCell({ t }: { t: NodeTotals }) {
  if (!t.big_count) return <span className="text-foreground-subtle">-</span>
  return <span title={`${formatCount(t.big_count)} 个 ≥256KB 的响应`}>{formatBps(t.avg_speed_bps)}</span>
}

// NodeCompareTable 各节点承载对比: 实时带宽 / QPS 来自快照, 流量 / 回源 / 命中 / 速度按所选窗口统计
export default function NodeCompareTable({ data, error, window, onWindowChange, onSelect }: NodeCompareTableProps) {
  const nodes = data?.nodes ?? []
  const clusterBytes = data?.cluster[window].bytes ?? 0

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <div>
          <h2 className="text-base font-semibold">节点承载</h2>
          <p className="text-sm text-foreground-muted">
            点击节点查看详情 · 下载速度为节点到外层 CDN 的下发速度, 只统计 ≥256KB 的响应
          </p>
        </div>
        <div role="radiogroup" aria-label="统计窗口" className="flex gap-1">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              role="radio"
              aria-checked={window === w.value}
              onClick={() => onWindowChange(w.value)}
              className={cn(
                "h-7 rounded-full border px-3 text-sm transition-colors",
                window === w.value
                  ? "border-primary bg-primary text-primary-foreground"
                  : "text-foreground-muted hover:bg-accent hover:text-foreground",
              )}
            >
              {w.label}
            </button>
          ))}
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[56rem] text-sm">
          <thead className="bg-muted text-foreground-muted">
            <tr className="border-b">
              <th className="px-5 py-2 text-left font-medium">节点</th>
              <th className="px-3 py-2 text-right font-medium">实时带宽</th>
              <th className="px-3 py-2 text-right font-medium">QPS</th>
              <th className="px-3 py-2 text-left font-medium">下发流量</th>
              <th className="px-3 py-2 text-right font-medium">回源流量</th>
              <th className="px-3 py-2 text-right font-medium">请求</th>
              <th className="px-3 py-2 text-right font-medium">命中率</th>
              <th className="px-3 py-2 text-right font-medium">下载均速</th>
              <th className="px-3 py-2 text-right font-medium">平均延迟</th>
              <th className="px-5 py-2 text-right font-medium">错误率</th>
            </tr>
          </thead>
          <tbody className="divide-y tabular-nums">
            {!data && !error &&
              Array.from({ length: 3 }, (_, i) => (
                <tr key={i}>
                  <td colSpan={10} className="px-5 py-3">
                    <Skeleton className="h-5 w-full" />
                  </td>
                </tr>
              ))}
            {error && !data && (
              <tr>
                <td colSpan={10} className="px-5 py-8 text-center text-destructive">
                  {error}
                </td>
              </tr>
            )}
            {data && nodes.length === 0 && (
              <tr>
                <td colSpan={10} className="px-5 py-8 text-center text-foreground-muted">
                  还没有节点接入
                </td>
              </tr>
            )}
            {nodes.map((n, i) => (
              <NodeRow key={n.node_id} n={n} t={n[window]} color={nodeColor(i)} clusterBytes={clusterBytes} onSelect={onSelect} />
            ))}
          </tbody>
          {data && nodes.length > 1 && (
            <tfoot className="border-t bg-muted tabular-nums">
              <tr>
                <td className="px-5 py-2 font-medium">合计</td>
                <td className="px-3 py-2 text-right">{formatBps(data.cluster.bandwidth_bps)}</td>
                <td className="px-3 py-2 text-right">{data.cluster.requests_per_second.toFixed(1)}</td>
                <td className="px-3 py-2 font-medium">{formatBytes(data.cluster[window].bytes)}</td>
                <td className="px-3 py-2 text-right">{formatBytes(data.cluster[window].origin_bytes)}</td>
                <td className="px-3 py-2 text-right">{formatCount(data.cluster[window].requests)}</td>
                <td className="px-3 py-2 text-right">{formatPct(data.cluster[window].hit_rate)}</td>
                <td className="px-3 py-2 text-right">
                  <SpeedCell t={data.cluster[window]} />
                </td>
                <td className="px-3 py-2 text-right">{formatMs(data.cluster[window].avg_latency_ms)}</td>
                <td className="px-5 py-2 text-right">{formatPct(data.cluster[window].error_rate, 2)}</td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
    </Card>
  )
}

function NodeRow({
  n,
  t,
  color,
  clusterBytes,
  onSelect,
}: {
  n: NodeOverviewRow
  t: NodeTotals
  color: string
  clusterBytes: number
  onSelect: (id: string) => void
}) {
  const share = clusterBytes > 0 ? t.bytes / clusterBytes : 0
  const errorHigh = t.requests >= 100 && t.error_rate >= 0.05
  return (
    <tr
      className="cursor-pointer transition-colors hover:bg-accent"
      onClick={() => onSelect(n.node_id)}
      onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onSelect(n.node_id)}
      tabIndex={0}
    >
      <td className="px-5 py-2.5">
        <div className="flex items-center gap-2">
          <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-sm" style={{ background: color }} />
          <span className="font-medium">{n.node_id}</span>
          {!n.online && (
            <span className="rounded-md bg-destructive-soft px-1.5 py-0.5 text-xs text-destructive">
              离线{n.last_seen ? ` ${formatAge(n.last_seen)}` : ""}
            </span>
          )}
        </div>
        {(n.note || n.version) && (
          <div className="mt-0.5 pl-[18px] text-xs text-foreground-subtle">{[n.note, n.version].filter(Boolean).join(" · ")}</div>
        )}
      </td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">{n.live ? formatBps(n.live.bandwidth_bps) : "-"}</td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">{n.live ? n.live.requests_per_second.toFixed(1) : "-"}</td>
      <td className="px-3 py-2.5">
        <div className="flex items-center gap-2">
          <span className="w-20 shrink-0 font-medium">{formatBytes(t.bytes)}</span>
          <span className="h-1.5 w-24 overflow-hidden rounded-full bg-secondary" title={`占集群 ${formatPct(share)}`}>
            <span className="block h-full rounded-full" style={{ width: `${Math.round(share * 100)}%`, background: color }} />
          </span>
          <span className="w-12 text-right text-xs text-foreground-subtle">{formatPct(share, 0)}</span>
        </div>
      </td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">{formatBytes(t.origin_bytes)}</td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">{formatCount(t.requests)}</td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">{formatPct(t.hit_rate)}</td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">
        <SpeedCell t={t} />
      </td>
      <td className="px-3 py-2.5 text-right text-foreground-muted">{formatMs(t.avg_latency_ms)}</td>
      <td className={cn("px-5 py-2.5 text-right", errorHigh ? "font-medium text-destructive" : "text-foreground-muted")}>
        {formatPct(t.error_rate, 2)}
      </td>
    </tr>
  )
}
