"use client"

import { useEffect, useState, useCallback, useRef } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import Link from "next/link"
import MetricTiles from "./components/MetricTiles"
import StatusCodeChart from "./components/StatusCodeChart"
import LatencyChart from "./components/LatencyChart"
import BandwidthChart from "./components/BandwidthChart"
import PathTrendChart from "./components/PathTrendChart"
import PathTotalsChart from "./components/PathTotalsChart"

interface Metrics {
  uptime: string
  active_requests: number
  total_requests: number
  total_errors: number
  num_goroutine: number
  memory_usage: string
  avg_response_time: string
  requests_per_second: number
  bytes_per_second: number
  error_rate: number
  status_code_stats: Record<string, number>
  recent_requests: Array<{
    Time: string
    Path: string
    Status: number
    Latency: number
    BytesSent: number
    ClientIP: string
    Referer?: string
  }>
  latency_stats: {
    min: string
    max: string
    distribution: Record<string, number>
  }
  bandwidth_history: Record<string, string>
  current_bandwidth: string
  total_bytes: number
  current_session_requests: number
  top_referers: Array<{
    path: string
    request_count: number
    error_count: number
    avg_latency: string
    bytes_transferred: number
    last_access_time: number
  }>
}

// RECENT_PAGE_SIZE 最近请求每次加载的步长; 后端 RequestQueue 上限 100, 所以 50 + 50 刚好两次到顶
const RECENT_PAGE_SIZE = 50
const RECENT_MAX = 100

