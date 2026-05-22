"use client"

import { useEffect, useMemo, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
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

interface DayPoint {
  date: string
  requests: number
  bytes: number
  errors: number
}

interface HostView {
  host: string
  daily: DayPoint[]
  total_requests: number
  total_bytes: number
  total_errors: number
  first_seen_date: string
  last_seen_date: string
}

interface ApiResponse {
  generated_at: number
  timezone: string
  days: number
  hosts: HostView[]
}

// 显示 Top N host, 默认 6 (折线图区分度的上限); 完整列表在下方表格里仍可查
const DEFAULT_VISIBLE = 6
const CHART_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
  "hsl(var(--chart-6))",
]

// RefererDailyChart 近 30 天每天每 host 的请求折线 + 总览表
// 用于排查"哪个站突然在用我的资源"
export default function RefererDailyChart() {
  const [data, setData] = useState<ApiResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [visibleHosts, setVisibleHosts] = useState<Set<string>>(new Set())

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const token = localStorage.getItem("token")
        const resp = await fetch("/admin/api/metrics/referer-daily?days=30&top=50", {
          headers: token ? { Authorization: `Bearer ${token}` } : undefined,
        })
        if (!resp.ok) throw new Error("加载 Referer 天序列失败")
        const json = (await resp.json()) as ApiResponse
        if (cancelled) return
        setData(json)
        setVisibleHosts(new Set(json.hosts.slice(0, DEFAULT_VISIBLE).map((h) => h.host)))
        setError(null)
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : "加载失败")
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => {
      cancelled = true
    }
  }, [])

  // chartData 把"每 host 的 daily 数组"翻转为 recharts 需要的"每天一行, 每 host 一列"
  const chartData = useMemo(() => {
    if (!data || data.hosts.length === 0) return []
    const dates = data.hosts[0].daily.map((d) => d.date)
    return dates.map((date, idx) => {
      const row: Record<string, string | number> = { date }
      for (const host of data.hosts) {
        if (visibleHosts.has(host.host)) {
          row[host.host] = host.daily[idx]?.requests ?? 0
        }
      }
      return row
    })
  }, [data, visibleHosts])

  const toggleHost = (host: string) => {
    setVisibleHosts((prev) => {
      const next = new Set(prev)
      if (next.has(host)) next.delete(host)
      else next.add(host)
      return next
    })
  }

  if (loading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>近 30 天引用来源 (Referer host)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex h-48 items-center justify-center text-muted-foreground">
            加载中...
          </div>
        </CardContent>
      </Card>
    )
  }

  if (error || !data) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>近 30 天引用来源 (Referer host)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex h-48 items-center justify-center text-destructive">
            {error || "暂无数据"}
          </div>
        </CardContent>
      </Card>
    )
  }

  if (data.hosts.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>近 30 天引用来源 (Referer host)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex h-48 items-center justify-center text-muted-foreground text-sm">
            暂无数据 (仅记录单日请求 ≥ 10 的 host, 至少需要等到第一个累计日才会出现)
          </div>
        </CardContent>
      </Card>
    )
  }

  const visibleHostList = data.hosts.filter((h) => visibleHosts.has(h.host))

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          近 30 天引用来源 (Referer host)
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            共 {data.hosts.length} 个 host (按总请求降序)
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="h-72 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
              <XAxis dataKey="date" stroke="hsl(var(--muted-foreground))" fontSize={11} />
              <YAxis stroke="hsl(var(--muted-foreground))" fontSize={11} />
              <Tooltip
                contentStyle={{
                  background: "hsl(var(--popover))",
                  border: "1px solid hsl(var(--border))",
                  borderRadius: 8,
                  color: "hsl(var(--popover-foreground))",
                }}
                formatter={(v: number) => v.toLocaleString()}
              />
              <Legend wrapperStyle={{ fontSize: 12 }} />
              {visibleHostList.map((host, i) => (
                <Line
                  key={host.host}
                  type="monotone"
                  dataKey={host.host}
                  stroke={CHART_COLORS[i % CHART_COLORS.length]}
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>

        <div className="space-y-2">
          <div className="text-sm font-medium">点击切换显示 (绿色=显示中)</div>
          <div className="flex flex-wrap gap-2">
            {data.hosts.map((h) => {
              const on = visibleHosts.has(h.host)
              return (
                <button
                  key={h.host}
                  type="button"
                  onClick={() => toggleHost(h.host)}
                  className="focus:outline-none"
                >
                  <Badge
                    variant={on ? "default" : "outline"}
                    className="cursor-pointer font-mono text-xs"
                  >
                    {h.host}
                    <span className="ml-1 text-[10px] opacity-75">
                      {h.total_requests.toLocaleString()}
                    </span>
                  </Badge>
                </button>
              )
            })}
          </div>
        </div>

        <details className="text-sm">
          <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
            查看完整列表 ({data.hosts.length} 个 host)
          </summary>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  <th className="p-2 text-left">Host</th>
                  <th className="p-2 text-right">总请求</th>
                  <th className="p-2 text-right">总流量</th>
                  <th className="p-2 text-right">总错误</th>
                  <th className="p-2 text-left">首次出现</th>
                  <th className="p-2 text-left">最近活跃</th>
                </tr>
              </thead>
              <tbody>
                {data.hosts.map((h) => (
                  <tr key={h.host} className="border-b hover:bg-accent/30">
                    <td className="p-2 font-mono">{h.host}</td>
                    <td className="p-2 text-right tabular-nums">
                      {h.total_requests.toLocaleString()}
                    </td>
                    <td className="p-2 text-right tabular-nums">{formatBytes(h.total_bytes)}</td>
                    <td className="p-2 text-right tabular-nums">
                      {h.total_errors > 0 ? (
                        <span className="text-destructive">
                          {h.total_errors.toLocaleString()}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">0</span>
                      )}
                    </td>
                    <td className="p-2 text-muted-foreground">{h.first_seen_date || "-"}</td>
                    <td className="p-2 text-muted-foreground">{h.last_seen_date || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      </CardContent>
    </Card>
  )
}

function formatBytes(bytes: number) {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}
