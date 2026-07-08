"use client"

import { useEffect, useState, useCallback } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { fetchApi } from "@/lib/api-client"
import {
  HardDrive,
  Database,
  TrendingUp,
  TrendingDown,
  Activity,
  Image as ImageIcon,
  FileText,
  Info,
  Zap,
  RotateCcw,
  Layers,
  HardDriveDownload,
} from "lucide-react"

// 与后端 CacheStats (internal/cache/manager.go) 对齐
interface CacheStats {
  total_items: number
  total_size: number
  disk_file_count: number
  disk_size: number
  hit_count: number
  miss_count: number
  hit_rate: number
  bytes_saved: number
  enabled: boolean
  format_fallback_hit: number
  image_cache_hit: number
  regular_cache_hit: number
}

interface CacheData {
  proxy: CacheStats
  mirror: CacheStats
}

// formatBytes 字节数 → 友好单位
function formatBytes(bytes: number) {
  const units = ["B", "KB", "MB", "GB"]
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(2)} ${units[unitIndex]}`
}

// sum 取 proxy + mirror 两侧同一字段之和
function sum(stats: CacheData | null, key: keyof CacheStats): number {
  return (Number(stats?.proxy[key]) || 0) + (Number(stats?.mirror[key]) || 0)
}

// CacheStatsTiles 首页"缓存"tab 的只读统计区: 规模概览 + 智能缓存汇总 + proxy/mirror 分项。
// 自带 5s 轮询拉取 /admin/api/cache/stats, 与首页 metrics 数据流互相独立。
export default function CacheStatsTiles() {
  const [stats, setStats] = useState<CacheData | null>(null)

  const fetchStats = useCallback(async () => {
    try {
      const response = await fetchApi("/admin/api/cache/stats")
      if (!response.ok) throw new Error("获取缓存统计失败")
      setStats(await response.json())
    } catch (error) {
      console.error("获取缓存统计失败:", error)
    }
  }, [])

  useEffect(() => {
    fetchStats()
    const interval = setInterval(fetchStats, 5000)
    return () => clearInterval(interval)
  }, [fetchStats])

  return (
    <TooltipProvider>
      <div className="space-y-6">
        {/* 缓存规模概览: 全部缓存 (proxy + mirror) 的条数与占用 */}
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-base">
              <HardDrive className="h-5 w-5 text-muted-foreground" />
              缓存规模
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <SummaryTile
                icon={<Layers className="h-5 w-5 text-muted-foreground" />}
                value={sum(stats, "total_items").toLocaleString()}
                label="缓存条数"
                tip="逻辑口径: 所有缓存键的总数 (同一文件被不同格式/浏览器分桶时算多条)"
              />
              <SummaryTile
                icon={<Database className="h-5 w-5 text-muted-foreground" />}
                value={formatBytes(sum(stats, "total_size"))}
                label="逻辑总量"
                tip="逻辑口径: 所有缓存项大小之和, 去重前的逻辑内容总量"
              />
              <SummaryTile
                icon={<FileText className="h-5 w-5 text-muted-foreground" />}
                value={sum(stats, "disk_file_count").toLocaleString()}
                label="磁盘文件数"
                tip="去重口径: 磁盘上的物理文件数 (相同内容的多个缓存键共享一个文件)"
              />
              <SummaryTile
                icon={<HardDriveDownload className="h-5 w-5 text-muted-foreground" />}
                value={formatBytes(sum(stats, "disk_size"))}
                label="磁盘占用"
                tip="去重口径: 磁盘实际占用字节数, 反映真实存储压力"
              />
            </div>
          </CardContent>
        </Card>

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
                value={sum(stats, "regular_cache_hit")}
                label="常规缓存命中"
                tip="所有常规文件的精确缓存命中总数"
              />
              <SummaryTile
                icon={<ImageIcon className="h-5 w-5 text-muted-foreground" aria-hidden="true" />}
                value={sum(stats, "image_cache_hit")}
                label="图片精确命中"
                tip="所有图片文件的精确格式缓存命中总数"
              />
              <SummaryTile
                icon={<RotateCcw className="h-5 w-5 text-muted-foreground" />}
                value={sum(stats, "format_fallback_hit")}
                label="格式回退命中"
                tip="图片格式回退命中总数, 提高了缓存效率"
              />
              <SummaryTile
                icon={<Activity className="h-5 w-5 text-muted-foreground" />}
                value={(() => {
                  const totalImageRequests = sum(stats, "image_cache_hit") + sum(stats, "format_fallback_hit")
                  const fallbackHits = sum(stats, "format_fallback_hit")
                  return totalImageRequests > 0 ? `${((fallbackHits / totalImageRequests) * 100).toFixed(1)}%` : "0.0%"
                })()}
                label="格式回退率"
                tip="格式回退在所有图片请求中的占比, 显示智能缓存的效果"
              />
            </div>
          </CardContent>
        </Card>

        {/* proxy / mirror 分项统计 */}
        <div className="grid gap-6 md:grid-cols-2">
          <CacheTypeStatsCard
            title="代理缓存"
            icon={<HardDrive className="h-5 w-5 text-muted-foreground" />}
            stats={stats?.proxy}
          />
          <CacheTypeStatsCard
            title="镜像缓存"
            icon={<Database className="h-5 w-5 text-muted-foreground" />}
            stats={stats?.mirror}
          />
        </div>
      </div>
    </TooltipProvider>
  )
}

// SummaryTile 顶部汇总小格子, 数值统一走前景色
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

// CacheTypeStatsCard 单个缓存类型 (proxy / mirror) 的只读统计卡 (无开关/清理/配置)
function CacheTypeStatsCard({
  title,
  icon,
  stats,
}: {
  title: string
  icon: React.ReactNode
  stats: CacheStats | undefined
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-base">
          {icon}
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="space-y-2">
          <StatRow icon={<Database className="h-4 w-4" />} label="缓存项数量">
            {(stats?.total_items ?? 0).toLocaleString()}
          </StatRow>
          <StatRow icon={<HardDrive className="h-4 w-4" />} label="总大小">
            {formatBytes(stats?.total_size ?? 0)}
          </StatRow>
          <StatRow icon={<HardDriveDownload className="h-4 w-4" />} label="磁盘占用">
            {formatBytes(stats?.disk_size ?? 0)}
          </StatRow>
          <StatRow
            icon={<TrendingUp className="h-4 w-4 text-success" />}
            label="命中次数"
            tone="success"
          >
            {(stats?.hit_count ?? 0).toLocaleString()}
          </StatRow>
          <StatRow
            icon={<TrendingDown className="h-4 w-4 text-destructive" />}
            label="未命中次数"
            tone="destructive"
          >
            {(stats?.miss_count ?? 0).toLocaleString()}
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