export default function DashboardPage() {
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [recentLimit, setRecentLimit] = useState(RECENT_PAGE_SIZE)
  const { toast } = useToast()
  const router = useRouter()
  const failureCountRef = useRef(0)
  const lastErrorToastAtRef = useRef(0)

  const fetchMetrics = useCallback(async (): Promise<boolean> => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return false
      }
      const response = await fetch("/admin/api/metrics", {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return false
      }
      if (!response.ok) throw new Error("加载监控数据失败")
      const data = await response.json()
      setMetrics(data)
      setError(null)
      return true
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载监控数据失败"
      setError(msg)
      const now = Date.now()
      if (now - lastErrorToastAtRef.current > 30_000) {
        lastErrorToastAtRef.current = now
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
      const ok = await fetchMetrics()
      if (cancelled) return
      if (ok) failureCountRef.current = 0
      else failureCountRef.current = Math.min(failureCountRef.current + 1, 5)
      const delay = ok ? 1000 : Math.min(1000 * 2 ** failureCountRef.current, 30000)
      timer = setTimeout(tick, delay)
    }
    tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [fetchMetrics])

  if (loading && !metrics) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium">加载中...</div>
          <div className="mt-1 text-sm text-muted-foreground">
            正在获取监控数据
          </div>
        </div>
      </div>
    )
  }

  if ((error || !metrics) && !metrics) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium text-destructive">
            {error || "暂无数据"}
          </div>
          <div className="mt-1 text-sm text-muted-foreground">
            请检查后端服务是否正常运行
          </div>
          <button
            onClick={() => {
              void fetchMetrics()
            }}
            className="mt-4 rounded-md bg-primary px-4 py-2 text-primary-foreground transition-colors hover:bg-primary/90"
          >
            重试
          </button>
        </div>
      </div>
    )
  }

  if (!metrics) return null

  const recentRequests = metrics.recent_requests || []
  const recentShown = recentRequests.slice(0, recentLimit)
  const recentTotal = Math.min(recentRequests.length, RECENT_MAX)
  const canLoadMore = recentLimit < recentTotal

  return (
    <Tabs defaultValue="overview" className="space-y-6">
      <TabsList>
        <TabsTrigger value="overview">概览</TabsTrigger>
        <TabsTrigger value="traffic">流量</TabsTrigger>
        <TabsTrigger value="quality">质量</TabsTrigger>
        <TabsTrigger value="requests">请求</TabsTrigger>
      </TabsList>

      <TabsContent value="overview" className="space-y-6">
        <MetricTiles m={metrics} />
      </TabsContent>

      <TabsContent value="traffic" className="space-y-6">
        <PathTrendChart />
        <PathTotalsChart />
        <BandwidthChart
          history={metrics.bandwidth_history || {}}
          current={metrics.current_bandwidth}
        />
      </TabsContent>

      <TabsContent value="quality" className="space-y-6">
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          <StatusCodeChart stats={metrics.status_code_stats || {}} />
          <LatencyChart stats={metrics.latency_stats} />
        </div>
      </TabsContent>

      <TabsContent value="requests" className="space-y-6">
        {metrics.top_referers && metrics.top_referers.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>
                引用来源
                <span className="ml-2 text-sm font-normal text-muted-foreground">
                  (近 24 小时, {metrics.top_referers.length} 条)
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="p-2 text-left">来源</th>
                      <th className="p-2 text-left">请求</th>
                      <th className="p-2 text-left">错误</th>
                      <th className="p-2 text-left">错误率</th>
                      <th className="p-2 text-left">平均延迟</th>
                      <th className="p-2 text-left">流量</th>
                      <th className="p-2 text-left">最后访问</th>
                    </tr>
                  </thead>
                  <tbody>
                    {metrics.top_referers
                      .slice()
                      .sort((a, b) => b.request_count - a.request_count)
                      .map((r, i) => {
                        const errPct = ((r.error_count / r.request_count) * 100).toFixed(1)
                        const last = new Date(r.last_access_time * 1000)
                        return (
                          <tr key={i} className="border-b hover:bg-accent/30">
                            <td className="max-w-xs truncate p-2">
                              <a
                                href={r.path}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="hover:underline"
                              >
                                {r.path}
                              </a>
                            </td>
                            <td className="p-2">{r.request_count}</td>
                            <td className="p-2">{r.error_count}</td>
                            <td className="p-2">
                              <span
                                className={
                                  errPct === "0.0" ? "text-foreground" : "text-destructive"
                                }
                              >
                                {errPct}%
                              </span>
                            </td>
                            <td className="p-2">{r.avg_latency}</td>
                            <td className="p-2">{formatBytes(r.bytes_transferred)}</td>
                            <td className="p-2">
                              <span title={last.toLocaleString()}>{getTimeAgo(last)}</span>
                            </td>
                          </tr>
                        )
                      })}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        )}

        <Card>
          <CardHeader>
            <CardTitle>
              最近请求
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                显示 {recentShown.length} / {recentTotal} 条
              </span>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b">
                    <th className="p-2 text-left">时间</th>
                    <th className="p-2 text-left">路径</th>
                    <th className="p-2 text-left">来源</th>
                    <th className="p-2 text-left">状态</th>
                    <th className="p-2 text-left">延迟</th>
                    <th className="p-2 text-left">大小</th>
                    <th className="p-2 text-left">客户端</th>
                  </tr>
                </thead>
                <tbody>
                  {recentShown.map((req, i) => (
                    <tr key={i} className="border-b hover:bg-accent/30">
                      <td className="p-2 whitespace-nowrap">{formatDate(req.Time)}</td>
                      <td className="max-w-xs truncate p-2">
                        <a
                          href={req.Path}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="hover:underline"
                        >
                          {req.Path}
                        </a>
                      </td>
                      <td className="max-w-[14rem] truncate p-2">
                        {req.Referer ? (
                          <a
                            href={req.Referer}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="hover:underline"
                            title={req.Referer}
                          >
                            {req.Referer}
                          </a>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </td>
                      <td className="p-2">
                        <span
                          className={`rounded-full px-2 py-0.5 text-xs ${getStatusBadge(
                            req.Status
                          )}`}
                        >
                          {req.Status}
                        </span>
                      </td>
                      <td className="p-2 whitespace-nowrap">{formatLatency(req.Latency)}</td>
                      <td className="p-2 whitespace-nowrap">{formatBytes(req.BytesSent)}</td>
                      <td className="p-2 whitespace-nowrap">
                        <Link
                          href={`https://ip.czl.net/${req.ClientIP}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="hover:underline"
                        >
                          {req.ClientIP}
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {canLoadMore && (
              <div className="mt-4 flex justify-center">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    setRecentLimit((n) => Math.min(n + RECENT_PAGE_SIZE, RECENT_MAX))
                  }
                >
                  加载更多 (+{Math.min(RECENT_PAGE_SIZE, recentTotal - recentLimit)})
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>
  )
}

// formatBytes 字节数 → 友好单位
function formatBytes(bytes: number) {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// formatDate 时间戳字符串 → 本地时间
function formatDate(s: string) {
  if (!s) return "-"
  return new Date(s).toLocaleString()
}

// formatLatency 纳秒 → 自适应单位
function formatLatency(ns: number) {
  if (!ns || isNaN(ns)) return "-"
  if (ns < 1000) return `${ns} ns`
  if (ns < 1_000_000) return `${(ns / 1000).toFixed(2)} µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(2)} ms`
  return `${(ns / 1_000_000_000).toFixed(2)} s`
}

// getTimeAgo 相对时间显示
function getTimeAgo(date: Date) {
  const diff = Math.floor((Date.now() - date.getTime()) / 1000)
  if (diff < 60) return `${diff}秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  return date.toLocaleDateString()
}

// getStatusBadge HTTP 状态码 → 语义色 badge (规范: 错误/成功类直接用语义色, 不复用 chart 分类色)
function getStatusBadge(status: number) {
  if (status >= 500) return "bg-destructive/10 text-destructive"
  if (status >= 400) return "bg-warning/10 text-warning"
  if (status >= 300) return "bg-muted text-muted-foreground"
  return "bg-success/10 text-success"
}
