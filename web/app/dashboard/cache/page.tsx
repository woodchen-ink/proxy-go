"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useRouter } from "next/navigation"
import { 
  HardDrive, 
  Database, 
  TrendingUp, 
  TrendingDown, 
  Activity, 
  Image as ImageIcon, 
  FileText, 
  RefreshCw,
  Trash2,
  Settings,
  Info,
  Zap,
  Target,
  RotateCcw
} from "lucide-react"

interface CacheStats {
  total_items: number
  total_size: number
  hit_count: number
  miss_count: number
  hit_rate: number
  bytes_saved: number
  enabled: boolean
  format_fallback_hit: number
  image_cache_hit: number
  regular_cache_hit: number
}

interface CacheConfig {
  max_age: number
  cleanup_tick: number
  max_cache_size: number
}

interface CacheData {
  proxy: CacheStats
  mirror: CacheStats
}

interface CacheConfigs {
  proxy: CacheConfig
  mirror: CacheConfig
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
    const interval = setInterval(fetchStats, 1000)
    return () => clearInterval(interval)
  }, [fetchStats, fetchConfigs])

  const handleToggleCache = async (type: "proxy" | "mirror", enabled: boolean) => {
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

  const handleUpdateConfig = async (type: "proxy" | "mirror", config: CacheConfig) => {
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

  const handleClearCache = async (type: "proxy" | "mirror" | "all") => {
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

  const renderCacheConfig = (type: "proxy" | "mirror" ) => {
    if (!configs) return null

    const config = configs[type]
    return (
      <div className="space-y-4 mt-4 p-4 bg-gray-50 rounded-lg border">
        <div className="flex items-center gap-2">
          <Settings className="h-4 w-4 text-gray-600" />
          <h3 className="text-sm font-medium text-gray-800">缓存配置</h3>
        </div>
        <div className="grid gap-4">
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-max-age`} className="text-sm">最大缓存时间（分钟）</Label>
            <Input
              id={`${type}-max-age`}
              type="number"
              value={config.max_age}
              className="h-8"
              onChange={(e) => {
                const newConfigs = { ...configs }
                newConfigs[type].max_age = parseInt(e.target.value)
                setConfigs(newConfigs)
              }}
              onBlur={() => handleUpdateConfig(type, config)}
            />
          </div>
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-cleanup-tick`} className="text-sm">清理间隔（分钟）</Label>
            <Input
              id={`${type}-cleanup-tick`}
              type="number"
              value={config.cleanup_tick}
              className="h-8"
              onChange={(e) => {
                const newConfigs = { ...configs }
                newConfigs[type].cleanup_tick = parseInt(e.target.value)
                setConfigs(newConfigs)
              }}
              onBlur={() => handleUpdateConfig(type, config)}
            />
          </div>
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-max-cache-size`} className="text-sm">最大缓存大小（GB）</Label>
            <Input
              id={`${type}-max-cache-size`}
              type="number"
              value={config.max_cache_size}
              className="h-8"
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
          <RefreshCw className="h-8 w-8 animate-spin mx-auto mb-4 text-blue-500" />
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-gray-500 mt-1">正在获取缓存统计信息</div>
        </div>
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className="space-y-6">
        <div className="flex justify-between items-center">
          <div className="flex items-center gap-2">
            <Database className="h-6 w-6 text-blue-600" />
            <h1 className="text-2xl font-bold">缓存管理</h1>
          </div>
          <Button 
            variant="outline" 
            onClick={() => handleClearCache("all")}
            className="flex items-center gap-2"
          >
            <Trash2 className="h-4 w-4" />
            清理所有缓存
          </Button>
        </div>

        {/* 智能缓存汇总 */}
        <Card className="border-2 border-blue-100 bg-gradient-to-r from-blue-50 to-purple-50">
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-blue-800">
              <Zap className="h-5 w-5" />
              智能缓存汇总
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="text-center p-4 bg-white rounded-lg shadow-sm border cursor-help hover:shadow-md transition-shadow">
                    <div className="flex items-center justify-center gap-2 mb-2">
                      <FileText className="h-5 w-5 text-blue-600" />
                      <Info className="h-3 w-3 text-gray-400" />
                    </div>
                    <div className="text-2xl font-bold text-blue-600">
                      {(stats?.proxy.regular_cache_hit ?? 0) + (stats?.mirror.regular_cache_hit ?? 0)}
                    </div>
                    <div className="text-sm text-gray-600 font-medium">常规缓存命中</div>
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <p>所有常规文件的精确缓存命中总数</p>
                </TooltipContent>
              </Tooltip>

              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="text-center p-4 bg-white rounded-lg shadow-sm border cursor-help hover:shadow-md transition-shadow">
                    <div className="flex items-center justify-center gap-2 mb-2">
                      <ImageIcon className="h-5 w-5 text-green-600" aria-hidden="true" />
                      <Info className="h-3 w-3 text-gray-400" />
                    </div>
                    <div className="text-2xl font-bold text-green-600">
                      {(stats?.proxy.image_cache_hit ?? 0) + (stats?.mirror.image_cache_hit ?? 0)}
                    </div>
                    <div className="text-sm text-gray-600 font-medium">图片精确命中</div>
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <p>所有图片文件的精确格式缓存命中总数</p>
                </TooltipContent>
              </Tooltip>

              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="text-center p-4 bg-white rounded-lg shadow-sm border cursor-help hover:shadow-md transition-shadow">
                    <div className="flex items-center justify-center gap-2 mb-2">
                      <RotateCcw className="h-5 w-5 text-orange-600" />
                      <Info className="h-3 w-3 text-gray-400" />
                    </div>
                    <div className="text-2xl font-bold text-orange-600">
                      {(stats?.proxy.format_fallback_hit ?? 0) + (stats?.mirror.format_fallback_hit ?? 0)}
                    </div>
                    <div className="text-sm text-gray-600 font-medium">格式回退命中</div>
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <p>图片格式回退命中总数，提高了缓存效率</p>
                </TooltipContent>
              </Tooltip>

              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="text-center p-4 bg-white rounded-lg shadow-sm border cursor-help hover:shadow-md transition-shadow">
                    <div className="flex items-center justify-center gap-2 mb-2">
                      <Target className="h-5 w-5 text-purple-600" />
                      <Info className="h-3 w-3 text-gray-400" />
                    </div>
                    <div className="text-2xl font-bold text-purple-600">
                      {(() => {
                        const totalImageRequests = (stats?.proxy.image_cache_hit ?? 0) + (stats?.mirror.image_cache_hit ?? 0) + (stats?.proxy.format_fallback_hit ?? 0) + (stats?.mirror.format_fallback_hit ?? 0)
                        const fallbackHits = (stats?.proxy.format_fallback_hit ?? 0) + (stats?.mirror.format_fallback_hit ?? 0)
                        return totalImageRequests > 0 ? ((fallbackHits / totalImageRequests) * 100).toFixed(1) : '0.0'
                      })()}%
                    </div>
                    <div className="text-sm text-gray-600 font-medium">格式回退率</div>
                  </div>
                </TooltipTrigger>
                <TooltipContent>
                  <p>格式回退在所有图片请求中的占比，显示智能缓存的效果</p>
                </TooltipContent>
              </Tooltip>
            </div>
          </CardContent>
        </Card>

        <div className="grid gap-6 md:grid-cols-2">
          {/* 代理缓存 */}
          <Card className="border-l-4 border-l-blue-500">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="flex items-center gap-2">
                <HardDrive className="h-5 w-5 text-blue-600" />
                代理缓存
              </CardTitle>
              <div className="flex items-center space-x-2">
                <Switch
                  checked={stats?.proxy.enabled ?? false}
                  onCheckedChange={(checked) => handleToggleCache("proxy", checked)}
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleClearCache("proxy")}
                  className="flex items-center gap-1"
                >
                  <Trash2 className="h-3 w-3" />
                  清理
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="space-y-3">
                <div className="flex justify-between items-center p-2 bg-gray-50 rounded">
                  <dt className="text-sm font-medium text-gray-600 flex items-center gap-2">
                    <Database className="h-4 w-4" />
                    缓存项数量
                  </dt>
                  <dd className="text-sm font-semibold text-gray-900">{stats?.proxy.total_items ?? 0}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-gray-50 rounded">
                  <dt className="text-sm font-medium text-gray-600 flex items-center gap-2">
                    <HardDrive className="h-4 w-4" />
                    总大小
                  </dt>
                  <dd className="text-sm font-semibold text-gray-900">{formatBytes(stats?.proxy.total_size ?? 0)}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-green-50 rounded">
                  <dt className="text-sm font-medium text-green-700 flex items-center gap-2">
                    <TrendingUp className="h-4 w-4" />
                    命中次数
                  </dt>
                  <dd className="text-sm font-semibold text-green-800">{stats?.proxy.hit_count ?? 0}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-red-50 rounded">
                  <dt className="text-sm font-medium text-red-700 flex items-center gap-2">
                    <TrendingDown className="h-4 w-4" />
                    未命中次数
                  </dt>
                  <dd className="text-sm font-semibold text-red-800">{stats?.proxy.miss_count ?? 0}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-blue-50 rounded">
                  <dt className="text-sm font-medium text-blue-700 flex items-center gap-2">
                    <Activity className="h-4 w-4" />
                    命中率
                  </dt>
                  <dd className="text-sm font-semibold text-blue-800">{(stats?.proxy.hit_rate ?? 0).toFixed(2)}%</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-purple-50 rounded">
                  <dt className="text-sm font-medium text-purple-700 flex items-center gap-2">
                    <Zap className="h-4 w-4" />
                    节省带宽
                  </dt>
                  <dd className="text-sm font-semibold text-purple-800">{formatBytes(stats?.proxy.bytes_saved ?? 0)}</dd>
                </div>
              </dl>
              
              <div className="border-t pt-4 mt-4">
                <div className="flex items-center gap-2 mb-3">
                  <Zap className="h-4 w-4 text-gray-600" />
                  <div className="text-sm font-medium text-gray-800">智能缓存统计</div>
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="text-center p-3 bg-blue-50 rounded-lg border cursor-help hover:bg-blue-100 transition-colors">
                        <FileText className="h-4 w-4 mx-auto mb-1 text-blue-600" />
                        <div className="text-lg font-bold text-blue-600">{stats?.proxy.regular_cache_hit ?? 0}</div>
                        <div className="text-xs text-blue-700">常规命中</div>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>常规文件的精确缓存命中</p>
                    </TooltipContent>
                  </Tooltip>
                  
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="text-center p-3 bg-green-50 rounded-lg border cursor-help hover:bg-green-100 transition-colors">
                        <ImageIcon className="h-4 w-4 mx-auto mb-1 text-green-600" aria-hidden="true" />
                        <div className="text-lg font-bold text-green-600">{stats?.proxy.image_cache_hit ?? 0}</div>
                        <div className="text-xs text-green-700">图片命中</div>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>图片文件的精确格式缓存命中</p>
                    </TooltipContent>
                  </Tooltip>
                  
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="text-center p-3 bg-orange-50 rounded-lg border cursor-help hover:bg-orange-100 transition-colors">
                        <RotateCcw className="h-4 w-4 mx-auto mb-1 text-orange-600" />
                        <div className="text-lg font-bold text-orange-600">{stats?.proxy.format_fallback_hit ?? 0}</div>
                        <div className="text-xs text-orange-700">格式回退</div>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>图片格式回退命中（如请求WebP但提供JPEG）</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
              {renderCacheConfig("proxy")}
            </CardContent>
          </Card>

          {/* 镜像缓存 */}
          <Card className="border-l-4 border-l-green-500">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="flex items-center gap-2">
                <Database className="h-5 w-5 text-green-600" />
                镜像缓存
              </CardTitle>
              <div className="flex items-center space-x-2">
                <Switch
                  checked={stats?.mirror.enabled ?? false}
                  onCheckedChange={(checked) => handleToggleCache("mirror", checked)}
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleClearCache("mirror")}
                  className="flex items-center gap-1"
                >
                  <Trash2 className="h-3 w-3" />
                  清理
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              <dl className="space-y-3">
                <div className="flex justify-between items-center p-2 bg-gray-50 rounded">
                  <dt className="text-sm font-medium text-gray-600 flex items-center gap-2">
                    <Database className="h-4 w-4" />
                    缓存项数量
                  </dt>
                  <dd className="text-sm font-semibold text-gray-900">{stats?.mirror.total_items ?? 0}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-gray-50 rounded">
                  <dt className="text-sm font-medium text-gray-600 flex items-center gap-2">
                    <HardDrive className="h-4 w-4" />
                    总大小
                  </dt>
                  <dd className="text-sm font-semibold text-gray-900">{formatBytes(stats?.mirror.total_size ?? 0)}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-green-50 rounded">
                  <dt className="text-sm font-medium text-green-700 flex items-center gap-2">
                    <TrendingUp className="h-4 w-4" />
                    命中次数
                  </dt>
                  <dd className="text-sm font-semibold text-green-800">{stats?.mirror.hit_count ?? 0}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-red-50 rounded">
                  <dt className="text-sm font-medium text-red-700 flex items-center gap-2">
                    <TrendingDown className="h-4 w-4" />
                    未命中次数
                  </dt>
                  <dd className="text-sm font-semibold text-red-800">{stats?.mirror.miss_count ?? 0}</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-blue-50 rounded">
                  <dt className="text-sm font-medium text-blue-700 flex items-center gap-2">
                    <Activity className="h-4 w-4" />
                    命中率
                  </dt>
                  <dd className="text-sm font-semibold text-blue-800">{(stats?.mirror.hit_rate ?? 0).toFixed(2)}%</dd>
                </div>
                <div className="flex justify-between items-center p-2 bg-purple-50 rounded">
                  <dt className="text-sm font-medium text-purple-700 flex items-center gap-2">
                    <Zap className="h-4 w-4" />
                    节省带宽
                  </dt>
                  <dd className="text-sm font-semibold text-purple-800">{formatBytes(stats?.mirror.bytes_saved ?? 0)}</dd>
                </div>
              </dl>
              
              <div className="border-t pt-4 mt-4">
                <div className="flex items-center gap-2 mb-3">
                  <Zap className="h-4 w-4 text-gray-600" />
                  <div className="text-sm font-medium text-gray-800">智能缓存统计</div>
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="text-center p-3 bg-blue-50 rounded-lg border cursor-help hover:bg-blue-100 transition-colors">
                        <FileText className="h-4 w-4 mx-auto mb-1 text-blue-600" />
                        <div className="text-lg font-bold text-blue-600">{stats?.mirror.regular_cache_hit ?? 0}</div>
                        <div className="text-xs text-blue-700">常规命中</div>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>常规文件的精确缓存命中</p>
                    </TooltipContent>
                  </Tooltip>
                  
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="text-center p-3 bg-green-50 rounded-lg border cursor-help hover:bg-green-100 transition-colors">
                        <ImageIcon className="h-4 w-4 mx-auto mb-1 text-green-600" aria-hidden="true" />
                        <div className="text-lg font-bold text-green-600">{stats?.mirror.image_cache_hit ?? 0}</div>
                        <div className="text-xs text-green-700">图片命中</div>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>图片文件的精确格式缓存命中</p>
                    </TooltipContent>
                  </Tooltip>
                  
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div className="text-center p-3 bg-orange-50 rounded-lg border cursor-help hover:bg-orange-100 transition-colors">
                        <RotateCcw className="h-4 w-4 mx-auto mb-1 text-orange-600" />
                        <div className="text-lg font-bold text-orange-600">{stats?.mirror.format_fallback_hit ?? 0}</div>
                        <div className="text-xs text-orange-700">格式回退</div>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>图片格式回退命中（如请求WebP但提供JPEG）</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>
              {renderCacheConfig("mirror")}
            </CardContent>
          </Card>
        </div>
      </div>
    </TooltipProvider>
  )
} 