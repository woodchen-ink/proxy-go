"use client"

import { useCallback, useState } from "react"
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { usePolling } from "@/hooks/use-polling"
import { fetchApi } from "@/lib/api-client"
import { formatBps, formatBytes, formatCount, formatMs, formatPct } from "@/lib/format"
import type { NodeOverviewRow, NodesSeries } from "../node-types"
import RecentRequests, { type RequestLogRow } from "./RecentRequests"

interface NodeDetailDialogProps {
  node: NodeOverviewRow | null
  color: string
  speedLabels: string[]
  onClose: () => void
}

function formatUptime(sec: number) {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return d > 0 ? `${d} 天 ${h} 小时` : h > 0 ? `${h} 小时 ${m} 分` : `${m} 分`
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div>
      <div className="text-sm text-foreground-muted">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums">{value}</div>
      {hint && <div className="text-xs text-foreground-subtle">{hint}</div>}
    </div>
  )
}

// NodeDetailDialog 单个节点详情 (居中弹窗): 今日关键指标、近 24 小时带宽、下载速度分布、该节点最近请求
export default function NodeDetailDialog({ node, color, speedLabels, onClose }: NodeDetailDialogProps) {
  const [series, setSeries] = useState<NodesSeries | null>(null)
  const [recent, setRecent] = useState<RequestLogRow[] | null>(null)
  const nodeId = node?.node_id ?? ""

  const load = useCallback(async () => {
    if (!nodeId) return true
    try {
      const [s, m] = await Promise.all([
        fetchApi("/admin/api/nodes/series?range=24h"),
        fetchApi(`/admin/api/metrics?node=${encodeURIComponent(nodeId)}`),
      ])
      if (s.ok) setSeries(await s.json())
      if (m.ok) setRecent(((await m.json()).recent_requests ?? []) as RequestLogRow[])
      return s.ok && m.ok
    } catch {
      return false
    }
  }, [nodeId])

  usePolling(load, 30_000, [nodeId])

  const t = node?.today
  const points = series?.series[nodeId] ?? []
  const chart = points.map((p) => ({ ts: p.ts, bps: p.bandwidth_bps }))
  const speedTotal = t ? t.speed.reduce((a, b) => a + b, 0) : 0

  return (
    <Dialog open={!!node} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[85vh] w-[calc(100vw-2rem)] max-w-3xl flex-col gap-0 overflow-hidden rounded-2xl bg-popover p-0 shadow-modal">
        {node && t && (
          <>
            <DialogHeader className="shrink-0 space-y-1 border-b px-6 py-4 pr-12 text-left">
              <DialogTitle className="flex items-center gap-2">
                <span aria-hidden className="h-3 w-3 rounded-sm" style={{ background: color }} />
                {node.node_id}
                {!node.online && <span className="rounded-md bg-destructive-soft px-1.5 py-0.5 text-xs font-normal text-destructive">离线</span>}
              </DialogTitle>
              <DialogDescription>
                {[node.note, node.hostname, node.version, node.live ? `运行 ${formatUptime(node.live.uptime_sec)}` : ""].filter(Boolean).join(" · ") || "无备注"}
              </DialogDescription>
            </DialogHeader>

            <div className="min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain px-6 py-5">
              <section className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3">
                <Stat label="实时带宽" value={node.live ? formatBps(node.live.bandwidth_bps) : "-"} hint={node.live ? `${node.live.requests_per_second.toFixed(1)} QPS` : undefined} />
                <Stat label="今日下发" value={formatBytes(t.bytes)} hint={`${formatCount(t.requests)} 次请求`} />
                <Stat label="今日回源" value={formatBytes(t.origin_bytes)} hint={t.bytes > 0 ? `占下发 ${formatPct(t.origin_bytes / t.bytes)}` : undefined} />
                <Stat label="命中率" value={formatPct(t.hit_rate)} />
                <Stat label="下载均速" value={t.big_count ? formatBps(t.avg_speed_bps) : "-"} hint={t.big_count ? `${formatCount(t.big_count)} 个大文件样本` : "暂无 ≥256KB 的响应"} />
                <Stat label="平均延迟 / 错误率" value={`${formatMs(t.avg_latency_ms)} / ${formatPct(t.error_rate, 2)}`} />
              </section>

              <section className="space-y-2 border-t pt-4">
                <h3 className="text-sm font-semibold">近 24 小时带宽</h3>
                {!series ? (
                  <Skeleton className="h-40 w-full" />
                ) : chart.length === 0 ? (
                  <div className="flex h-40 items-center justify-center rounded-lg bg-muted text-sm text-foreground-muted">暂无数据</div>
                ) : (
                  <div className="h-40">
                    <ResponsiveContainer width="100%" height="100%">
                      <AreaChart data={chart} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
                        <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                        <XAxis
                          dataKey="ts"
                          tickFormatter={(v: number) => `${String(new Date(v * 1000).getHours()).padStart(2, "0")}:00`}
                          stroke="hsl(var(--foreground-subtle))"
                          fontSize={12}
                          tickLine={false}
                          minTickGap={24}
                        />
                        <YAxis tickFormatter={formatBps} stroke="hsl(var(--foreground-subtle))" fontSize={12} tickLine={false} axisLine={false} width={72} />
                        <Tooltip
                          contentStyle={{ background: "hsl(var(--popover))", border: "1px solid hsl(var(--border))", borderRadius: 10, fontSize: 13 }}
                          labelFormatter={(v) => new Date(Number(v) * 1000).toLocaleString("zh-CN", { hour12: false })}
                          formatter={(v: number) => [formatBps(v), "平均带宽"]}
                        />
                        <Area type="monotone" dataKey="bps" stroke={color} fill={color} fillOpacity={0.25} strokeWidth={1.5} isAnimationActive={false} />
                      </AreaChart>
                    </ResponsiveContainer>
                  </div>
                )}
              </section>

              <section className="space-y-2 border-t pt-4">
                <h3 className="text-sm font-semibold">
                  今日下载速度分布 <span className="font-normal text-foreground-subtle">≥256KB 的响应</span>
                </h3>
                {speedTotal === 0 ? (
                  <p className="text-sm text-foreground-muted">今日暂无样本</p>
                ) : (
                  <ul className="space-y-1.5">
                    {t.speed.map((n, i) => (
                      <li key={i} className="flex items-center gap-3 text-sm">
                        <span className="w-24 shrink-0 text-foreground-muted">{speedLabels[i]}</span>
                        <span className="h-2 flex-1 overflow-hidden rounded-full bg-secondary">
                          <span className="block h-full rounded-full" style={{ width: `${(n / speedTotal) * 100}%`, background: color }} />
                        </span>
                        <span className="w-20 shrink-0 text-right tabular-nums text-foreground-muted">{formatPct(n / speedTotal, 0)}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </section>

              <section className="space-y-2 border-t pt-4">
                <h3 className="text-sm font-semibold">最近请求</h3>
                {recent === null ? (
                  <Skeleton className="h-32 w-full" />
                ) : (
                  <div className="overflow-hidden rounded-lg border">
                    <RecentRequests rows={recent} nodes={[]} node={nodeId} onNodeChange={() => {}} colors={{}} compact />
                  </div>
                )}
              </section>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
