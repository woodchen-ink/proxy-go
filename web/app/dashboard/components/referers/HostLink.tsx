"use client"

import { ExternalLink } from "lucide-react"

// HostLink 新标签页打开来源站点首页; noreferrer 避免把后台地址作为 Referer 发给对方
export default function HostLink({ host }: { host: string }) {
  return (
    <a
      href={`https://${host}/`}
      target="_blank"
      rel="noopener noreferrer"
      title={`打开 ${host}`}
      onClick={(e) => e.stopPropagation()}
      className="shrink-0 rounded p-0.5 text-foreground-subtle hover:bg-accent hover:text-link"
    >
      <ExternalLink className="h-3.5 w-3.5" />
    </a>
  )
}
