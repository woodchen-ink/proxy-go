"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts"

// parseBandwidth 把 "1.23 KB/min" 这种后端字符串解析成字节数
//
// 后端目前以 "value unit/min" 形式返回历史带宽; 解析失败时回 0
function parseBandwidth(text: string): number {
  if (!text) return 0
  const m = text.match(/([\d.]+)\s*([KMGT]?B)/i)
  if (!m) return 0
  const v = parseFloat(m[1])
  const unit = m[2].toUpperCase()
  const map: Record<string, number> = {
    B: 1,
    KB: 1024,
    MB: 1024 ** 2,
    GB: 1024 ** 3,
    TB: 1024 ** 4,
  }
  return v * (map[unit] || 1)
}

// formatBytes 字节数 → 友好单位
function formatBytes(bytes: number): string {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// BandwidthChart 带宽历史面积图, 替换原"带宽统计"卡片中的网格
export default function BandwidthChart({
  history,
  current,
}: {
  history: Record<string, string>
  current: string
}) {
  const entries = Object.entries(history || {})
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([time, val]) => ({
      time: time.slice(-5), // 只展示 HH:MM
      bytes: parseBandwidth(val),
    }))

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          带宽历史
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            当前 {current || "0 B/s"}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {entries.length === 0 ? (
          <div className="flex h-48 items-center justify-center text-muted-foreground">
            暂无历史数据 (运行至少 1 分钟后出现)
          </div>
        ) : (
          <div className="h-64 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={entries}>
                <defs>
                  <linearGradient id="bandwidthFill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="hsl(var(--chart-1))" stopOpacity={0.4} />
                    <stop offset="100%" stopColor="hsl(var(--chart-1))" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                <XAxis dataKey="time" stroke="hsl(var(--muted-foreground))" fontSize={12} />
                <YAxis
                  stroke="hsl(var(--muted-foreground))"
                  fontSize={12}
                  tickFormatter={formatBytes}
                />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 8,
                    color: "hsl(var(--popover-foreground))",
                  }}
                  formatter={(v: number) => `${formatBytes(v)}/min`}
                />
                <Area
                  type="monotone"
                  dataKey="bytes"
                  stroke="hsl(var(--chart-1))"
                  strokeWidth={2}
                  fill="url(#bandwidthFill)"
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
