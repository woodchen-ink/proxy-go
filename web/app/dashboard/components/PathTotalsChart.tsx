"use client"

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs"
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"

interface PathStat {
  path: string
  request_count: number
  error_count: number
  bytes_transferred: number
  status_2xx: number
  status_3xx: number
  status_4xx: number
  status_5xx: number
  cache_hits: number
  cache_misses: number
  cache_hit_rate: number
  bytes_saved: number
  avg_latency: string
  last_access_time: number
}

type MetricKey = "request_count" | "bytes_transferred"

// formatBytes 字节数 → 友好单位
function formatBytes(bytes: number): string {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// formatNumber 万级以上加 K/M 缩写, 用于 Y 轴刻度
function formatNumber(n: number): string {
  if (!n) return "0"
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toString()
}

// PathTotalsChart 各路径累计请求数 / 流量横向柱状图
//
// 数据来自 /admin/api/path-stats (累计值, 跨重启), 和 PathTrendChart 不同视角:
// 后者看时间维度趋势, 这里看 "总量榜单"
export default function PathTotalsChart() {
  const [stats, setStats] = useState<PathStat[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [metric, setMetric] = useState<MetricKey>("request_count")
  const failureCount = useRef(0)
  const lastErrorAt = useRef(0)
  const { toast } = useToast()
  const router = useRouter()

  const fetchStats = useCallback(async (): Promise<boolean> => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return false
      }
      const resp = await fetch("/admin/api/path-stats", {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (resp.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return false
      }
      if (!resp.ok) throw new Error("加载路径统计失败")
      const json = (await resp.json()) as { path_stats: PathStat[] }
      setStats(json.path_stats || [])
      setError(null)
      return true
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载路径统计失败"
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
      const ok = await fetchStats()
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
  }, [fetchStats])

  // 按当前指标降序, 取前 12 条避免柱子太密; 路径多时用户依赖排序识别
  const sorted = useMemo(() => {
    return [...stats]
      .filter((s) => s[metric] > 0)
      .sort((a, b) => b[metric] - a[metric])
      .slice(0, 12)
      .map((s) => ({ ...s, _value: s[metric] }))
  }, [stats, metric])

  const yFormatter = metric === "bytes_transferred" ? formatBytes : formatNumber

  // 横向 BarChart: layout="vertical" + XAxis dataKey 在数值轴, YAxis dataKey 在分类轴
  // 按 12 条估算高度, 单条约 28px + 上下留白
  const chartHeight = Math.max(220, sorted.length * 32 + 60)

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>路径累计排行</CardTitle>
          <Tabs value={metric} onValueChange={(v) => setMetric(v as MetricKey)}>
            <TabsList>
              <TabsTrigger value="request_count">总请求数</TabsTrigger>
              <TabsTrigger value="bytes_transferred">总流量</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </CardHeader>
      <CardContent>
        {loading && stats.length === 0 ? (
          <div className="flex h-48 items-center justify-center text-muted-foreground">
            加载中...
          </div>
        ) : error && stats.length === 0 ? (
          <div className="flex h-48 items-center justify-center text-destructive">
            {error}
          </div>
        ) : sorted.length === 0 ? (
          <div className="flex h-48 items-center justify-center text-muted-foreground">
            暂无数据
          </div>
        ) : (
          <div className="w-full" style={{ height: chartHeight }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart
                data={sorted}
                layout="vertical"
                margin={{ top: 8, right: 24, bottom: 8, left: 8 }}
              >
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" horizontal={false} />
                <XAxis
                  type="number"
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={12}
                  tickFormatter={yFormatter}
                />
                <YAxis
                  type="category"
                  dataKey="path"
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={12}
                  width={120}
                  tickFormatter={(v: string) => (v.length > 16 ? v.slice(0, 14) + "…" : v)}
                />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 8,
                    color: "hsl(var(--popover-foreground))",
                  }}
                  formatter={(v: number) =>
                    metric === "bytes_transferred"
                      ? formatBytes(v)
                      : v.toLocaleString()
                  }
                  labelFormatter={(label: string) => label}
                />
                <Bar
                  dataKey="_value"
                  fill="hsl(var(--foreground))"
                  radius={[0, 4, 4, 0]}
                  isAnimationActive={false}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
