"use client"

import { useMemo } from "react"
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { ActiveHost } from "./types"

// RefererTrend 选中来源的逐日请求折线
export default function RefererTrend({ hosts, colorOf }: { hosts: ActiveHost[]; colorOf: (host: string) => string | undefined }) {
  const data = useMemo(() => {
    const dates = hosts[0]?.daily.map((d) => d.date) ?? []
    return dates.map((date, i) => {
      const row: Record<string, string | number> = { date: date.slice(5) }
      for (const h of hosts) row[h.host] = h.daily[i]?.requests ?? 0
      return row
    })
  }, [hosts])

  if (hosts.length === 0) {
    return <div className="flex h-56 items-center justify-center text-sm text-foreground-subtle">点击列表左侧的圆点把来源加入折线图</div>
  }
  return (
    <div className="h-56 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, right: 8, left: -8, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />
          <XAxis dataKey="date" stroke="hsl(var(--foreground-subtle))" fontSize={11} tickLine={false} />
          <YAxis stroke="hsl(var(--foreground-subtle))" fontSize={11} tickLine={false} axisLine={false} />
          <Tooltip
            contentStyle={{ background: "hsl(var(--popover))", border: "1px solid hsl(var(--border))", borderRadius: 8, color: "hsl(var(--popover-foreground))", fontSize: 12 }}
            formatter={(v: number) => v.toLocaleString()}
          />
          {hosts.map((h) => (
            <Line key={h.host} type="monotone" dataKey={h.host} stroke={colorOf(h.host)} strokeWidth={2} dot={false} isAnimationActive={false} />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}
