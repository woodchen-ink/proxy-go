"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import Link from "next/link"

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
  }>
  latency_stats: {
    min: string
    max: string
    distribution: Record<string, number>
  }
  bandwidth_history: Record<string, string>
  current_bandwidth: string
  total_bytes: number
  top_referers: Array<{
    path: string
    request_count: number
    error_count: number
    avg_latency: string
    bytes_transferred: number
  }>
}

export default function DashboardPage() {
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const { toast } = useToast()
  const router = useRouter()

  const fetchMetrics = useCallback(async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/metrics", {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) {
        throw new Error("加载监控数据失败")
      }

      const data = await response.json()
      setMetrics(data)
      setError(null)
    } catch (error) {
      const message = error instanceof Error ? error.message : "加载监控数据失败"
      setError(message)
      toast({
        title: "错误",
        description: message,
        variant: "destructive",
      })
    } finally {
      setLoading(false)
    }
  }, [router, toast])

  useEffect(() => {
    fetchMetrics()
    const interval = setInterval(fetchMetrics, 3000)
    return () => clearInterval(interval)
  }, [fetchMetrics])

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-gray-500 mt-1">正在获取监控数据</div>
        </div>
      </div>
    )
  }

  if (error || !metrics) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium text-red-600">
            {error || "暂无数据"}
          </div>
          <div className="text-sm text-gray-500 mt-1">
            请检查后端服务是否正常运行
          </div>
          <button
            onClick={fetchMetrics}
            className="mt-4 px-4 py-2 bg-primary text-white rounded-md hover:bg-primary/90 transition-colors"
          >
            重试
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>基础指标</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-sm font-medium text-gray-500">运行时间</div>
                <div className="text-lg font-semibold">{metrics.uptime}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">当前活跃请求</div>
                <div className="text-lg font-semibold">{metrics.active_requests}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">总传输数据</div>
                <div className="text-lg font-semibold">{formatBytes(metrics.total_bytes)}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">每秒传输数据</div>
                <div className="text-lg font-semibold">{formatBytes(metrics.bytes_per_second)}/s</div>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>系统指标</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-sm font-medium text-gray-500">Goroutine数量</div>
                <div className="text-lg font-semibold">{metrics.num_goroutine}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">内存使用</div>
                <div className="text-lg font-semibold">{metrics.memory_usage}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">平均响应时间</div>
                <div className="text-lg font-semibold">{metrics.avg_response_time}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">平均每秒请求数</div>
                <div className="text-lg font-semibold">
                  {metrics.requests_per_second.toFixed(2)}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>状态码统计</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
            {Object.entries(metrics.status_code_stats || {})
              .sort((a, b) => a[0].localeCompare(b[0]))
              .map(([status, count]) => {
                const statusNum = parseInt(status);
                let colorClass = "text-green-600";
                if (statusNum >= 500) {
                  colorClass = "text-red-600";
                } else if (statusNum >= 400) {
                  colorClass = "text-yellow-600";
                } else if (statusNum >= 300) {
                  colorClass = "text-blue-600";
                }

                // 计算总请求数
                const totalRequests = Object.values(metrics.status_code_stats || {}).reduce((a, b) => a + (b as number), 0);

                return (
                  <div
                    key={status}
                    className="p-4 rounded-lg border bg-card text-card-foreground shadow-sm"
                  >
                    <div className="text-sm font-medium text-gray-500">
                      状态码 {status}
                    </div>
                    <div className={`text-lg font-semibold ${colorClass}`}>{count}</div>
                    <div className="text-sm text-gray-500 mt-1">
                      {totalRequests ?
                        ((count as number / totalRequests) * 100).toFixed(1) : 0}%
                    </div>
                  </div>
                );
              })}
          </div>
        </CardContent>
      </Card>


      {/* 新增：延迟统计卡片 */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>延迟统计</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <div className="text-sm font-medium text-gray-500">最小响应时间</div>
                  <div className="text-lg font-semibold">{metrics.latency_stats?.min || "0ms"}</div>
                </div>
                <div>
                  <div className="text-sm font-medium text-gray-500">最大响应时间</div>
                  <div className="text-lg font-semibold">{metrics.latency_stats?.max || "0ms"}</div>
                </div>
              </div>

              <div>
                <div className="text-sm font-medium text-gray-500 mb-2">响应时间分布</div>
                <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
                  {metrics.latency_stats?.distribution &&
                    Object.entries(metrics.latency_stats.distribution)
                      .sort((a, b) => {
                        // 按照延迟范围排序
                        const order = ["lt10ms", "10-50ms", "50-200ms", "200-1000ms", "gt1s"];
                        return order.indexOf(a[0]) - order.indexOf(b[0]);
                      })
                      .map(([range, count]) => {
                        // 转换桶键为更友好的显示
                        let displayRange = range;
                        if (range === "lt10ms") displayRange = "<10ms";
                        if (range === "gt1s") displayRange = ">1s";
                        if (range === "200-1000ms") displayRange = "0.2-1s";
                        
                        return (
                          <div key={range} className="p-3 rounded-lg border bg-card text-card-foreground shadow-sm">
                            <div className="text-sm font-medium text-gray-500">{displayRange}</div>
                            <div className="text-lg font-semibold">{count}</div>
                            <div className="text-xs text-gray-500 mt-1">
                              {Object.values(metrics.latency_stats?.distribution || {}).reduce((sum, val) => sum + val, 0) > 0
                                ? ((count / Object.values(metrics.latency_stats?.distribution || {}).reduce((sum, val) => sum + val, 0)) * 100).toFixed(1)
                                : 0}%
                            </div>
                          </div>
                        );
                      })}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>带宽统计</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div>
                <div className="text-sm font-medium text-gray-500">当前带宽</div>
                <div className="text-lg font-semibold">{metrics.current_bandwidth || "0 B/s"}</div>
              </div>

              <div>
                <div className="text-sm font-medium text-gray-500 mb-2">带宽历史</div>
                <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                  {metrics.bandwidth_history &&
                    Object.entries(metrics.bandwidth_history)
                      .sort((a, b) => a[0].localeCompare(b[0]))
                      .map(([time, bandwidth]) => (
                        <div key={time} className="p-3 rounded-lg border bg-card text-card-foreground shadow-sm">
                          <div className="text-sm font-medium text-gray-500">{time}</div>
                          <div className="text-lg font-semibold">{bandwidth}</div>
                        </div>
                      ))
                  }
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 引用来源统计卡片 */}
      {metrics.top_referers && metrics.top_referers.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>引用来源统计 (Top {metrics.top_referers.length})</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b">
                    <th className="text-left p-2">来源域名</th>
                    <th className="text-left p-2">请求数</th>
                    <th className="text-left p-2">错误数</th>
                    <th className="text-left p-2">平均延迟</th>
                    <th className="text-left p-2">传输大小</th>
                  </tr>
                </thead>
                <tbody>
                  {metrics.top_referers.map((referer, index) => (
                    <tr key={index} className="border-b">
                      <td className="p-2 max-w-xs truncate">
                        <span className="text-blue-600">
                          {referer.path}
                        </span>
                      </td>
                      <td className="p-2">{referer.request_count}</td>
                      <td className="p-2">{referer.error_count}</td>
                      <td className="p-2">{referer.avg_latency}</td>
                      <td className="p-2">{formatBytes(referer.bytes_transferred)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>最近请求</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-2">时间</th>
                  <th className="text-left p-2">路径</th>
                  <th className="text-left p-2">状态</th>
                  <th className="text-left p-2">延迟</th>
                  <th className="text-left p-2">大小</th>
                  <th className="text-left p-2">客户端IP</th>
                </tr>
              </thead>
              <tbody>
                {(metrics.recent_requests || [])
                  .slice(0, 20)  // 只显示最近20条记录
                  .map((req, index) => (
                    <tr key={index} className="border-b hover:bg-gray-50">
                      <td className="p-2">{formatDate(req.Time)}</td>
                      <td className="p-2 max-w-xs truncate">
                        <a
                          href={req.Path}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-blue-600 hover:text-blue-800 hover:underline"
                        >
                          {req.Path}
                        </a>
                      </td>
                      <td className="p-2">
                        <span
                          className={`px-2 py-1 rounded-full text-xs ${getStatusColor(
                            req.Status
                          )}`}
                        >
                          {req.Status}
                        </span>
                      </td>
                      <td className="p-2">{formatLatency(req.Latency)}</td>
                      <td className="p-2">{formatBytes(req.BytesSent)}</td>
                      <td className="p-2">
                        <Link href={`https://ipinfo.io/${req.ClientIP}`} target="_blank" rel="noopener noreferrer">
                          {req.ClientIP}
                        </Link>
                        </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

    </div>
  )
}

function formatBytes(bytes: number) {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB"]
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i]
}

function formatDate(dateStr: string) {
  if (!dateStr) return "-"
  const date = new Date(dateStr)
  return date.toLocaleString()
}

function formatLatency(nanoseconds: number) {
  if (!nanoseconds || isNaN(nanoseconds)) return "-"
  if (nanoseconds < 1000) {
    return nanoseconds + " ns"
  } else if (nanoseconds < 1000000) {
    return (nanoseconds / 1000).toFixed(2) + " µs"
  } else if (nanoseconds < 1000000000) {
    return (nanoseconds / 1000000).toFixed(2) + " ms"
  } else {
    return (nanoseconds / 1000000000).toFixed(2) + " s"
  }
}

function getStatusColor(status: number) {
  if (status >= 500) return "bg-red-100 text-red-800"
  if (status >= 400) return "bg-yellow-100 text-yellow-800"
  if (status >= 300) return "bg-blue-100 text-blue-800"
  return "bg-green-100 text-green-800"
} 