"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { TimeInput } from "@/components/ui/time-input"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { useRouter } from "next/navigation"
import {
  HardDrive,
  Database,
  RefreshCw,
  Trash2,
  Settings,
  ListX,
} from "lucide-react"

// 配置页只关心缓存启用状态 (统计已挪到首页"缓存"tab), 故 CacheStats 只保留 enabled
interface CacheStats {
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
}

interface CacheConfigs {
  proxy: CacheConfig
  mirror: CacheConfig
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

    // 定时刷新: 缓存统计非实时数据, 5s 一次足够, 避免高频轮询空转
    const interval = setInterval(fetchStats, 5000)
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

        <div className="grid gap-6 md:grid-cols-2">
          <CacheTypeCard
            title="代理缓存"
            icon={<HardDrive className="h-5 w-5 text-muted-foreground" />}
            enabled={stats?.proxy.enabled ?? false}
            onToggle={(checked) => handleToggleCache("proxy", checked)}
            onClear={() => handleClearCache("proxy")}
            renderConfig={() => renderCacheConfig("proxy")}
          />
          <CacheTypeCard
            title="镜像缓存"
            icon={<Database className="h-5 w-5 text-muted-foreground" />}
            enabled={stats?.mirror.enabled ?? false}
            onToggle={(checked) => handleToggleCache("mirror", checked)}
            onClear={() => handleClearCache("mirror")}
            renderConfig={() => renderCacheConfig("mirror")}
          />
        </div>
      </div>
  )
}

// CacheTypeCard 单个缓存类型 (proxy / mirror) 的操作 + 配置卡 (统计已挪到首页"缓存"tab)
function CacheTypeCard({
  title,
  icon,
  enabled,
  onToggle,
  onClear,
  renderConfig,
}: {
  title: string
  icon: React.ReactNode
  enabled: boolean
  onToggle: (checked: boolean) => void
  onClear: () => void
  renderConfig: () => React.ReactNode
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
            checked={enabled}
            onCheckedChange={onToggle}
          />
          <Button variant="outline" size="sm" onClick={onClear}>
            <Trash2 className="h-3 w-3 mr-1" />
            清理
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {renderConfig()}
      </CardContent>
    </Card>
  )
}
