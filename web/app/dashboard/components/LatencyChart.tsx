"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts"

const BUCKET_ORDER = ["lt10ms", "10-50ms", "50-200ms", "200-1000ms", "gt1s"]
const BUCKET_LABEL: Record<string, string> = {
  lt10ms: "<10ms",
  "10-50ms": "10-50ms",
  "50-200ms": "50-200ms",
  "200-1000ms": "0.2-1s",
  gt1s: ">1s",
}

interface LatencyStats {
  min?: string
  max?: string
  distribution?: Record<string, number>
}

// LatencyChart 响应时间分布柱状图, 替换原"延迟统计"卡片中的网格
export default function LatencyChart({ stats }: { stats?: LatencyStats }) {
  const dist = stats?.distribution || {}
  const entries = BUCKET_ORDER.map((b) => ({
    bucket: BUCKET_LABEL[b],
    count: Number(dist[b] || 0),
  }))
  const total = entries.reduce((s, e) => s + e.count, 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          响应时间分布
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            最小 {stats?.min || "0ms"} · 最大 {stats?.max || "0ms"}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {total === 0 ? (
          <div className="flex h-48 items-center justify-center text-muted-foreground">
            暂无数据
          </div>
        ) : (
          <div className="h-64 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={entries}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                <XAxis dataKey="bucket" stroke="hsl(var(--muted-foreground))" fontSize={12} />
                <YAxis stroke="hsl(var(--muted-foreground))" fontSize={12} />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 8,
                    color: "hsl(var(--popover-foreground))",
                  }}
                  formatter={(v: number) => v.toLocaleString()}
                />
                <Bar
                  dataKey="count"
                  fill="hsl(var(--chart-1))"
                  radius={[4, 4, 0, 0]}
                  isAnimationActive={false}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
