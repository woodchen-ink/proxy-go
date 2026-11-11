"use client"

import { useCallback, useState } from "react"
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { usePolling } from "@/hooks/use-polling"
import { fetchApi } from "@/lib/api-client"
import { formatBps, formatBytes, formatCount } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { NodeSeriesPoint, NodesSeries, SeriesRange } from "../node-types"

type Metric = "bandwidth" | "requests" | "origin"

const RANGES: { value: SeriesRange; label: string; poll: number }[] = [
  { value: "1h", label: "1 小时", poll: 15_000 },
  { value: "24h", label: "24 小时", poll: 60_000 },
  { value: "30d", label: "30 天", poll: 300_000 },
]

const METRICS: { value: Metric; label: string }[] = [
  { value: "bandwidth", label: "带宽" },
  { value: "requests", label: "请求" },
  { value: "origin", label: "回源" },
]

function pick(p: NodeSeriesPoint, metric: Metric, step: number): number {
  if (metric === "bandwidth") return p.bandwidth_bps
  if (metric === "requests") return p.requests
  return p.origin_bytes / step // 回源也按速率画, 与带宽同量纲
}

function formatValue(v: number, metric: Metric): string {
  return metric === "requests" ? formatCount(v) : formatBps(v)
}

function formatTick(ts: number | string, range: SeriesRange): string {
  if (typeof ts === "string") return ts.slice(5) // MM-DD
  const d = new Date(ts * 1000)
  const hh = String(d.getHours()).padStart(2, "0")
  const mm = String(d.getMinutes()).padStart(2, "0")
  return range === "1h" ? `${hh}:${mm}` : `${hh}:00`
}

function Pills<T extends string>({ items, value, onChange, label }: { items: { value: T; label: string }[]; value: T; onChange: (v: T) => void; label: string }) {
  return (
    <div role="radiogroup" aria-label={label} className="flex gap-1">
      {items.map((it) => (
        <button
          key={it.value}
          role="radio"
          aria-checked={value === it.value}
          onClick={() => onChange(it.value)}
          className={cn(
            "h-7 rounded-full border px-3 text-sm transition-colors",
            value === it.value ? "border-primary bg-primary text-primary-foreground" : "text-foreground-muted hover:bg-accent hover:text-foreground",
          )}
        >
          {it.label}
        </button>
      ))}
    </div>
  )
}

// NodeTrafficPanel 按节点堆叠的流量趋势: 1 小时为分钟粒度 (实时快照), 24 小时为小时粒度, 30 天为天粒度
export default function NodeTrafficPanel({ colors }: { colors: Record<string, string> }) {
  const [range, setRange] = useState<SeriesRange>("24h")
  const [metric, setMetric] = useState<Metric>("bandwidth")
  const [data, setData] = useState<NodesSeries | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await fetchApi(`/admin/api/nodes/series?range=${range}`)
      if (!res.ok) throw new Error("加载节点流量失败")
      const body: NodesSeries = await res.json()
      setData(body)
      setError(null)
      return true
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载节点流量失败")
      return false
    }
  }, [range])

  usePolling(load, RANGES.find((r) => r.value === range)!.poll, [range])

  const current = data && data.range === range ? data : null
  const ids = current?.node_ids ?? []
  const rows = current
    ? current.buckets.map((_, i) => {
        const row: Record<string, number | string> = { label: current.labels[i] }
        for (const id of ids) row[id] = pick(current.series[id][i], metric, current.step)
        return row
      })
    : []

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <h2 className="text-base font-semibold">节点流量</h2>
        <div className="flex flex-wrap gap-3">
          <Pills label="指标" items={METRICS} value={metric} onChange={setMetric} />
          <Pills label="时间范围" items={RANGES} value={range} onChange={setRange} />
        </div>
      </div>
      <div className="px-5 py-4">
        {!current && !error ? (
          <Skeleton className="h-64 w-full" />
        ) : error && !current ? (
          <div className="flex h-64 items-center justify-center text-sm text-destructive">{error}</div>
        ) : ids.length === 0 ? (
          <div className="flex h-64 items-center justify-center rounded-lg bg-muted text-sm text-foreground-muted">
            {range === "1h" ? "没有在线且已升级的节点, 分钟级数据来自节点实时快照" : "暂无数据, 节点升级后每 5 分钟上报一次"}
          </div>
        ) : (
          <div className="h-64 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                <XAxis
                  dataKey="label"
                  tickFormatter={(v) => formatTick(v, range)}
                  stroke="hsl(var(--foreground-subtle))"
                  fontSize={12}
                  tickLine={false}
                  minTickGap={24}
                />
                <YAxis
                  tickFormatter={(v: number) => formatValue(v, metric)}
                  stroke="hsl(var(--foreground-subtle))"
                  fontSize={12}
                  tickLine={false}
                  axisLine={false}
                  width={72}
                />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 10,
                    color: "hsl(var(--popover-foreground))",
                    fontSize: 13,
                  }}
                  labelFormatter={(v) => (typeof v === "string" ? v : new Date(Number(v) * 1000).toLocaleString("zh-CN", { hour12: false }))}
                  formatter={(v: number, name: string) => [formatValue(v, metric), name]}
                />
                {ids.map((id) => (
                  <Area
                    key={id}
                    type="monotone"
                    dataKey={id}
                    stackId="nodes"
                    stroke={colors[id] ?? "hsl(var(--chart-1))"}
                    fill={colors[id] ?? "hsl(var(--chart-1))"}
                    fillOpacity={0.25}
                    strokeWidth={1.5}
                    isAnimationActive={false}
                  />
                ))}
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
        {current && ids.length > 0 && (
          <ul className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-sm">
            {ids.map((id) => {
              const t = current.totals[id]
              return (
                <li key={id} className="flex items-center gap-1.5">
                  <span aria-hidden className="h-2.5 w-2.5 rounded-sm" style={{ background: colors[id] ?? "hsl(var(--chart-1))" }} />
                  <span className="font-medium">{id}</span>
                  <span className="text-foreground-muted tabular-nums">
                    {metric === "requests" ? `${formatCount(t?.requests ?? 0)} 次` : formatBytes(metric === "origin" ? t?.origin_bytes ?? 0 : t?.bytes ?? 0)}
                  </span>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </Card>
  )
}
