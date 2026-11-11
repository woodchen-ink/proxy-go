"use client"

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { formatBytes } from "@/lib/format"
import { cn } from "@/lib/utils"

export interface RequestLogRow {
  Time: string
  Host?: string // 旧版节点不带, 此时路径不出链接 (相对路径会落到中控域名)
  Path: string
  Status: number
  Latency: number
  BytesSent: number
  ClientIP: string
  Referer?: string
  node_id?: string
}

// 后端每节点保留 100 条; 合并后按时间倒序截前 100 条
const PAGE = 50
const MAX = 100

function formatLatency(ns: number) {
  if (!ns || isNaN(ns)) return "-"
  if (ns < 1_000_000) return `${(ns / 1000).toFixed(0)} µs`
  if (ns < 1_000_000_000) return `${(ns / 1_000_000).toFixed(ns < 10_000_000 ? 1 : 0)} ms`
  return `${(ns / 1_000_000_000).toFixed(2)} s`
}

function formatClock(s: string) {
  if (!s) return "-"
  return new Date(s).toLocaleTimeString("zh-CN", { hour12: false })
}

// statusClass 常态 2xx/3xx 弱化, 4xx 警告, 5xx 危险
function statusClass(status: number) {
  if (status >= 500) return "bg-destructive-soft text-destructive"
  if (status >= 400) return "bg-warning-soft text-warning"
  return "text-foreground-muted"
}

interface RecentRequestsProps {
  rows: RequestLogRow[]
  nodes: string[]
  node: string
  onNodeChange: (node: string) => void
  colors: Record<string, string>
  compact?: boolean // 节点详情抽屉里: 不显示节点列与筛选
}

// RecentRequests 最近请求: 可按节点筛选, 节点列带节点色块
export default function RecentRequests({ rows, nodes, node, onNodeChange, colors, compact }: RecentRequestsProps) {
  const [limit, setLimit] = useState(PAGE)
  const total = Math.min(rows.length, MAX)
  const shown = rows.slice(0, limit)

  const table = (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[48rem] text-sm">
        <thead className="bg-muted text-foreground-muted">
          <tr className="border-b">
            <th className="px-4 py-2 text-left font-medium">时间</th>
            {!compact && <th className="px-3 py-2 text-left font-medium">节点</th>}
            <th className="px-3 py-2 text-left font-medium">路径</th>
            <th className="px-3 py-2 text-left font-medium">状态</th>
            <th className="px-3 py-2 text-right font-medium">延迟</th>
            <th className="px-3 py-2 text-right font-medium">大小</th>
            <th className="px-3 py-2 text-left font-medium">来源</th>
            <th className="px-4 py-2 text-left font-medium">客户端</th>
          </tr>
        </thead>
        <tbody className="divide-y tabular-nums">
          {shown.length === 0 && (
            <tr>
              <td colSpan={compact ? 7 : 8} className="px-4 py-8 text-center text-foreground-muted">
                暂无请求
              </td>
            </tr>
          )}
          {shown.map((r, i) => {
            const url = r.Host ? `https://${r.Host}${r.Path}` : ""
            return (
              <tr key={i} className="hover:bg-accent">
                <td className="whitespace-nowrap px-4 py-2 text-foreground-muted" title={new Date(r.Time).toLocaleString("zh-CN")}>
                  {formatClock(r.Time)}
                </td>
                {!compact && (
                  <td className="whitespace-nowrap px-3 py-2">
                    <span className="inline-flex items-center gap-1.5 text-foreground-muted">
                      <span aria-hidden className="h-2 w-2 rounded-sm" style={{ background: colors[r.node_id ?? ""] ?? "hsl(var(--border))" }} />
                      {r.node_id}
                    </span>
                  </td>
                )}
                <td className="max-w-xs truncate px-3 py-2 font-medium">
                  {url ? (
                    <a href={url} target="_blank" rel="noopener noreferrer" className="text-link hover:underline" title={url}>
                      {r.Path}
                    </a>
                  ) : (
                    <span title={r.Path}>{r.Path}</span>
                  )}
                </td>
                <td className="px-3 py-2">
                  <span className={cn("rounded-md px-1.5 py-0.5 text-xs", statusClass(r.Status))}>{r.Status}</span>
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-right text-foreground-muted">{formatLatency(r.Latency)}</td>
                <td className="whitespace-nowrap px-3 py-2 text-right text-foreground-muted">{formatBytes(r.BytesSent)}</td>
                <td className="max-w-[14rem] truncate px-3 py-2">
                  {r.Referer ? (
                    <a href={r.Referer} target="_blank" rel="noopener noreferrer" className="text-link hover:underline" title={r.Referer}>
                      {r.Referer}
                    </a>
                  ) : (
                    <span className="text-foreground-subtle">-</span>
                  )}
                </td>
                <td className="whitespace-nowrap px-4 py-2">
                  <a href={`https://ip.czl.net/${r.ClientIP}`} target="_blank" rel="noopener noreferrer" className="text-link hover:underline">
                    {r.ClientIP}
                  </a>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      {limit < total && (
        <div className="flex justify-center border-t py-3">
          <Button variant="ghost" size="sm" onClick={() => setLimit((n) => Math.min(n + PAGE, MAX))}>
            加载更多 (+{Math.min(PAGE, total - limit)})
          </Button>
        </div>
      )}
    </div>
  )

  if (compact) return table

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <h2 className="text-base font-semibold">
          最近请求 <span className="ml-1 text-sm font-normal text-foreground-subtle tabular-nums">{shown.length} / {total}</span>
        </h2>
        <div role="radiogroup" aria-label="按节点筛选" className="flex flex-wrap gap-1">
          {["", ...nodes].map((id) => (
            <button
              key={id || "all"}
              role="radio"
              aria-checked={node === id}
              onClick={() => {
                setLimit(PAGE)
                onNodeChange(id)
              }}
              className={cn(
                "h-7 rounded-full border px-3 text-sm transition-colors",
                node === id ? "border-primary bg-primary text-primary-foreground" : "text-foreground-muted hover:bg-accent hover:text-foreground",
              )}
            >
              {id || "全部节点"}
            </button>
          ))}
        </div>
      </div>
      {table}
    </Card>
  )
}
