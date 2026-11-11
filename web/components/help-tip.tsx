"use client"

import { useState, type ReactNode } from "react"
import { CircleHelp } from "lucide-react"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"

// HelpTip 字段说明收进问号图标: 桌面悬停显示, 触屏点一下显示 (不能只靠 hover)
export function HelpTip({ children, label = "说明" }: { children: ReactNode; label?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <TooltipProvider delayDuration={150}>
      <Tooltip open={open} onOpenChange={setOpen}>
        <TooltipTrigger asChild>
          <button
            type="button"
            aria-label={label}
            onClick={(e) => {
              e.preventDefault()
              setOpen((v) => !v)
            }}
            className="relative inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-foreground-subtle transition-colors before:absolute before:-inset-2 hover:text-foreground"
          >
            <CircleHelp className="h-4 w-4" />
          </button>
        </TooltipTrigger>
        <TooltipContent
          side="top"
          className="max-w-xs border bg-popover px-3 py-2 text-sm leading-relaxed text-popover-foreground shadow-popover"
        >
          {children}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
