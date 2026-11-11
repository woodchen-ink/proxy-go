"use client"

import { useCallback, useMemo, useState } from "react"
import { Bar, CartesianGrid, ComposedChart, Line, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { usePolling } from "@/hooks/use-polling"
import { fetchApi } from "@/lib/api-client"
import { formatBytes, formatCount, formatPct } from "@/lib/format"
import { cn } from "@/lib/utils"

interface Point {
  requests: number
  bytes: number
  errors: number
}
interface HourPoint extends Point {
  hour: number
}
interface DayPoint extends Point {
  date: string
}
interface PathSeries {
  path: string
  today: HourPoint[]
  yesterday: HourPoint[]
  daily: DayPoint[]
  today_requests: number
  today_bytes: number
  month_requests: number
  month_bytes: number
}
interface TimeseriesResponse {
  days: number
  paths: PathSeries[]
}

type RangeKey = "today" | "30d"
type MetricKey = "requests" | "bytes"

// 选中路径各占一个 chart 色, 其余合并成"其他"一段, 堆起来始终等于全站总量
const MAX_SELECTED = 5
const OTHER = "__other"
const OTHER_COLOR = "hsl(var(--foreground-subtle) / 0.35)"
const pathColor = (i: number) => `hsl(var(--chart-${(i % MAX_SELECTED) + 1}))`

const fmt = (v: number, metric: MetricKey) => (metric === "bytes" ? formatBytes(v) : formatCount(v))

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

function Stat({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: "up" | "down" }) {
  return (
    <div>
      <div className="text-sm text-foreground-muted">{label}</div>
      <div className="mt-0.5 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tabular-nums">{value}</span>
        {sub && (
          <span className={cn("text-sm tabular-nums", tone === "up" ? "text-success" : tone === "down" ? "text-destructive" : "text-foreground-subtle")}>{sub}</span>
        )}
      </div>
    </div>
  )
}

// PathTrendChart 路径访问趋势: 今日按小时 / 近 30 天按天的堆叠柱, 今日叠一条"昨日同时刻"总量对比线
export default function PathTrendChart() {
  const [data, setData] = useState<TimeseriesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [range, setRange] = useState<RangeKey>("today")
  const [metric, setMetric] = useState<MetricKey>("requests")
  const [picked, setPicked] = useState<string[] | null>(null) // null = 跟随默认 Top 5

  const load = useCallback(async () => {
    try {
      const res = await fetchApi("/admin/api/metrics/timeseries?days=30")
      if (!res.ok) throw new Error("加载趋势数据失败")
      setData(await res.json())
      setError(null)
      return true
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载趋势数据失败")
      return false
    }
  }, [])
  usePolling(load, 60_000)

  // 路径按当前视图的量排序 (今日看今日、30 天看 30 天), 不按历史累计: 早已没人访问的路径不该占着前排
  const ranked = useMemo(() => {
    const val = (p: PathSeries) =>
      range === "today" ? (metric === "bytes" ? p.today_bytes : p.today_requests) : metric === "bytes" ? p.month_bytes : p.month_requests
    return [...(data?.paths ?? [])].map((p) => ({ p, v: val(p) })).sort((a, b) => b.v - a.v)
  }, [data, range, metric])
  const selected = picked ?? ranked.filter((r) => r.v > 0).slice(0, MAX_SELECTED).map((r) => r.p.path)

  const view = useMemo(() => {
    if (!data) return null
    const now = new Date()
    const curHour = now.getHours()
    const sel = new Set(selected)
    const pick = (pt: Point | undefined) => (pt ? pt[metric] : 0)

    if (range === "today") {
      const rows = Array.from({ length: 24 }, (_, h) => {
        const row: Record<string, number | string | null> = { x: `${h}:00` }
        let other = 0
        let yesterday = 0
        for (const s of data.paths) {
          const v = h <= curHour ? pick(s.today.find((t) => t.hour === h)) : 0
          if (sel.has(s.path)) row[s.path] = v
          else other += v
          yesterday += pick(s.yesterday.find((t) => t.hour === h))
        }
        row[OTHER] = other
        row.yesterday = yesterday
        return row
      })
      const sum = (key: "today" | "yesterday", upto: number) =>
        data.paths.reduce((a, s) => a + s[key].filter((t) => t.hour <= upto).reduce((b, t) => b + t[metric], 0), 0)
      const today = sum("today", curHour)
      const ySame = sum("yesterday", curHour)
      let peakHour = 0
      let peak = -1
      for (let hr = 0; hr <= curHour; hr++) {
        const v = data.paths.reduce((a, s) => a + pick(s.today.find((t) => t.hour === hr)), 0)
        if (v > peak) {
          peak = v
          peakHour = hr
        }
      }
      return { rows, today, ySame, peakLabel: `${peakHour}:00`, peak }
    }

    const dates = data.paths[0]?.daily.map((d) => d.date) ?? []
    const rows = dates.map((date) => {
      const row: Record<string, number | string> = { x: date.slice(5) }
      let other = 0
      for (const s of data.paths) {
        const v = pick(s.daily.find((d) => d.date === date))
        if (sel.has(s.path)) row[s.path] = v
        else other += v
      }
      row[OTHER] = other
      return row
    })
    const dayTotals = dates.map((date) => data.paths.reduce((a, s) => a + pick(s.daily.find((d) => d.date === date)), 0))
    const total = dayTotals.reduce((a, b) => a + b, 0)
    const peakIdx = dayTotals.indexOf(Math.max(...dayTotals, 0))
    return { rows, today: total, ySame: 0, peakLabel: dates[peakIdx]?.slice(5) ?? "-", peak: dayTotals[peakIdx] ?? 0, avg: dates.length ? total / dates.length : 0 }
  }, [data, range, metric, selected])

  const toggle = (path: string) => {
    const cur = selected
    if (cur.includes(path)) setPicked(cur.filter((p) => p !== path))
    else setPicked([...cur, path].slice(-MAX_SELECTED)) // 超过 5 条时挤掉最早选的
  }

  const change = view && view.ySame > 0 ? view.today / view.ySame - 1 : null

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <h2 className="text-base font-semibold">路径访问趋势</h2>
        <div className="flex flex-wrap gap-3">
          <Pills
            label="指标"
            items={[
              { value: "requests", label: "请求数" },
              { value: "bytes", label: "流量" },
            ]}
            value={metric}
            onChange={setMetric}
          />
          <Pills
            label="时间范围"
            items={[
              { value: "today", label: "今日" },
              { value: "30d", label: "近 30 天" },
            ]}
            value={range}
            onChange={setRange}
          />
        </div>
      </div>

      <div className="space-y-4 px-5 py-4">
        {!data && !error ? (
          <Skeleton className="h-80 w-full" />
        ) : error && !data ? (
          <div className="flex h-80 items-center justify-center text-sm text-destructive">{error}</div>
        ) : !view || data!.paths.length === 0 ? (
          <div className="flex h-80 items-center justify-center rounded-lg bg-muted text-sm text-foreground-muted">暂无数据, 节点每 5 分钟上报一次</div>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
              {range === "today" ? (
                <>
                  <Stat
                    label="今日累计"
                    value={fmt(view.today, metric)}
                    sub={change === null ? "昨日同期无数据" : `${change >= 0 ? "+" : ""}${formatPct(change)} 较昨日同期`}
                    tone={change === null ? undefined : change >= 0 ? "up" : "down"}
                  />
                  <Stat label="昨日同期" value={fmt(view.ySame, metric)} />
                  <Stat label="今日峰值时段" value={view.peakLabel} sub={fmt(Math.max(view.peak, 0), metric)} />
                </>
              ) : (
                <>
                  <Stat label="近 30 天合计" value={fmt(view.today, metric)} />
                  <Stat label="日均" value={fmt(view.avg ?? 0, metric)} />
                  <Stat label="最高的一天" value={view.peakLabel} sub={fmt(view.peak, metric)} />
                </>
              )}
            </div>

            <div className="h-72 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <ComposedChart data={view.rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barCategoryGap="18%">
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
                  <XAxis dataKey="x" stroke="hsl(var(--foreground-subtle))" fontSize={12} tickLine={false} minTickGap={12} />
                  <YAxis
                    tickFormatter={(v: number) => fmt(v, metric)}
                    stroke="hsl(var(--foreground-subtle))"
                    fontSize={12}
                    tickLine={false}
                    axisLine={false}
                    width={64}
                  />
                  <Tooltip
                    cursor={{ fill: "hsl(var(--accent))" }}
                    contentStyle={{
                      background: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: 10,
                      color: "hsl(var(--popover-foreground))",
                      fontSize: 13,
                    }}
                    formatter={(v: number, name: string) => [fmt(v, metric), name === OTHER ? "其他路径" : name === "yesterday" ? "昨日同时刻 (全部)" : name]}
                  />
                  {selected.map((p, i) => (
                    <Bar key={p} dataKey={p} name={p} stackId="a" fill={pathColor(i)} isAnimationActive={false} />
                  ))}
                  <Bar dataKey={OTHER} name={OTHER} stackId="a" fill={OTHER_COLOR} radius={[3, 3, 0, 0]} isAnimationActive={false} />
                  {range === "today" && (
                    <Line
                      dataKey="yesterday"
                      name="yesterday"
                      type="linear"
                      stroke="hsl(var(--foreground-muted))"
                      strokeWidth={1.5}
                      strokeDasharray="4 3"
                      dot={false}
                      isAnimationActive={false}
                    />
                  )}
                </ComposedChart>
              </ResponsiveContainer>
            </div>

            <div className="flex flex-wrap items-center gap-x-1 gap-y-1.5 border-t pt-3 text-sm">
              {ranked.filter((r) => r.v > 0 || selected.includes(r.p.path)).map(({ p, v }) => {
                const idx = selected.indexOf(p.path)
                const on = idx >= 0
                return (
                  <button
                    key={p.path}
                    onClick={() => toggle(p.path)}
                    aria-pressed={on}
                    className={cn(
                      "inline-flex h-7 items-center gap-1.5 rounded-md px-2 transition-colors hover:bg-accent",
                      on ? "text-foreground" : "text-foreground-subtle",
                    )}
                  >
                    <span aria-hidden className="h-2.5 w-2.5 rounded-sm" style={{ background: on ? pathColor(idx) : OTHER_COLOR }} />
                    <span className={cn("font-mono", on && "font-medium")}>{p.path}</span>
                    <span className="tabular-nums text-foreground-subtle">{fmt(v, metric)}</span>
                  </button>
                )
              })}
              {ranked.some((r) => r.v === 0) && (
                <span className="px-2 text-foreground-subtle">
                  另有 {ranked.filter((r) => r.v === 0).length} 个路径{range === "today" ? "今日" : "近 30 天"}无访问
                </span>
              )}
              {range === "today" && (
                <span className="ml-auto inline-flex items-center gap-1.5 text-foreground-muted">
                  <span aria-hidden className="w-4 border-t-[1.5px] border-dashed border-foreground-muted" />
                  昨日同时刻
                </span>
              )}
            </div>
            <p className="text-xs text-foreground-subtle">点路径单独着色 (最多 {MAX_SELECTED} 条), 未选中的合并为灰色「其他」</p>
          </>
        )}
      </div>
    </Card>
  )
}
