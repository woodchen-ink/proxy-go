"use client"

import { Card } from "@/components/ui/card"
import { formatBytes, formatPct } from "@/lib/format"

export interface TopReferer {
  path: string
  request_count: number
  error_count: number
  avg_latency: string
  bytes_transferred: number
  last_access_time: number
}

function timeAgo(sec: number) {
  const diff = Math.floor(Date.now() / 1000 - sec)
  if (diff < 60) return `${diff} 秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return new Date(sec * 1000).toLocaleDateString("zh-CN")
}

// TopReferers 近 24 小时引用来源 (URL 维度), 来自在线节点快照
export default function TopReferers({ rows }: { rows: TopReferer[] }) {
  if (rows.length === 0) return null
  return (
    <Card className="overflow-hidden">
      <div className="border-b px-5 py-3">
        <h2 className="text-base font-semibold">
          引用来源 <span className="ml-1 text-sm font-normal text-foreground-subtle">近 24 小时 · {rows.length} 条</span>
        </h2>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[44rem] text-sm">
          <thead className="bg-muted text-foreground-muted">
            <tr className="border-b">
              <th className="px-5 py-2 text-left font-medium">来源</th>
              <th className="px-3 py-2 text-right font-medium">请求</th>
              <th className="px-3 py-2 text-right font-medium">错误率</th>
              <th className="px-3 py-2 text-right font-medium">平均延迟</th>
              <th className="px-3 py-2 text-right font-medium">流量</th>
              <th className="px-5 py-2 text-right font-medium">最后访问</th>
            </tr>
          </thead>
          <tbody className="divide-y tabular-nums">
            {rows.map((r) => {
              const errRate = r.request_count > 0 ? r.error_count / r.request_count : 0
              return (
                <tr key={r.path} className="hover:bg-accent">
                  <td className="max-w-sm truncate px-5 py-2 font-medium">
                    <a href={r.path} target="_blank" rel="noopener noreferrer" className="text-link hover:underline" title={r.path}>
                      {r.path}
                    </a>
                  </td>
                  <td className="px-3 py-2 text-right text-foreground-muted">{r.request_count}</td>
                  <td className={errRate >= 0.05 ? "px-3 py-2 text-right text-destructive" : "px-3 py-2 text-right text-foreground-muted"}>
                    {formatPct(errRate)}
                  </td>
                  <td className="px-3 py-2 text-right text-foreground-muted">{r.avg_latency}</td>
                  <td className="px-3 py-2 text-right text-foreground-muted">{formatBytes(r.bytes_transferred)}</td>
                  <td className="px-5 py-2 text-right text-foreground-muted" title={new Date(r.last_access_time * 1000).toLocaleString("zh-CN")}>
                    {timeAgo(r.last_access_time)}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Card>
  )
}
