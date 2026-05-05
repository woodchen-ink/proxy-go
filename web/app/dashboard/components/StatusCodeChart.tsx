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
  Cell,
} from "recharts"

// statusColor HTTP 状态码 → 语义色 (状态语义图表直接走 destructive/warning/info/success)
function statusColor(code: number) {
  if (code >= 500) return "hsl(var(--destructive))"
  if (code >= 400) return "hsl(var(--warning))"
  if (code >= 300) return "hsl(var(--info))"
  return "hsl(var(--success))"
}

// StatusCodeChart 状态码分布柱状图, 替换原"状态码统计"网格文字
export default function StatusCodeChart({ stats }: { stats: Record<string, number> }) {
  const entries = Object.entries(stats || {})
    .map(([code, count]) => ({ code, count: Number(count) }))
    .filter((e) => e.count > 0)
    .sort((a, b) => a.code.localeCompare(b.code))

  const total = entries.reduce((s, e) => s + e.count, 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          状态码分布
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            (总请求 {total.toLocaleString()})
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {entries.length === 0 ? (
          <div className="flex h-48 items-center justify-center text-muted-foreground">
            暂无数据
          </div>
        ) : (
          <div className="h-64 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={entries}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" />
                <XAxis dataKey="code" stroke="hsl(var(--muted-foreground))" fontSize={12} />
                <YAxis stroke="hsl(var(--muted-foreground))" fontSize={12} />
                <Tooltip
                  contentStyle={{
                    background: "hsl(var(--popover))",
                    border: "1px solid hsl(var(--border))",
                    borderRadius: 8,
                    color: "hsl(var(--popover-foreground))",
                  }}
                  formatter={(v: number) => v.toLocaleString()}
                  labelFormatter={(label) => `状态码 ${label}`}
                />
                <Bar dataKey="count" radius={[4, 4, 0, 0]} isAnimationActive={false}>
                  {entries.map((e) => (
                    <Cell key={e.code} fill={statusColor(parseInt(e.code))} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
