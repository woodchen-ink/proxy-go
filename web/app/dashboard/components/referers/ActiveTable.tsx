"use client"

import { Fragment, useState } from "react"
import { ChevronRight } from "lucide-react"
import { formatBytes } from "@/lib/format"
import HostBadges from "./HostBadges"
import HostLink from "./HostLink"
import PathList from "./PathList"
import RowActions from "./RowActions"
import { PATH_CAP, type ActiveHost, type HostAction } from "./types"

interface Props {
  rows: ActiveHost[]
  colorOf: (host: string) => string | undefined
  onToggleChart: (host: string) => void
  onAction: (a: HostAction) => void
  busy: string | null
}

// ActiveTable 近 30 天出现过的来源; 左侧色点切换是否画进折线图 (仅前 50 个带逐日数据)
export default function ActiveTable({ rows, colorOf, onToggleChart, onAction, busy }: Props) {
  const [open, setOpen] = useState<string | null>(null)
  return (
    <table className="w-full min-w-[56rem] text-sm">
      <thead className="sticky top-0 z-10 bg-muted text-foreground-muted">
        <tr className="border-b">
          <th className="w-8 py-2 pl-4" />
          <th className="px-2 py-2 text-left font-medium">来源</th>
          <th className="px-3 py-2 text-right font-medium">30 天请求</th>
          <th className="px-3 py-2 text-right font-medium">流量</th>
          <th className="px-3 py-2 text-right font-medium">已拦截</th>
          <th className="px-3 py-2 text-right font-medium" title="单日引用的不同路径数, 30 天内取最大">路径数</th>
          <th className="px-3 py-2 text-left font-medium">首次出现</th>
          <th className="px-3 py-2 text-left font-medium">最近</th>
          <th className="px-4 py-2 text-right font-medium">操作</th>
        </tr>
      </thead>
      <tbody className="divide-y tabular-nums">
        {rows.map((r) => {
          const color = colorOf(r.host)
          const chartable = r.daily.length > 0
          const expanded = open === r.host
          return (
            <Fragment key={r.host}>
              <tr className="hover:bg-accent">
                <td className="py-2 pl-4">
                  <button
                    type="button"
                    disabled={!chartable}
                    onClick={() => onToggleChart(r.host)}
                    title={chartable ? "在折线图中显示 / 隐藏" : "只有请求量前 50 的来源带逐日数据"}
                    className="flex h-4 w-4 items-center justify-center rounded-full border disabled:opacity-30"
                    style={color ? { background: color, borderColor: color } : undefined}
                  />
                </td>
                <td className="px-2 py-2">
                  <div className="flex min-w-0 items-center gap-1.5">
                    <button type="button" onClick={() => setOpen(expanded ? null : r.host)} className="flex min-w-0 items-center gap-1.5 text-left">
                      <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-foreground-subtle transition-transform ${expanded ? "rotate-90" : ""}`} />
                      <span className="truncate font-mono font-medium">{r.host}</span>
                    </button>
                    <HostLink host={r.host} />
                    <HostBadges isNew={r.is_new} isOwn={r.is_own} isBanned={r.is_banned} wholeSite={r.distinct_paths >= PATH_CAP} />
                  </div>
                </td>
                <td className="px-3 py-2 text-right">{r.requests.toLocaleString()}</td>
                <td className="px-3 py-2 text-right text-foreground-muted">{formatBytes(r.bytes)}</td>
                <td className="px-3 py-2 text-right">
                  {r.blocked > 0 ? <span className="text-destructive">{r.blocked.toLocaleString()}</span> : <span className="text-foreground-subtle">0</span>}
                </td>
                <td className="px-3 py-2 text-right text-foreground-muted">
                  {r.distinct_paths > 0 ? `${r.distinct_paths}${r.distinct_paths >= PATH_CAP ? "+" : ""}` : "-"}
                </td>
                <td className="px-3 py-2 text-foreground-muted">{r.first_seen}</td>
                <td className="px-3 py-2 text-foreground-muted">{r.last_seen}</td>
                <td className="px-4 py-2 text-right">
                  <RowActions host={r.host} isOwn={r.is_own} isBanned={r.is_banned} busy={busy === r.host} onAction={onAction} />
                </td>
              </tr>
              {expanded && (
                <tr className="bg-background-subtle">
                  <td />
                  <td colSpan={8} className="px-2 py-3">
                    <div className="mb-2 flex flex-wrap gap-x-6 gap-y-1 text-xs text-foreground-muted">
                      <span>首次出现以来 {r.all_requests.toLocaleString()} 次 · {formatBytes(r.all_bytes)}</span>
                      {r.redirected > 0 && <span>30 天内 302 分流 {r.redirected.toLocaleString()} 次</span>}
                      {r.errors > 0 && <span>30 天内错误 {r.errors.toLocaleString()} 次</span>}
                    </div>
                    <PathList paths={r.top_paths} note="30 天内被引用最多的路径 (各节点每天前 10 合并)" />
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
