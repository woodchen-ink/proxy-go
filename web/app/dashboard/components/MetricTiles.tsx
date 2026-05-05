"use client"

import { Card, CardContent } from "@/components/ui/card"
import {
  ResponsiveContainer,
  RadialBarChart,
  RadialBar,
  PolarAngleAxis,
} from "recharts"

interface MetricsLite {
  uptime: string
  active_requests: number
  total_requests: number
  total_errors: number
  error_rate: number
  total_bytes: number
  bytes_per_second: number
  current_session_requests: number
  requests_per_second: number
  num_goroutine: number
  memory_usage: string
  avg_response_time: string
  current_bandwidth: string
}

// formatBytes 字节数 → 友好单位
function formatBytes(bytes: number): string {
  if (!bytes || isNaN(bytes)) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// formatNumber 大数 → 缩写 (1.2K / 3.4M)
function formatNumber(n: number): string {
  if (!n) return "0"
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return n.toString()
}

// MetricTiles 概览数据栅格, 把原来的"基础指标 + 系统指标"两张卡合并为一组卡片
export default function MetricTiles({ m }: { m: MetricsLite }) {
  const errorPct = (m.error_rate || 0) * 100

  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <StatCard label="运行时间" value={m.uptime || "-"} />
      <StatCard
        label="活跃请求"
        value={String(m.active_requests || 0)}
        accent="chart-1"
      />
      <StatCard
        label="总请求数"
        value={formatNumber(m.total_requests || 0)}
        sub={`本会话 ${formatNumber(m.current_session_requests || 0)}`}
      />
      <ErrorRateCard
        errorRate={errorPct}
        totalErrors={m.total_errors || 0}
      />

      <StatCard
        label="总传输"
        value={formatBytes(m.total_bytes || 0)}
        sub={`实时 ${m.current_bandwidth || "0 B/s"}`}
      />
      <StatCard
        label="平均 RPS"
        value={(m.requests_per_second || 0).toFixed(2)}
        sub="近 5 分钟"
      />
      <StatCard
        label="平均响应"
        value={m.avg_response_time || "0 ms"}
      />
      <StatCard
        label="Goroutine"
        value={String(m.num_goroutine || 0)}
        sub={`内存 ${m.memory_usage || "-"}`}
      />
    </div>
  )
}

// StatCard 单个统计指标卡, 大字号数值 + 次级说明
function StatCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string
  value: string
  sub?: string
  accent?: "chart-1" | "chart-2"
}) {
  const accentClass =
    accent === "chart-1"
      ? "text-[hsl(var(--chart-1))]"
      : accent === "chart-2"
      ? "text-[hsl(var(--chart-2))]"
      : "text-foreground"
  return (
    <Card>
      <CardContent className="py-4">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className={`mt-1 text-2xl font-semibold ${accentClass}`}>{value}</div>
        {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
      </CardContent>
    </Card>
  )
}

// ErrorRateCard 错误率卡片, 带圆环可视化 (语义上是状态色, 故用 destructive)
function ErrorRateCard({
  errorRate,
  totalErrors,
}: {
  errorRate: number
  totalErrors: number
}) {
  const data = [{ name: "error", value: Math.min(errorRate, 100) }]
  const color =
    errorRate >= 5 ? "hsl(var(--destructive))" : "hsl(var(--chart-2))"
  return (
    <Card>
      <CardContent className="flex items-center gap-3 py-4">
        <div className="relative h-16 w-16 shrink-0">
          <ResponsiveContainer width="100%" height="100%">
            <RadialBarChart
              cx="50%"
              cy="50%"
              innerRadius="70%"
              outerRadius="100%"
              barSize={6}
              data={data}
              startAngle={90}
              endAngle={-270}
            >
              <PolarAngleAxis
                type="number"
                domain={[0, 100]}
                tick={false}
                axisLine={false}
              />
              <RadialBar dataKey="value" cornerRadius={6} fill={color} background={{ fill: "hsl(var(--muted))" }} />
            </RadialBarChart>
          </ResponsiveContainer>
          <div className="absolute inset-0 flex items-center justify-center text-[10px] font-semibold">
            {errorRate.toFixed(1)}%
          </div>
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-xs text-muted-foreground">错误率</div>
          <div className="mt-1 text-lg font-semibold">
            {totalErrors.toLocaleString()}
          </div>
          <div className="text-xs text-muted-foreground">累计错误</div>
        </div>
      </CardContent>
    </Card>
  )
}
