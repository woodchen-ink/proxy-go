"use client"

import { useEffect, useMemo, useRef, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs"
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  Legend,
} from "recharts"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"

interface HourPoint {
  hour: number
  requests: number
  bytes: number
  errors: number
}

interface DayPoint {
  date: string
  requests: number
  bytes: number
  errors: number
}

interface PathSeries {
  path: string
  today: HourPoint[]
  yesterday: HourPoint[]
  daily: DayPoint[]
  today_requests: number
  today_bytes: number
  yesterday_requests: number
  yesterday_bytes: number
  month_requests: number
  month_bytes: number
}

interface TimeseriesResponse {
  generated_at: number
  timezone: string
  days: number
  paths: PathSeries[]
}

type RangeKey = "today_yesterday" | "30d"
type MetricKey = "requests" | "bytes"

// 图表系列颜色, 引用 globals.css 已定义的 chart token
const CHART_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
]

// formatBytes 把字节数格式化为 B/KB/MB/GB
function formatBytes(bytes: number): string {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// formatNumber 简化的数字格式化, 万级以上加 K/M 缩写
function formatNumber(n: number): string {
  if (!n) return "0"
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toString()
}

// fillTodayYesterday 把今日 / 昨日两条小时序列拼装成 24 行 chart 数据
//
// 缺失小时填 0, x 轴使用 0..23 小时数, 配合 monotone 平滑插值
function fillTodayYesterday(series: PathSeries[], paths: string[], metric: MetricKey) {
  const rows = Array.from({ length: 24 }, (_, h) => {
    const row: Record<string, number | string> = { hour: `${h}:00` }
    for (const p of paths) {
      const s = series.find((x) => x.path === p)
      const tdy = s?.today.find((x) => x.hour === h)
      const yst = s?.yesterday.find((x) => x.hour === h)
      row[`${p}__today`] = tdy ? tdy[metric] : 0
      row[`${p}__yesterday`] = yst ? yst[metric] : 0
    }
    return row
  })
  return rows
}

// fillDaily 把近 N 天的日序列拼装成 chart 数据
function fillDaily(series: PathSeries[], paths: string[], metric: MetricKey) {
  const allDates = new Set<string>()
  series.forEach((s) => s.daily.forEach((d) => allDates.add(d.date)))
  const dates = Array.from(allDates).sort()
  return dates.map((date) => {
    const row: Record<string, number | string> = {
      date: date.slice(5), // MM-DD 显示
    }
    for (const p of paths) {
      const s = series.find((x) => x.path === p)
      const dp = s?.daily.find((x) => x.date === date)
      row[p] = dp ? dp[metric] : 0
    }
    return row
  })
}

// PathTrendChart 主路径趋势图组件: 今日/昨日 + 近 30 天的请求与流量
export default function PathTrendChart() {
  const [data, setData] = useState<TimeseriesResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [range, setRange] = useState<RangeKey>("today_yesterday")
  const [metric, setMetric] = useState<MetricKey>("requests")
  const [selectedPaths, setSelectedPaths] = useState<string[]>([])
  const failureCount = useRef(0)
  const { toast } = useToast()
  const router = useRouter()
  const lastErrorAt = useRef(0)

  const fetchData = useCallback(async (): Promise<boolean> => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return false
      }
      const resp = await fetch("/admin/api/metrics/timeseries?days=30", {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (resp.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return false
      }
      if (!resp.ok) throw new Error("加载趋势数据失败")
      const json = (await resp.json()) as TimeseriesResponse
      setData(json)
      setError(null)
      // 默认选中 Top 5 路径 (按近 30 天请求数排序, 后端已排好)
      setSelectedPaths((prev) => {
        if (prev.length > 0) return prev
        return json.paths.slice(0, 5).map((p) => p.path)
      })
      return true
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载趋势数据失败"
      setError(msg)
      const now = Date.now()
      if (now - lastErrorAt.current > 30_000) {
        lastErrorAt.current = now
        toast({ title: "错误", description: msg, variant: "destructive" })
      }
      return false
    } finally {
      setLoading(false)
    }
  }, [router, toast])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const tick = async () => {
      if (cancelled) return
      const ok = await fetchData()
      if (cancelled) return
      if (ok) failureCount.current = 0
      else failureCount.current = Math.min(failureCount.current + 1, 5)
      const delay = ok ? 30_000 : Math.min(1000 * 2 ** failureCount.current, 60_000)
      timer = setTimeout(tick, delay)
    }
    tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [fetchData])

  const togglePath = (path: string) => {
    setSelectedPaths((prev) =>
      prev.includes(path) ? prev.filter((p) => p !== path) : [...prev, path]
    )
  }

  const chartData = useMemo(() => {
    if (!data) return []
    if (range === "today_yesterday") {
      return fillTodayYesterday(data.paths, selectedPaths, metric)
    }
    return fillDaily(data.paths, selectedPaths, metric)
  }, [data, range, metric, selectedPaths])

  const yFormatter = metric === "bytes" ? (v: number) => formatBytes(v) : formatNumber

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>路径访问趋势</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <Tabs value={metric} onValueChange={(v) => setMetric(v as MetricKey)}>
              <TabsList>
                <TabsTrigger value="requests">请求数</TabsTrigger>
                <TabsTrigger value="bytes">流量</TabsTrigger>
              </TabsList>
            </Tabs>
            <Select value={range} onValueChange={(v) => setRange(v as RangeKey)}>
              <SelectTrigger className="w-36">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="today_yesterday">今日 vs 昨日</SelectItem>
                <SelectItem value="30d">近 30 天</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {loading && !data ? (
          <div className="flex h-64 items-center justify-center text-muted-foreground">
            加载中...
          </div>
        ) : error && !data ? (
          <div className="flex h-64 items-center justify-center text-destructive">
            {error}
          </div>
        ) : !data || data.paths.length === 0 ? (
          <div className="flex h-64 items-center justify-center text-muted-foreground">
            暂无数据 (D1 时间序列上报后才会有图)
          </div>
        ) : (
          <>
            <PathSelector
              paths={data.paths}
              selected={selectedPaths}
              onToggle={togglePath}
              metric={metric}
            />
            <div className="mt-4 h-72 w-full">
              <ResponsiveContainer width="100%" height="100%">
                {range === "today_yesterday" ? (
                  <LineChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                    <XAxis
                      dataKey="hour"
                      stroke="hsl(var(--muted-foreground))"
                      fontSize={12}
                    />
                    <YAxis
                      stroke="hsl(var(--muted-foreground))"
                      fontSize={12}
                      tickFormatter={yFormatter}
                    />
                    <Tooltip
                      contentStyle={{
                        background: "hsl(var(--popover))",
                        border: "1px solid hsl(var(--border))",
                        borderRadius: 8,
                        color: "hsl(var(--popover-foreground))",
                      }}
                      formatter={(v: number) =>
                        metric === "bytes" ? formatBytes(v) : v.toLocaleString()
                      }
                    />
                    <Legend />
                    {selectedPaths.flatMap((p, idx) => {
                      const color = CHART_COLORS[idx % CHART_COLORS.length]
                      return [
                        <Line
                          key={`${p}-today`}
                          type="monotone"
                          dataKey={`${p}__today`}
                          name={`${p} 今日`}
                          stroke={color}
                          strokeWidth={2}
                          dot={false}
                          isAnimationActive={false}
                        />,
                        <Line
                          key={`${p}-yesterday`}
                          type="monotone"
                          dataKey={`${p}__yesterday`}
                          name={`${p} 昨日`}
                          stroke={color}
                          strokeWidth={1.5}
                          strokeDasharray="4 4"
                          dot={false}
                          isAnimationActive={false}
                        />,
                      ]
                    })}
                  </LineChart>
                ) : (
                  <LineChart data={chartData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                    <XAxis
                      dataKey="date"
                      stroke="hsl(var(--muted-foreground))"
                      fontSize={12}
                    />
                    <YAxis
                      stroke="hsl(var(--muted-foreground))"
                      fontSize={12}
                      tickFormatter={yFormatter}
                    />
                    <Tooltip
                      contentStyle={{
                        background: "hsl(var(--popover))",
                        border: "1px solid hsl(var(--border))",
                        borderRadius: 8,
                        color: "hsl(var(--popover-foreground))",
                      }}
                      formatter={(v: number) =>
                        metric === "bytes" ? formatBytes(v) : v.toLocaleString()
                      }
                    />
                    <Legend />
                    {selectedPaths.map((p, idx) => (
                      <Line
                        key={p}
                        type="monotone"
                        dataKey={p}
                        name={p}
                        stroke={CHART_COLORS[idx % CHART_COLORS.length]}
                        strokeWidth={2}
                        dot={false}
                        isAnimationActive={false}
                      />
                    ))}
                  </LineChart>
                )}
              </ResponsiveContainer>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

// PathSelector 路径多选器, 默认 Top 5, 点击切换包含 / 排除
function PathSelector({
  paths,
  selected,
  onToggle,
  metric,
}: {
  paths: PathSeries[]
  selected: string[]
  onToggle: (p: string) => void
  metric: MetricKey
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {paths.map((p) => {
        const isOn = selected.includes(p.path)
        const color = CHART_COLORS[selected.indexOf(p.path) % CHART_COLORS.length]
        const sub =
          metric === "bytes"
            ? formatBytes(p.month_bytes)
            : `${formatNumber(p.month_requests)} 次`
        return (
          <button
            key={p.path}
            onClick={() => onToggle(p.path)}
            className={`group flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition-colors ${
              isOn
                ? "border-foreground bg-secondary"
                : "border-border bg-card hover:bg-accent/40"
            }`}
          >
            <span
              className="inline-block h-2.5 w-2.5 rounded-full"
              style={{
                background: isOn ? color : "hsl(var(--muted-foreground))",
                opacity: isOn ? 1 : 0.4,
              }}
            />
            <span className={isOn ? "font-medium" : "text-muted-foreground"}>
              {p.path}
            </span>
            <span className="text-xs text-muted-foreground">{sub}</span>
          </button>
        )
      })}
    </div>
  )
}
