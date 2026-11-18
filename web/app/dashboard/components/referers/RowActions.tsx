"use client"

import type { HostAction } from "./types"

const btn = "rounded px-1.5 py-0.5 text-xs hover:bg-accent disabled:opacity-40"

// RowActions 拉黑 (全局 Referer 黑名单, 全节点生效) / 标记自有
export default function RowActions({ host, isOwn, isBanned, busy, onAction }: { host: string; isOwn: boolean; isBanned: boolean; busy: boolean; onAction: (a: HostAction) => void }) {
  return (
    <div className="inline-flex items-center gap-1 whitespace-nowrap">
      {!isOwn && (
        <button
          type="button"
          disabled={busy}
          className={`${btn} ${isBanned ? "text-foreground-muted" : "text-destructive"}`}
          onClick={() => onAction({ kind: "ban", host, banned: !isBanned })}
        >
          {isBanned ? "取消拉黑" : "拉黑"}
        </button>
      )}
      <button type="button" disabled={busy} className={`${btn} text-foreground-muted`} onClick={() => onAction({ kind: "own", host, own: !isOwn })}>
        {isOwn ? "取消自有" : "标为自有"}
      </button>
    </div>
  )
}
