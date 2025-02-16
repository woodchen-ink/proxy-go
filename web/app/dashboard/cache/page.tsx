"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useRouter } from "next/navigation"

interface CacheStats {
  total_items: number
  total_size: number
  hit_count: number
  miss_count: number
  hit_rate: number
  bytes_saved: number
  enabled: boolean
}

interface CacheConfig {
  max_age: number
  cleanup_tick: number
  max_cache_size: number
}

interface CacheData {
  proxy: CacheStats
  mirror: CacheStats
  fixedPath: CacheStats
}

interface CacheConfigs {
  proxy: CacheConfig
  mirror: CacheConfig
  fixedPath: CacheConfig
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
  const [configs, setConfigs] = useState<CacheConfigs | null>(null)
  const [loading, setLoading] = useState(true)
  const { toast } = useToast()
  const router = useRouter()

  const fetchStats = useCallback(async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/stats", {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

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
  }, [toast, router])

  const fetchConfigs = useCallback(async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/config", {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) throw new Error("获取缓存配置失败")
      const data = await response.json()
      setConfigs(data)
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "获取缓存配置失败",
        variant: "destructive",
      })
    }
  }, [toast, router])

  useEffect(() => {
    // 立即获取一次数据
    fetchStats()
    fetchConfigs()

    // 设置定时刷新
    const interval = setInterval(fetchStats, 5000)
    return () => clearInterval(interval)
  }, [fetchStats, fetchConfigs])

  const handleToggleCache = async (type: "proxy" | "mirror" | "fixedPath", enabled: boolean) => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/enable", {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ type, enabled }),
      })
      
      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) throw new Error("切换缓存状态失败")
      
      toast({
        title: "成功",
        description: `${type === "proxy" ? "代理" : type === "mirror" ? "镜像" : "固定路径"}缓存已${enabled ? "启用" : "禁用"}`,
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

  const handleUpdateConfig = async (type: "proxy" | "mirror" | "fixedPath", config: CacheConfig) => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/config", {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ type, config }),
      })
      
      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) throw new Error("更新缓存配置失败")
      
      toast({
        title: "成功",
        description: "缓存配置已更新",
      })
      
      fetchConfigs()
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "更新缓存配置失败",
        variant: "destructive",
      })
    }
  }

  const handleClearCache = async (type: "proxy" | "mirror" | "fixedPath" | "all") => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/clear", {
        method: "POST",
        headers: { 
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({ type }),
      })
      
      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

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

  const renderCacheConfig = (type: "proxy" | "mirror" | "fixedPath") => {
    if (!configs) return null

    const config = configs[type]
    return (
      <div className="space-y-4 mt-4">
        <h3 className="text-sm font-medium">缓存配置</h3>
        <div className="grid gap-4">
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-max-age`}>最大缓存时间（分钟）</Label>
            <Input
              id={`${type}-max-age`}
              type="number"
              value={config.max_age}
              onChange={(e) => {
                const newConfigs = { ...configs }
                newConfigs[type].max_age = parseInt(e.target.value)
                setConfigs(newConfigs)
              }}
              onBlur={() => handleUpdateConfig(type, config)}
            />
          </div>
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-cleanup-tick`}>清理间隔（分钟）</Label>
            <Input
              id={`${type}-cleanup-tick`}
              type="number"
              value={config.cleanup_tick}
              onChange={(e) => {
                const newConfigs = { ...configs }
                newConfigs[type].cleanup_tick = parseInt(e.target.value)
                setConfigs(newConfigs)
              }}
              onBlur={() => handleUpdateConfig(type, config)}
            />
          </div>
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-max-cache-size`}>最大缓存大小（GB）</Label>
            <Input
              id={`${type}-max-cache-size`}
              type="number"
              value={config.max_cache_size}
              onChange={(e) => {
                const newConfigs = { ...configs }
                newConfigs[type].max_cache_size = parseInt(e.target.value)
                setConfigs(newConfigs)
              }}
              onBlur={() => handleUpdateConfig(type, config)}
            />
          </div>
        </div>
      </div>
    )
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
            {renderCacheConfig("proxy")}
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
            {renderCacheConfig("mirror")}
          </CardContent>
        </Card>

        {/* 固定路径缓存 */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle>固定路径缓存</CardTitle>
            <div className="flex items-center space-x-2">
              <Switch
                checked={stats?.fixedPath.enabled ?? false}
                onCheckedChange={(checked) => handleToggleCache("fixedPath", checked)}
              />
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleClearCache("fixedPath")}
              >
                清理
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">缓存项数量</dt>
                <dd className="text-sm text-gray-900">{stats?.fixedPath.total_items ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">总大小</dt>
                <dd className="text-sm text-gray-900">{formatBytes(stats?.fixedPath.total_size ?? 0)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">命中次数</dt>
                <dd className="text-sm text-gray-900">{stats?.fixedPath.hit_count ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">未命中次数</dt>
                <dd className="text-sm text-gray-900">{stats?.fixedPath.miss_count ?? 0}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">命中率</dt>
                <dd className="text-sm text-gray-900">{(stats?.fixedPath.hit_rate ?? 0).toFixed(2)}%</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm font-medium text-gray-500">节省带宽</dt>
                <dd className="text-sm text-gray-900">{formatBytes(stats?.fixedPath.bytes_saved ?? 0)}</dd>
              </div>
            </dl>
            {renderCacheConfig("fixedPath")}
          </CardContent>
        </Card>
      </div>
    </div>
  )
} 