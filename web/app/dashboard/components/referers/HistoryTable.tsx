"use client"

import { Fragment, useState } from "react"
import { ChevronRight } from "lucide-react"
import { formatBytes } from "@/lib/format"
import HostBadges from "./HostBadges"
import HostLink from "./HostLink"
import PathList from "./PathList"
import RowActions from "./RowActions"
import type { HistoryHost, HostAction } from "./types"

// HistoryTable 近 30 天没再出现的来源, 来自长期汇总, 按最后出现倒序
export default function HistoryTable({ rows, onAction, busy }: { rows: HistoryHost[]; onAction: (a: HostAction) => void; busy: string | null }) {
  const [open, setOpen] = useState<string | null>(null)
  return (
    <table className="w-full min-w-[52rem] text-sm">
      <thead className="sticky top-0 z-10 bg-muted text-foreground-muted">
        <tr className="border-b">
          <th className="px-4 py-2 text-left font-medium">来源</th>
          <th className="px-3 py-2 text-right font-medium">累计请求</th>
          <th className="px-3 py-2 text-right font-medium">流量</th>
          <th className="px-3 py-2 text-right font-medium">已拦截</th>
          <th className="px-3 py-2 text-left font-medium">首次出现</th>
          <th className="px-3 py-2 text-left font-medium">最后出现</th>
          <th className="px-4 py-2 text-right font-medium">操作</th>
        </tr>
      </thead>
      <tbody className="divide-y tabular-nums">
        {rows.map((r) => {
          const expanded = open === r.host
          return (
            <Fragment key={r.host}>
              <tr className="hover:bg-accent">
                <td className="px-4 py-2">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <button type="button" onClick={() => setOpen(expanded ? null : r.host)} className="flex min-w-0 items-center gap-1.5 text-left">
                      <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-foreground-subtle transition-transform ${expanded ? "rotate-90" : ""}`} />
                      <span className="truncate font-mono font-medium">{r.host}</span>
                    </button>
                    <HostLink host={r.host} />
                    <HostBadges isOwn={r.is_own} isBanned={r.is_banned} />
                  </div>
                </td>
                <td className="px-3 py-2 text-right">{r.requests.toLocaleString()}</td>
                <td className="px-3 py-2 text-right text-foreground-muted">{formatBytes(r.bytes)}</td>
                <td className="px-3 py-2 text-right">
                  {r.blocked > 0 ? <span className="text-destructive">{r.blocked.toLocaleString()}</span> : <span className="text-foreground-subtle">0</span>}
                </td>
                <td className="px-3 py-2 text-foreground-muted">{r.first_seen}</td>
                <td className="px-3 py-2 text-foreground-muted">{r.last_seen}</td>
                <td className="px-4 py-2 text-right">
                  <RowActions host={r.host} isOwn={r.is_own} isBanned={r.is_banned} busy={busy === r.host} onAction={onAction} />
                </td>
              </tr>
              {expanded && (
                <tr className="bg-background-subtle">
                  <td colSpan={7} className="px-10 py-3">
                    <PathList paths={r.top_paths} note={r.peak_date ? `请求最多的一天 (${r.peak_date}) 被引用的路径` : undefined} />
                  </td>
                </tr>
              )}
            </Fragment>
          )
        })}
      </tbody>
    </table>
  )
}
