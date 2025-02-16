"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"

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
  status_code_stats: Record<string, number>
  top_paths: Array<{
    path: string
    request_count: number
    error_count: number
    avg_latency: string
    bytes_transferred: number
  }>
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
  error_stats: {
    client_errors: number
    server_errors: number
    types: Record<string, number>
  }
  bandwidth_history: Record<string, string>
  current_bandwidth: string
  total_bytes: number
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
    const interval = setInterval(fetchMetrics, 5000)
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
                <div className="text-sm font-medium text-gray-500">总请求数</div>
                <div className="text-lg font-semibold">{metrics.total_requests}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-gray-500">错误数</div>
                <div className="text-lg font-semibold">{metrics.total_errors}</div>
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
                <div className="text-sm font-medium text-gray-500">每秒请求数</div>
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
              .map(([status, count]) => (
                <div
                  key={status}
                  className="p-4 rounded-lg border bg-card text-card-foreground shadow-sm"
                >
                  <div className="text-sm font-medium text-gray-500">
                    状态码 {status}
                  </div>
                  <div className="text-lg font-semibold">{count}</div>
                </div>
              ))}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>热门路径 (Top 10)</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-2">路径</th>
                  <th className="text-left p-2">请求数</th>
                  <th className="text-left p-2">错误数</th>
                  <th className="text-left p-2">平均延迟</th>
                  <th className="text-left p-2">传输大小</th>
                </tr>
              </thead>
              <tbody>
                {(metrics.top_paths || []).map((path, index) => (
                  <tr key={index} className="border-b">
                    <td className="p-2">
                      <a
                        href={path.path}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-blue-600 hover:text-blue-800 hover:underline"
                      >
                        {path.path}
                      </a>
                    </td>
                    <td className="p-2">{path.request_count}</td>
                    <td className="p-2">{path.error_count}</td>
                    <td className="p-2">{path.avg_latency}</td>
                    <td className="p-2">{formatBytes(path.bytes_transferred)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

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
                    <td className="p-2">{req.ClientIP}</td>
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