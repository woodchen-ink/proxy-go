"use client"

import type { PathHit } from "./types"

// PathList 被引用最多的路径; 点击在新窗口打开 (经引用方同一站点访问, 便于确认是什么资源)
export default function PathList({ paths, note }: { paths: PathHit[]; note?: string }) {
  if (paths.length === 0) {
    return <div className="text-xs text-foreground-subtle">暂无路径明细 (节点升级后开始记录)</div>
  }
  return (
    <div className="space-y-1">
      {note && <div className="text-xs text-foreground-subtle">{note}</div>}
      <ol className="space-y-0.5 text-xs">
        {paths.map((h) => (
          <li key={h.p} className="flex items-baseline gap-3">
            <span className="w-14 shrink-0 text-right tabular-nums text-foreground-muted">{h.n.toLocaleString()}</span>
            <span className="min-w-0 break-all font-mono">{h.p}</span>
          </li>
        ))}
      </ol>
    </div>
  )
}
