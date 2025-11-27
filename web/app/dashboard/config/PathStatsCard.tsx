import React, { useState } from "react"
import { Card, CardContent } from "@/components/ui/card"
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

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + " " + sizes[i]
}

function formatNumber(num: number): string {
  return num.toLocaleString()
}

export default function PathStatsCard({ stats, onReset }: PathStatsCardProps) {
  const [isResetting, setIsResetting] = useState(false)

  if (!stats || stats.request_count === 0) {
    return (
      <div className="text-sm text-muted-foreground">
        暂无统计数据
      </div>
    )
  }

  const errorRate = stats.request_count > 0
    ? ((stats.error_count / stats.request_count) * 100).toFixed(2)
    : "0.00"

  const cacheHitRate = (stats.cache_hit_rate * 100).toFixed(1)
  const totalCacheRequests = stats.cache_hits + stats.cache_misses

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
    <div className="space-y-3 relative">
      {/* 重置按钮 */}
      {onReset && (
        <div className="absolute top-0 right-0">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleReset}
            disabled={isResetting}
            className="h-7 px-2 text-muted-foreground hover:text-foreground"
          >
            <RotateCcw className={`h-3.5 w-3.5 ${isResetting ? 'animate-spin' : ''}`} />
            <span className="ml-1.5 text-xs">重置</span>
          </Button>
        </div>
      )}
      {/* 第一行：请求统计 */}
      <div className="flex flex-wrap gap-4 text-sm">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">总请求:</span>
          <span className="font-semibold">{formatNumber(stats.request_count)}</span>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">错误率:</span>
          <Badge variant={parseFloat(errorRate) > 5 ? "destructive" : "secondary"}>
            {errorRate}%
          </Badge>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">平均延迟:</span>
          <span className="font-mono text-xs">{stats.avg_latency}</span>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">流量:</span>
          <span className="font-mono text-xs">{formatBytes(stats.bytes_transferred)}</span>
        </div>
      </div>

      {/* 第二行：状态码分布 */}
      <div className="flex flex-wrap gap-4 text-sm">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">状态码:</span>
          <div className="flex gap-2">
            {stats.status_2xx > 0 && (
              <Badge variant="outline" className="bg-green-50 text-green-700 border-green-200">
                2xx: {formatNumber(stats.status_2xx)}
              </Badge>
            )}
            {stats.status_3xx > 0 && (
              <Badge variant="outline" className="bg-blue-50 text-blue-700 border-blue-200">
                3xx: {formatNumber(stats.status_3xx)}
              </Badge>
            )}
            {stats.status_4xx > 0 && (
              <Badge variant="outline" className="bg-yellow-50 text-yellow-700 border-yellow-200">
                4xx: {formatNumber(stats.status_4xx)}
              </Badge>
            )}
            {stats.status_5xx > 0 && (
              <Badge variant="outline" className="bg-red-50 text-red-700 border-red-200">
                5xx: {formatNumber(stats.status_5xx)}
              </Badge>
            )}
          </div>
        </div>
      </div>

      {/* 第三行：缓存统计 */}
      {totalCacheRequests > 0 && (
        <div className="flex flex-wrap gap-4 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">缓存命中率:</span>
            <Badge
              variant="outline"
              className={
                parseFloat(cacheHitRate) > 70
                  ? "bg-green-50 text-green-700 border-green-200"
                  : parseFloat(cacheHitRate) > 40
                  ? "bg-yellow-50 text-yellow-700 border-yellow-200"
                  : "bg-red-50 text-red-700 border-red-200"
              }
            >
              {cacheHitRate}%
            </Badge>
            <span className="text-xs text-muted-foreground">
              ({formatNumber(stats.cache_hits)} / {formatNumber(totalCacheRequests)})
            </span>
          </div>

          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">节省流量:</span>
            <span className="font-mono text-xs text-green-600">
              {formatBytes(stats.bytes_saved)}
            </span>
          </div>
        </div>
      )}
    </div>
  )
}
