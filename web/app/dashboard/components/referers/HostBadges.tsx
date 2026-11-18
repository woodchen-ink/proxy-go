"use client"

const base = "inline-flex shrink-0 items-center rounded px-1.5 py-px text-[11px] font-medium leading-4"

export default function HostBadges({ isNew, isOwn, isBanned, wholeSite }: { isNew?: boolean; isOwn?: boolean; isBanned?: boolean; wholeSite?: boolean }) {
  return (
    <>
      {isNew && <span className={`${base} bg-warning-soft text-warning`}>新</span>}
      {isBanned && <span className={`${base} bg-destructive-soft text-destructive`}>已拉黑</span>}
      {wholeSite && !isOwn && <span className={`${base} bg-destructive-soft text-destructive`} title="单日引用不同路径达到记录上限, 疑似镜像或整站引用">整站引用</span>}
      {isOwn && <span className={`${base} bg-muted text-foreground-muted`}>自有</span>}
    </>
  )
}
