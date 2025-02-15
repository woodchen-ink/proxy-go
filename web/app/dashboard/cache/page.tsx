"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { Switch } from "@/components/ui/switch"

interface CacheStats {
  total_items: number
  total_size: number
  hit_count: number
  miss_count: number
  hit_rate: number
  bytes_saved: number
  enabled: boolean
}

interface CacheData {
  proxy: CacheStats
  mirror: CacheStats
}

function formatBytes(bytes: number) {
  const units = ['B', 'KB', 'MB', 'GB']
  let size = bytes
  let unitIndex = 0
  
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  
  return `${size.toFixed(2)} ${units[unitIndex]}`
}

export default function CachePage() {
  const [stats, setStats] = useState<CacheData | null>(null)
  const [loading, setLoading] = useState(true)
  const { toast } = useToast()

  const fetchStats = useCallback(async () => {
    try {
      const response = await fetch("/admin/api/cache/stats")
      if (!response.ok) throw new Error("获取缓存统计失败")
      const data = await response.json()
      setStats(data)
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "获取缓存统计失败",
        variant: "destructive",
      })
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => {
    // 立即获取一次数据
    fetchStats()

    // 设置定时刷新
    const interval = setInterval(fetchStats, 5000)
    return () => clearInterval(interval)
  }, [fetchStats])

  const handleToggleCache = async (type: "proxy" | "mirror", enabled: boolean) => {
    try {
      const response = await fetch("/admin/api/cache/enable", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type, enabled }),
      })
      
      if (!response.ok) throw new Error("切换缓存状态失败")
      
      toast({
        title: "成功",
        description: `${type === "proxy" ? "代理" : "镜像"}缓存已${enabled ? "启用" : "禁用"}`,
      })
      
      fetchStats()
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "切换缓存状态失败",
        variant: "destructive",
      })
    }
  }

  const handleClearCache = async (type: "proxy" | "mirror" | "all") => {
    try {
      const response = await fetch("/admin/api/cache/clear", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type }),
      })
      
      if (!response.ok) throw new Error("清理缓存失败")
      
      toast({
        title: "成功",
        description: "缓存已清理",
      })
      
      fetchStats()
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "清理缓存失败",
        variant: "destructive",
      })
    }
  }

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-gray-500 mt-1">正在获取缓存统计信息</div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-end space-x-2">
        <Button variant="outline" onClick={() => handleClearCache("all")}>
          清理所有缓存
        </Button>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        {/* 代理缓存 */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle>代理缓存</CardTitle>
            <div className="flex items-center space-x-2">
              <Switch
                checked={stats?.proxy.enabled ?? false}
                onCheckedChange={(checked) => handleToggleCache("proxy", checked)}
              />
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleClearCache("proxy")}
              >
                清理
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">缓存项数量</dt>
                <dd className="text-sm text-gray-900">{stats?.proxy.total_items ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">总大小</dt>
                <dd className="text-sm text-gray-900">{formatBytes(stats?.proxy.total_size ?? 0)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">命中次数</dt>
                <dd className="text-sm text-gray-900">{stats?.proxy.hit_count ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">未命中次数</dt>
                <dd className="text-sm text-gray-900">{stats?.proxy.miss_count ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">命中率</dt>
                <dd className="text-sm text-gray-900">{(stats?.proxy.hit_rate ?? 0).toFixed(2)}%</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">节省带宽</dt>
                <dd className="text-sm text-gray-900">{formatBytes(stats?.proxy.bytes_saved ?? 0)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>

        {/* 镜像缓存 */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle>镜像缓存</CardTitle>
            <div className="flex items-center space-x-2">
              <Switch
                checked={stats?.mirror.enabled ?? false}
                onCheckedChange={(checked) => handleToggleCache("mirror", checked)}
              />
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleClearCache("mirror")}
              >
                清理
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">缓存项数量</dt>
                <dd className="text-sm text-gray-900">{stats?.mirror.total_items ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">总大小</dt>
                <dd className="text-sm text-gray-900">{formatBytes(stats?.mirror.total_size ?? 0)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">命中次数</dt>
                <dd className="text-sm text-gray-900">{stats?.mirror.hit_count ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">未命中次数</dt>
                <dd className="text-sm text-gray-900">{stats?.mirror.miss_count ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">命中率</dt>
                <dd className="text-sm text-gray-900">{(stats?.mirror.hit_rate ?? 0).toFixed(2)}%</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">节省带宽</dt>
                <dd className="text-sm text-gray-900">{formatBytes(stats?.mirror.bytes_saved ?? 0)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>
    </div>
  )
} 