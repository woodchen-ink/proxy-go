"use client"

import { Plus } from "lucide-react"
import { cn } from "@/lib/utils"
import { type PathMapping, toMapping } from "../types"

interface PathListProps {
  map: Record<string, PathMapping | string>
  selected: string | null
  adding: boolean
  disabled: boolean
  onSelect: (path: string) => void
  onAdd: () => void
}

// PathList 路径映射左栏: chrome 区紧凑档, 当前项品牌浅底, 新增入口挂在分组标题行尾
export default function PathList({ map, selected, adding, disabled, onSelect, onAdd }: PathListProps) {
  const paths = Object.keys(map)
  return (
    <nav aria-label="路径列表" className="flex min-h-0 flex-col">
      <div className="flex h-10 shrink-0 items-center justify-between border-b px-3">
        <span className="text-sm font-medium">
          路径 <span className="font-normal text-foreground-subtle tabular-nums">{paths.length}</span>
        </span>
        <button
          type="button"
          onClick={onAdd}
          disabled={disabled}
          aria-label="新增路径"
          className={cn(
            "inline-flex h-7 items-center gap-1 rounded-md px-2 text-sm text-foreground-muted transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50",
            adding && "bg-brand-soft text-brand-strong hover:bg-brand-soft hover:text-brand-strong",
          )}
        >
          <Plus className="h-4 w-4" />
          新增
        </button>
      </div>
      <ul className="min-h-0 flex-1 space-y-0.5 overflow-y-auto overscroll-contain p-2">
        {paths.map((path) => {
          const m = toMapping(map[path])
          const enabled = m.Enabled !== false
          const active = !adding && selected === path
          return (
            <li key={path}>
              <button
                type="button"
                onClick={() => onSelect(path)}
                aria-current={active ? "true" : undefined}
                className={cn(
                  "flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm transition-colors",
                  active ? "bg-brand-soft font-medium text-brand-strong" : "hover:bg-accent",
                  !enabled && !active && "text-foreground-subtle",
                )}
              >
                <span
                  aria-hidden
                  className={cn("h-1.5 w-1.5 shrink-0 rounded-full", enabled ? "bg-success" : "bg-foreground-subtle/50")}
                />
                <span className="truncate font-mono">{path}</span>
                {!enabled && <span className="ml-auto shrink-0 text-xs">已停用</span>}
              </button>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
