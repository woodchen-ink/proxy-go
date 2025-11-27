"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"

interface TargetHealth {
  url: string
  is_healthy: boolean
  last_check: string
  last_success: string
  fail_count: number
  success_count: number
  total_requests: number
  failed_requests: number
  success_rate: number
  avg_latency: string
  last_error?: string
}

interface HealthSummary {
  total_targets: number
  healthy_targets: number
  unhealthy_targets: number
  overall_health: number
}

interface HealthStatus {
  targets: TargetHealth[]
  summary: HealthSummary
}

export default function HealthPage() {
  const [healthStatus, setHealthStatus] = useState<HealthStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [resetTarget, setResetTarget] = useState<string | null>(null)
  const [showClearDialog, setShowClearDialog] = useState(false)
  const { toast } = useToast()
  const router = useRouter()

  const fetchHealthStatus = useCallback(async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/health/status", {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) {
        throw new Error("加载健康状态失败")
      }

      const data = await response.json()
      setHealthStatus(data)
      setError(null)
    } catch (error) {
      const message = error instanceof Error ? error.message : "加载健康状态失败"
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

  const handleReset = async (url: string) => {
    try {
      const token = localStorage.getItem("token")
      const response = await fetch("/admin/api/health/reset", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ url }),
      })

      if (!response.ok) {
        throw new Error("重置失败")
      }

      toast({
        title: "成功",
        description: `已重置 ${url} 的健康状态`,
      })

      fetchHealthStatus()
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "重置失败",
        variant: "destructive",
      })
    } finally {
      setResetTarget(null)
    }
  }

  const handleClearAll = async () => {
    try {
      const token = localStorage.getItem("token")
      const response = await fetch("/admin/api/health/clear", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
      })

      if (!response.ok) {
        throw new Error("清理失败")
      }

      const data = await response.json()
      toast({
        title: "成功",
        description: `已清理 ${data.count} 条健康检查记录`,
      })

      fetchHealthStatus()
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "清理失败",
        variant: "destructive",
      })
    } finally {
      setShowClearDialog(false)
    }
  }

  useEffect(() => {
    fetchHealthStatus()
    const interval = setInterval(fetchHealthStatus, 5000) // 每5秒刷新一次
    return () => clearInterval(interval)
  }, [fetchHealthStatus])

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-gray-500 mt-1">正在获取健康状态</div>
        </div>
      </div>
    )
  }

  if (error || !healthStatus) {
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
            onClick={fetchHealthStatus}
            className="mt-4 px-4 py-2 bg-primary text-white rounded-md hover:bg-primary/90 transition-colors"
          >
            重试
          </button>
        </div>
      </div>
    )
  }

  const { targets, summary } = healthStatus

  return (
    <div className="space-y-6">
      {/* 健康摘要 */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
        <Card>
          <CardHeader>
            <CardTitle>总体健康度</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold" style={{
              color: summary.overall_health >= 90 ? '#518751' :
                     summary.overall_health >= 70 ? '#ecec70' : '#b85e48'
            }}>
              {summary.overall_health.toFixed(1)}%
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>总目标数</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold">{summary.total_targets}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>健康目标</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold" style={{ color: '#518751' }}>
              {summary.healthy_targets}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>不健康目标</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold" style={{ color: '#b85e48' }}>
              {summary.unhealthy_targets}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 目标列表 */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>目标健康状态</CardTitle>
          <div className="flex gap-2">
            <Button onClick={fetchHealthStatus} variant="outline" size="sm">
              刷新
            </Button>
            {targets.length > 0 && (
              <Button
                onClick={() => setShowClearDialog(true)}
                variant="outline"
                size="sm"
                style={{
                  backgroundColor: '#b85e48',
                  color: '#F8F7F6',
                  borderColor: '#b85e48'
                }}
                className="hover:opacity-90"
              >
                清理所有记录
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {targets.length === 0 ? (
            <div className="text-center text-gray-500 py-8">
              暂无目标服务器健康数据
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b" style={{ backgroundColor: '#EEEDEC' }}>
                    <th className="text-left p-3">状态</th>
                    <th className="text-left p-3">目标URL</th>
                    <th className="text-left p-3">成功率</th>
                    <th className="text-left p-3">平均延迟</th>
                    <th className="text-left p-3">请求数</th>
                    <th className="text-left p-3">失败次数</th>
                    <th className="text-left p-3">上次检查</th>
                    <th className="text-left p-3">上次成功</th>
                    <th className="text-left p-3">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {targets
                    .sort((a, b) => {
                      // 不健康的排在前面
                      if (a.is_healthy === b.is_healthy) {
                        return b.total_requests - a.total_requests
                      }
                      return a.is_healthy ? 1 : -1
                    })
                    .map((target, index) => (
                      <tr
                        key={index}
                        className="border-b hover:bg-gray-50 transition-colors"
                      >
                        <td className="p-3">
                          <div className="flex items-center gap-2">
                            <div
                              className="w-3 h-3 rounded-full"
                              style={{
                                backgroundColor: target.is_healthy
                                  ? '#518751'
                                  : '#b85e48',
                              }}
                            />
                            <span className="text-sm font-medium">
                              {target.is_healthy ? '健康' : '不健康'}
                            </span>
                          </div>
                        </td>
                        <td className="p-3">
                          <a
                            href={target.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-sm hover:underline max-w-md truncate block"
                            style={{ color: '#C08259' }}
                          >
                            {target.url}
                          </a>
                          {target.last_error && (
                            <div className="text-xs text-red-600 mt-1">
                              错误: {target.last_error}
                            </div>
                          )}
                        </td>
                        <td className="p-3">
                          <div className="flex flex-col">
                            <span
                              className="text-sm font-semibold"
                              style={{
                                color:
                                  target.success_rate >= 90
                                    ? '#518751'
                                    : target.success_rate >= 70
                                    ? '#ecec70'
                                    : '#b85e48',
                              }}
                            >
                              {target.success_rate.toFixed(1)}%
                            </span>
                            <span className="text-xs text-gray-500">
                              {target.success_count} / {target.fail_count}
                            </span>
                          </div>
                        </td>
                        <td className="p-3">
                          <span className="text-sm">{target.avg_latency}</span>
                        </td>
                        <td className="p-3">
                          <div className="flex flex-col">
                            <span className="text-sm font-semibold">
                              {target.total_requests}
                            </span>
                            <span className="text-xs text-gray-500">
                              失败: {target.failed_requests}
                            </span>
                          </div>
                        </td>
                        <td className="p-3">
                          <span
                            className="text-sm font-semibold"
                            style={{
                              color: target.fail_count > 0 ? '#b85e48' : '#518751',
                            }}
                          >
                            {target.fail_count}
                          </span>
                        </td>
                        <td className="p-3">
                          <span className="text-sm text-gray-600">
                            {target.last_check === 'N/A'
                              ? '从未检查'
                              : formatTime(target.last_check)}
                          </span>
                        </td>
                        <td className="p-3">
                          <span className="text-sm text-gray-600">
                            {target.last_success === 'N/A'
                              ? '从未成功'
                              : formatTime(target.last_success)}
                          </span>
                        </td>
                        <td className="p-3">
                          <Button
                            onClick={() => setResetTarget(target.url)}
                            variant="outline"
                            size="sm"
                          >
                            重置
                          </Button>
                        </td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 重置确认对话框 */}
      <AlertDialog open={!!resetTarget} onOpenChange={() => setResetTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认重置</AlertDialogTitle>
            <AlertDialogDescription>
              确定要重置 <span className="font-semibold">{resetTarget}</span> 的健康状态吗?
              <br />
              这将清除该目标的所有健康检查历史数据。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => resetTarget && handleReset(resetTarget)}
              style={{ backgroundColor: '#C08259', color: '#F8F7F6' }}
            >
              确认重置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* 清理所有记录确认对话框 */}
      <AlertDialog open={showClearDialog} onOpenChange={setShowClearDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认清理所有记录</AlertDialogTitle>
            <AlertDialogDescription>
              确定要清理所有健康检查记录吗？
              <br />
              这将删除 <span className="font-semibold">{targets.length}</span> 个目标的所有健康检查历史数据。
              <br />
              <span className="text-sm text-gray-500 mt-2 block">
                注意：健康的目标记录会在1小时未访问后自动清理，不健康的目标会保留以便跟踪问题。
              </span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleClearAll}
              style={{ backgroundColor: '#b85e48', color: '#F8F7F6' }}
            >
              确认清理
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* 说明卡片 */}
      <Card style={{ backgroundColor: '#F4E8E0' }}>
        <CardHeader>
          <CardTitle>健康检查说明</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <p>
            <strong>健康判定标准:</strong>
          </p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li>连续失败 <strong>3次</strong> 标记为不健康</li>
            <li>连续成功 <strong>2次</strong> 恢复为健康</li>
            <li>不健康超过 <strong>5分钟</strong> 后自动重试</li>
            <li>每 <strong>30秒</strong> 主动检查不健康的目标</li>
          </ul>
          <p className="mt-4">
            <strong>自动清理机制:</strong>
          </p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li>每 <strong>10分钟</strong> 自动清理一次过期记录</li>
            <li>超过 <strong>1小时</strong> 未访问的健康目标记录将被清理</li>
            <li>不健康的目标会保留以便跟踪问题</li>
            <li>支持手动清理所有记录</li>
          </ul>
          <p className="mt-4">
            <strong>成功率颜色:</strong>
          </p>
          <div className="flex gap-4 ml-2">
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded-full" style={{ backgroundColor: '#518751' }} />
              <span>≥90% (优秀)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded-full" style={{ backgroundColor: '#ecec70' }} />
              <span>70-89% (一般)</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-3 h-3 rounded-full" style={{ backgroundColor: '#b85e48' }} />
              <span>&lt;70% (差)</span>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function formatTime(timeStr: string) {
  if (timeStr === 'N/A') return '从未'
  try {
    const date = new Date(timeStr)
    const now = new Date()
    const diffInSeconds = Math.floor((now.getTime() - date.getTime()) / 1000)

    if (diffInSeconds < 60) return `${diffInSeconds}秒前`
    if (diffInSeconds < 3600) return `${Math.floor(diffInSeconds / 60)}分钟前`
    if (diffInSeconds < 86400) return `${Math.floor(diffInSeconds / 3600)}小时前`
    return date.toLocaleString()
  } catch {
    return timeStr
  }
}
