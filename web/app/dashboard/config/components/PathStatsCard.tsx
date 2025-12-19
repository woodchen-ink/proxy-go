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
      <div className="text-sm" style={{ color: '#999' }}>
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
    <div className="space-y-3">
      {/* 核心指标 */}
      <div className="flex items-center justify-between">
        <div className="flex flex-wrap gap-4 text-sm">
          <div className="flex items-center gap-2">
            <span style={{ color: '#666' }}>总请求:</span>
            <span className="font-semibold">{formatNumber(stats.request_count)}</span>
          </div>

          <div className="flex items-center gap-2">
            <span style={{ color: '#666' }}>缓存命中率:</span>
            <Badge
              style={{
                backgroundColor: parseFloat(cacheHitRate) > 70 ? '#F4E8E0' : parseFloat(cacheHitRate) > 40 ? '#fcfce0' : '#fce8e8',
                color: parseFloat(cacheHitRate) > 70 ? '#518751' : parseFloat(cacheHitRate) > 40 ? '#9d8b00' : '#b85e48',
                border: 'none'
              }}
            >
              {totalCacheRequests > 0 ? cacheHitRate : '0.0'}%
            </Badge>
          </div>

          <div className="flex items-center gap-2">
            <span style={{ color: '#666' }}>错误率:</span>
            <Badge
              style={{
                backgroundColor: parseFloat(errorRate) > 5 ? '#fce8e8' : '#F4E8E0',
                color: parseFloat(errorRate) > 5 ? '#b85e48' : '#518751',
                border: 'none'
              }}
            >
              {errorRate}%
            </Badge>
          </div>

          <div className="flex items-center gap-2">
            <span style={{ color: '#666' }}>流量:</span>
            <span className="font-mono text-xs">{formatBytes(stats.bytes_transferred)}</span>
          </div>
        </div>

        {onReset && (
          <Button
            variant="ghost"
            size="sm"
            onClick={handleReset}
            disabled={isResetting}
            className="h-7 px-2 hover:bg-[#EEEDEC]"
            style={{ color: '#666' }}
          >
            <RotateCcw className={`h-3.5 w-3.5 ${isResetting ? 'animate-spin' : ''}`} />
            <span className="ml-1.5 text-xs">重置</span>
          </Button>
        )}
      </div>

      {/* 状态码分布 */}
      <div className="flex flex-wrap gap-4 text-sm">
        <div className="flex items-center gap-2">
          <span style={{ color: '#666' }}>状态码:</span>
          <div className="flex gap-2">
            {stats.status_2xx > 0 && (
              <Badge
                variant="outline"
                style={{
                  backgroundColor: '#F4E8E0',
                  color: '#518751',
                  borderColor: '#518751'
                }}
              >
                2xx: {formatNumber(stats.status_2xx)}
              </Badge>
            )}
            {stats.status_3xx > 0 && (
              <Badge
                variant="outline"
                style={{
                  backgroundColor: '#F8F7F6',
                  color: '#666',
                  borderColor: '#999'
                }}
              >
                3xx: {formatNumber(stats.status_3xx)}
              </Badge>
            )}
            {stats.status_4xx > 0 && (
              <Badge
                variant="outline"
                style={{
                  backgroundColor: '#fcfce0',
                  color: '#9d8b00',
                  borderColor: '#ecec70'
                }}
              >
                4xx: {formatNumber(stats.status_4xx)}
              </Badge>
            )}
            {stats.status_5xx > 0 && (
              <Badge
                variant="outline"
                style={{
                  backgroundColor: '#fce8e8',
                  color: '#b85e48',
                  borderColor: '#b85e48'
                }}
              >
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
            <span style={{ color: '#666' }}>缓存命中:</span>
            <span className="text-xs">
              {formatNumber(stats.cache_hits)} / {formatNumber(totalCacheRequests)}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <span style={{ color: '#666' }}>节省流量:</span>
            <span className="font-mono text-xs" style={{ color: '#518751' }}>
              {formatBytes(stats.bytes_saved)}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <span style={{ color: '#666' }}>平均延迟:</span>
            <span className="font-mono text-xs">{stats.avg_latency}</span>
          </div>
        </div>
      )}
    </div>
  )
}
