import React, { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { RotateCcw } from "lucide-react"
import { toast } from "sonner"

interface PathStats {
  path: string
  request_count: number
  error_count: number
  bytes_transferred: number
  avg_latency: string
  last_access_time: number
  status_2xx: number
  status_3xx: number
  status_4xx: number
  status_5xx: number
  cache_hits: number
  cache_misses: number
  cache_hit_rate: number
  bytes_saved: number
}

interface PathStatsCardProps {
  stats: PathStats | undefined
  onReset?: () => void
}

// formatBytes 字节数 → 友好单位
function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + " " + sizes[i]
}

// formatNumber 千分位格式化
function formatNumber(num: number): string {
  return num.toLocaleString()
}

// rateBadgeClass 命中率/错误率分档 → 语义色 badge 的 className
//
// 三档语义: 高 success / 中 warning / 低 destructive, 不再用 brand 或 chart token
function rateBadgeClass(rate: number, kind: "hit" | "error") {
  if (kind === "hit") {
    if (rate > 70) return "bg-success/10 text-success border-transparent"
    if (rate > 40) return "bg-warning/10 text-warning border-transparent"
    return "bg-destructive/10 text-destructive border-transparent"
  }
  return rate > 5
    ? "bg-destructive/10 text-destructive border-transparent"
    : "bg-success/10 text-success border-transparent"
}

// statusBadgeClass HTTP 状态码区间 → 语义色 outline badge
function statusBadgeClass(group: "2xx" | "3xx" | "4xx" | "5xx") {
  switch (group) {
    case "2xx":
      return "bg-success/10 text-success border-success/30"
    case "3xx":
      return "bg-muted text-muted-foreground"
    case "4xx":
      return "bg-warning/10 text-warning border-warning/30"
    case "5xx":
      return "bg-destructive/10 text-destructive border-destructive/30"
  }
}

export default function PathStatsCard({ stats, onReset }: PathStatsCardProps) {
  const [isResetting, setIsResetting] = useState(false)

  if (!stats || stats.request_count === 0) {
    return <div className="text-sm text-muted-foreground">暂无统计数据</div>
  }

  const errorRate = stats.request_count > 0
    ? ((stats.error_count / stats.request_count) * 100)
    : 0
  const cacheHitRate = stats.cache_hit_rate * 100
  const totalCacheRequests = stats.cache_hits + stats.cache_misses

  // handleReset 触发外部重置回调, 完成后 toast 反馈
  const handleReset = async () => {
    if (!onReset) return
    setIsResetting(true)
    try {
      await onReset()
      toast.success("统计数据已重置")
    } catch (error) {
      toast.error("重置失败: " + (error as Error).message)
    } finally {
      setIsResetting(false)
    }
  }

  return (
    <div className="space-y-3">
      {/* 核心指标 */}
      <div className="flex items-center justify-between">
        <div className="flex flex-wrap gap-4 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">总请求:</span>
            <span className="font-semibold">{formatNumber(stats.request_count)}</span>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">缓存命中率:</span>
            <Badge className={rateBadgeClass(cacheHitRate, "hit")}>
              {totalCacheRequests > 0 ? cacheHitRate.toFixed(1) : "0.0"}%
            </Badge>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">错误率:</span>
            <Badge className={rateBadgeClass(errorRate, "error")}>
              {errorRate.toFixed(2)}%
            </Badge>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">流量:</span>
            <span className="font-mono text-xs">{formatBytes(stats.bytes_transferred)}</span>
          </div>
        </div>

        {onReset && (
          <Button
            variant="ghost"
            size="sm"
            onClick={handleReset}
            disabled={isResetting}
            className="h-7 px-2 text-muted-foreground"
          >
            <RotateCcw className={`h-3.5 w-3.5 ${isResetting ? "animate-spin" : ""}`} />
            <span className="ml-1.5 text-xs">重置</span>
          </Button>
        )}
      </div>

      {/* 状态码分布 */}
      <div className="flex flex-wrap gap-4 text-sm">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">状态码:</span>
          <div className="flex gap-2">
            {stats.status_2xx > 0 && (
              <Badge variant="outline" className={statusBadgeClass("2xx")}>
                2xx: {formatNumber(stats.status_2xx)}
              </Badge>
            )}
            {stats.status_3xx > 0 && (
              <Badge variant="outline" className={statusBadgeClass("3xx")}>
                3xx: {formatNumber(stats.status_3xx)}
              </Badge>
            )}
            {stats.status_4xx > 0 && (
              <Badge variant="outline" className={statusBadgeClass("4xx")}>
                4xx: {formatNumber(stats.status_4xx)}
              </Badge>
            )}
            {stats.status_5xx > 0 && (
              <Badge variant="outline" className={statusBadgeClass("5xx")}>
                5xx: {formatNumber(stats.status_5xx)}
              </Badge>
            )}
          </div>
        </div>
      </div>

      {/* 缓存详细统计 */}
      {totalCacheRequests > 0 && (
        <div className="flex flex-wrap gap-4 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">缓存命中:</span>
            <span className="text-xs">
              {formatNumber(stats.cache_hits)} / {formatNumber(totalCacheRequests)}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">节省流量:</span>
            <span className="font-mono text-xs text-success">
              {formatBytes(stats.bytes_saved)}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">平均延迟:</span>
            <span className="font-mono text-xs">{stats.avg_latency}</span>
          </div>
        </div>
      )}
    </div>
  )
}
