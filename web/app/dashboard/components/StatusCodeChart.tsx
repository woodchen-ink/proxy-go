"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Tooltip,
} from "recharts"

// statusColor HTTP 状态码 → 语义色 (状态语义图表直接走 destructive/warning/info/success, 不复用 chart 分类色)
function statusColor(code: number) {
  if (code >= 500) return "hsl(var(--destructive))"
  if (code >= 400) return "hsl(var(--warning))"
  if (code >= 300) return "hsl(var(--info))"
  return "hsl(var(--success))"
}

// StatusCodeChart 状态码分布环形图; 中心展示总请求数, 扇区按 2xx/3xx/4xx/5xx 语义色着色
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
          <div className="flex h-64 items-center justify-center text-muted-foreground">
            暂无数据
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-[1fr_auto] md:items-center">
            <div className="relative h-64 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={entries}
                    dataKey="count"
                    nameKey="code"
                    innerRadius="60%"
                    outerRadius="85%"
                    paddingAngle={1}
                    strokeWidth={0}
                    isAnimationActive={false}
                  >
                    {entries.map((e) => (
                      <Cell key={e.code} fill={statusColor(parseInt(e.code))} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{
                      background: "hsl(var(--popover))",
                      border: "1px solid hsl(var(--border))",
                      borderRadius: 8,
                      color: "hsl(var(--popover-foreground))",
                    }}
                    formatter={(v: number, _name, payload) => {
                      const pct = total > 0 ? ((v / total) * 100).toFixed(1) : "0"
                      return [`${v.toLocaleString()} (${pct}%)`, `状态码 ${payload?.payload?.code}`]
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
              <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                <span className="text-2xl font-semibold tabular-nums">
                  {total.toLocaleString()}
                </span>
                <span className="text-xs text-muted-foreground">总请求</span>
              </div>
            </div>
            <ul className="space-y-2 text-sm">
              {entries.map((e) => {
                const pct = total > 0 ? ((e.count / total) * 100).toFixed(1) : "0"
                return (
                  <li key={e.code} className="flex items-center gap-2">
                    <span
                      className="inline-block h-3 w-3 rounded-sm"
                      style={{ background: statusColor(parseInt(e.code)) }}
                    />
                    <span className="font-mono">{e.code}</span>
                    <span className="text-muted-foreground">
                      {e.count.toLocaleString()} · {pct}%
                    </span>
                  </li>
                )
              })}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
