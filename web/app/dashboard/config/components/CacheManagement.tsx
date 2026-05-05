"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { TimeInput } from "@/components/ui/time-input"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
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
  RotateCcw,
  ListX
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

export default function CacheManagement() {
  const [stats, setStats] = useState<CacheData | null>(null)
  const [configs, setConfigs] = useState<CacheConfigs | null>(null)
  const [loading, setLoading] = useState(true)
  const [urlListDialogOpen, setUrlListDialogOpen] = useState(false)
  const [urlListText, setUrlListText] = useState("")
  const [clearingUrls, setClearingUrls] = useState(false)
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
      console.error("获取缓存统计失败:", error)
    } finally {
      setLoading(false)
    }
  }, [router])

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
      console.error("获取缓存配置失败:", error)
    }
  }, [router])

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

  const handleClearCacheByURLs = async () => {
    try {
      // 解析 URL 列表（支持换行符和逗号分隔）
      const urls = urlListText
        .split(/[\n,]/)
        .map(url => url.trim())
        .filter(url => url.length > 0)

      if (urls.length === 0) {
        toast({
          title: "错误",
          description: "请输入至少一个 URL",
          variant: "destructive",
        })
        return
      }

      setClearingUrls(true)

      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/clear-by-urls", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          type: "all",
          urls: urls
        }),
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) {
        throw new Error("清理缓存失败")
      }

      const result = await response.json()

      toast({
        title: "清理成功",
        description: result.message || `已清理 ${result.cleared_items} 个缓存项`,
      })

      // 关闭对话框并清空输入
      setUrlListDialogOpen(false)
      setUrlListText("")

      // 刷新统计数据
      fetchStats()
    } catch (error) {
      toast({
        title: "清理失败",
        description: error instanceof Error ? error.message : "清理缓存失败",
        variant: "destructive",
      })
    } finally {
      setClearingUrls(false)
    }
  }

  const renderCacheConfig = (type: "proxy" | "mirror" ) => {
    if (!configs) return null

    const config = configs[type]
    return (
      <div className="space-y-4 mt-4 p-4 bg-muted rounded-lg border">
        <div className="flex items-center gap-2">
          <Settings className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-medium">缓存配置</h3>
        </div>
        <div className="grid gap-4">
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-max-age`} className="text-sm">最大缓存时间</Label>
            <TimeInput
              id={`${type}-max-age`}
              value={config.max_age}
              onChange={(minutes) => {
                const newConfigs = { ...configs }
                newConfigs[type].max_age = minutes
                setConfigs(newConfigs)
                handleUpdateConfig(type, { ...config, max_age: minutes })
              }}
              placeholder="30"
              min={0}
            />
          </div>
          <div className="grid grid-cols-2 items-center gap-4">
            <Label htmlFor={`${type}-cleanup-tick`} className="text-sm">清理间隔</Label>
            <TimeInput
              id={`${type}-cleanup-tick`}
              value={config.cleanup_tick}
              onChange={(minutes) => {
                const newConfigs = { ...configs }
                newConfigs[type].cleanup_tick = minutes
                setConfigs(newConfigs)
                handleUpdateConfig(type, { ...config, cleanup_tick: minutes })
              }}
              placeholder="5"
              min={0}
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
      <div className="flex h-[calc(100vh-16rem)] items-center justify-center">
        <div className="text-center">
          <RefreshCw className="h-8 w-8 animate-spin mx-auto mb-4 text-muted-foreground" />
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-muted-foreground mt-1">正在获取缓存统计信息</div>
        </div>
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className="space-y-6">
        <div className="flex justify-between items-center">
          <div className="flex items-center gap-2">
            <Database className="h-6 w-6 text-muted-foreground" />
            <h2 className="text-xl font-semibold tracking-tight">缓存管理</h2>
          </div>
          <div className="flex items-center gap-2">
            <Dialog open={urlListDialogOpen} onOpenChange={setUrlListDialogOpen}>
              <DialogTrigger asChild>
                <Button
                  variant="outline"
                  className="flex items-center gap-2"
                >
                  <ListX className="h-4 w-4" />
                  按 URL 清理
                </Button>
              </DialogTrigger>
              <DialogContent className="sm:max-w-[600px]">
                <DialogHeader>
                  <DialogTitle>按 URL 列表清理缓存</DialogTitle>
                  <DialogDescription>
                    输入需要清理的 URL 列表，每行一个 URL，或使用逗号分隔
                  </DialogDescription>
                </DialogHeader>
                <div className="space-y-4 py-4">
                  <div className="space-y-2">
                    <Label htmlFor="url-list">URL 列表</Label>
                    <Textarea
                      id="url-list"
                      placeholder="例如：&#10;/b2/img/photo.jpg&#10;/oracle/file.pdf&#10;/b2/video/demo.mp4"
                      value={urlListText}
                      onChange={(e) => setUrlListText(e.target.value)}
                      className="min-h-[200px] font-mono text-sm"
                    />
                    <p className="text-sm text-muted-foreground">
                      提示：支持换行符或逗号分隔，例如 <code className="text-xs bg-muted px-1 py-0.5 rounded">/b2/img/photo.jpg</code>
                    </p>
                  </div>
                </div>
                <DialogFooter>
                  <Button
                    variant="outline"
                    onClick={() => {
                      setUrlListDialogOpen(false)
                      setUrlListText("")
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    onClick={handleClearCacheByURLs}
                    disabled={clearingUrls || !urlListText.trim()}
                  >
                    {clearingUrls ? "清理中..." : "确认清理"}
                  </Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
            <Button
              variant="outline"
              onClick={() => handleClearCache("all")}
              className="flex items-center gap-2"
            >
              <Trash2 className="h-4 w-4" />
              清理所有缓存
            </Button>
          </div>
        </div>

        {/* 智能缓存汇总 */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <Zap className="h-5 w-5 text-muted-foreground" />
              智能缓存汇总
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <SummaryTile
                icon={<FileText className="h-5 w-5 text-muted-foreground" />}
                value={(stats?.proxy.regular_cache_hit ?? 0) + (stats?.mirror.regular_cache_hit ?? 0)}
                label="常规缓存命中"
                tip="所有常规文件的精确缓存命中总数"
              />
              <SummaryTile
                icon={<ImageIcon className="h-5 w-5 text-muted-foreground" aria-hidden="true" />}
                value={(stats?.proxy.image_cache_hit ?? 0) + (stats?.mirror.image_cache_hit ?? 0)}
                label="图片精确命中"
                tip="所有图片文件的精确格式缓存命中总数"
              />
              <SummaryTile
                icon={<RotateCcw className="h-5 w-5 text-muted-foreground" />}
                value={(stats?.proxy.format_fallback_hit ?? 0) + (stats?.mirror.format_fallback_hit ?? 0)}
                label="格式回退命中"
                tip="图片格式回退命中总数, 提高了缓存效率"
              />
              <SummaryTile
                icon={<Target className="h-5 w-5 text-muted-foreground" />}
                value={(() => {
                  const totalImageRequests = (stats?.proxy.image_cache_hit ?? 0) + (stats?.mirror.image_cache_hit ?? 0) + (stats?.proxy.format_fallback_hit ?? 0) + (stats?.mirror.format_fallback_hit ?? 0)
                  const fallbackHits = (stats?.proxy.format_fallback_hit ?? 0) + (stats?.mirror.format_fallback_hit ?? 0)
                  return totalImageRequests > 0 ? `${((fallbackHits / totalImageRequests) * 100).toFixed(1)}%` : "0.0%"
                })()}
                label="格式回退率"
                tip="格式回退在所有图片请求中的占比, 显示智能缓存的效果"
              />
            </div>
          </CardContent>
        </Card>

        <div className="grid gap-6 md:grid-cols-2">
          <CacheTypeCard
            title="代理缓存"
            icon={<HardDrive className="h-5 w-5 text-muted-foreground" />}
            stats={stats?.proxy}
            onToggle={(checked) => handleToggleCache("proxy", checked)}
            onClear={() => handleClearCache("proxy")}
            renderConfig={() => renderCacheConfig("proxy")}
            formatBytes={formatBytes}
          />
          <CacheTypeCard
            title="镜像缓存"
            icon={<Database className="h-5 w-5 text-muted-foreground" />}
            stats={stats?.mirror}
            onToggle={(checked) => handleToggleCache("mirror", checked)}
            onClear={() => handleClearCache("mirror")}
            renderConfig={() => renderCacheConfig("mirror")}
            formatBytes={formatBytes}
          />
        </div>
      </div>
    </TooltipProvider>
  )
}

// SummaryTile 顶部"智能缓存汇总"中的小格子, 数值统一走前景色
function SummaryTile({
  icon,
  value,
  label,
  tip,
}: {
  icon: React.ReactNode
  value: number | string
  label: string
  tip: string
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="text-center p-4 rounded-lg border bg-card cursor-help hover:bg-accent/40 transition-colors">
          <div className="flex items-center justify-center gap-2 mb-2">
            {icon}
            <Info className="h-3 w-3 text-muted-foreground" />
          </div>
          <div className="text-2xl font-semibold text-foreground">{value}</div>
          <div className="mt-0.5 text-sm text-muted-foreground">{label}</div>
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <p>{tip}</p>
      </TooltipContent>
    </Tooltip>
  )
}

// CacheTypeCard 单个缓存类型 (proxy / mirror) 的统计 + 操作 + 配置卡
function CacheTypeCard({
  title,
  icon,
  stats,
  onToggle,
  onClear,
  renderConfig,
  formatBytes,
}: {
  title: string
  icon: React.ReactNode
  stats: CacheStats | undefined
  onToggle: (checked: boolean) => void
  onClear: () => void
  renderConfig: () => React.ReactNode
  formatBytes: (bytes: number) => string
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="flex items-center gap-2 text-base">
          {icon}
          {title}
        </CardTitle>
        <div className="flex items-center space-x-2">
          <Switch
            checked={stats?.enabled ?? false}
            onCheckedChange={onToggle}
          />
          <Button variant="outline" size="sm" onClick={onClear}>
            <Trash2 className="h-3 w-3 mr-1" />
            清理
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <dl className="space-y-2">
          <StatRow icon={<Database className="h-4 w-4" />} label="缓存项数量">
            {stats?.total_items ?? 0}
          </StatRow>
          <StatRow icon={<HardDrive className="h-4 w-4" />} label="总大小">
            {formatBytes(stats?.total_size ?? 0)}
          </StatRow>
          <StatRow
            icon={<TrendingUp className="h-4 w-4 text-success" />}
            label="命中次数"
            tone="success"
          >
            {stats?.hit_count ?? 0}
          </StatRow>
          <StatRow
            icon={<TrendingDown className="h-4 w-4 text-destructive" />}
            label="未命中次数"
            tone="destructive"
          >
            {stats?.miss_count ?? 0}
          </StatRow>
          <StatRow icon={<Activity className="h-4 w-4" />} label="命中率">
            {(stats?.hit_rate ?? 0).toFixed(2)}%
          </StatRow>
          <StatRow icon={<Zap className="h-4 w-4" />} label="节省带宽">
            {formatBytes(stats?.bytes_saved ?? 0)}
          </StatRow>
        </dl>

        <div className="border-t pt-4 mt-4">
          <div className="flex items-center gap-2 mb-3">
            <Zap className="h-4 w-4 text-muted-foreground" />
            <div className="text-sm font-medium">智能缓存统计</div>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <DetailTile
              icon={<FileText className="h-4 w-4 mx-auto mb-1 text-muted-foreground" />}
              value={stats?.regular_cache_hit ?? 0}
              label="常规命中"
              tip="常规文件的精确缓存命中"
            />
            <DetailTile
              icon={<ImageIcon className="h-4 w-4 mx-auto mb-1 text-muted-foreground" aria-hidden="true" />}
              value={stats?.image_cache_hit ?? 0}
              label="图片命中"
              tip="图片文件的精确格式缓存命中"
            />
            <DetailTile
              icon={<RotateCcw className="h-4 w-4 mx-auto mb-1 text-muted-foreground" />}
              value={stats?.format_fallback_hit ?? 0}
              label="格式回退"
              tip="图片格式回退命中 (如请求 WebP 但提供 JPEG)"
            />
          </div>
        </div>
        {renderConfig()}
      </CardContent>
    </Card>
  )
}

// StatRow 缓存详情中的"标签 + 数值"行, 命中/未命中走 success/destructive 软底
function StatRow({
  icon,
  label,
  tone,
  children,
}: {
  icon: React.ReactNode
  label: string
  tone?: "success" | "destructive"
  children: React.ReactNode
}) {
  const toneClass =
    tone === "success"
      ? "bg-success/10"
      : tone === "destructive"
      ? "bg-destructive/10"
      : "bg-muted"
  const valueClass =
    tone === "success"
      ? "text-success"
      : tone === "destructive"
      ? "text-destructive"
      : "text-foreground"
  return (
    <div className={`flex justify-between items-center p-2 rounded ${toneClass}`}>
      <dt className="text-sm font-medium text-muted-foreground flex items-center gap-2">
        {icon}
        {label}
      </dt>
      <dd className={`text-sm font-semibold ${valueClass}`}>{children}</dd>
    </div>
  )
}

// DetailTile 智能缓存统计三联格子
function DetailTile({
  icon,
  value,
  label,
  tip,
}: {
  icon: React.ReactNode
  value: number | string
  label: string
  tip: string
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="text-center p-3 rounded-lg border bg-card cursor-help hover:bg-accent/40 transition-colors">
          {icon}
          <div className="text-lg font-semibold text-foreground">{value}</div>
          <div className="text-xs text-muted-foreground">{label}</div>
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <p>{tip}</p>
      </TooltipContent>
    </Tooltip>
  )
}
