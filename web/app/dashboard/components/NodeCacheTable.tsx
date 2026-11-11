"use client"

import { Card } from "@/components/ui/card"
import { formatBytes, formatCount, formatPct } from "@/lib/format"
import { cn } from "@/lib/utils"

interface CacheLite {
  total_items: number
  disk_file_count: number
  disk_size: number
  hit_count: number
  miss_count: number
  bytes_saved: number
  enabled: boolean
}

export interface NodeCacheRow {
  node_id: string
  proxy: CacheLite | null
  mirror: CacheLite | null
}

const n = (v: number | undefined) => Number(v) || 0

// NodeCacheTable 各在线节点自己的缓存规模与命中 (代理 + 镜像合计, 括号内为代理 / 镜像分项)
export default function NodeCacheTable({ rows }: { rows: NodeCacheRow[] }) {
  const sorted = [...rows].sort((a, b) => a.node_id.localeCompare(b.node_id))
  return (
    <Card className="overflow-hidden">
      <div className="border-b px-5 py-3">
        <h2 className="text-base font-semibold">各节点缓存</h2>
        <p className="text-sm text-foreground-muted">每个节点独立缓存, 数据来自在线节点的实时快照; 离线节点不在此列</p>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[52rem] text-sm">
          <thead className="bg-muted text-foreground-muted">
            <tr className="border-b">
              <th className="px-5 py-2 text-left font-medium">节点</th>
              <th className="px-3 py-2 text-left font-medium">缓存开关</th>
              <th className="px-3 py-2 text-right font-medium">缓存条数</th>
              <th className="px-3 py-2 text-right font-medium">磁盘文件</th>
              <th className="px-3 py-2 text-right font-medium">磁盘占用</th>
              <th className="px-3 py-2 text-right font-medium">命中率</th>
              <th className="px-3 py-2 text-right font-medium">命中 / 未命中</th>
              <th className="px-5 py-2 text-right font-medium">节省带宽</th>
            </tr>
          </thead>
          <tbody className="divide-y tabular-nums">
            {sorted.length === 0 && (
              <tr>
                <td colSpan={8} className="px-5 py-8 text-center text-foreground-muted">
                  没有在线节点
                </td>
              </tr>
            )}
            {sorted.map((r) => {
              const p = r.proxy
              const m = r.mirror
              const hits = n(p?.hit_count) + n(m?.hit_count)
              const misses = n(p?.miss_count) + n(m?.miss_count)
              const rate = hits + misses > 0 ? hits / (hits + misses) : 0
              const off = [p && !p.enabled ? "代理" : "", m && !m.enabled ? "镜像" : ""].filter(Boolean)
              return (
                <tr key={r.node_id} className="hover:bg-accent">
                  <td className="px-5 py-2.5 font-medium">{r.node_id}</td>
                  <td className="px-3 py-2.5">
                    {!p && !m ? (
                      <span className="text-foreground-subtle">未上报</span>
                    ) : off.length ? (
                      <span className="rounded-md bg-warning-soft px-1.5 py-0.5 text-xs text-warning">{off.join(" / ")}已关闭</span>
                    ) : (
                      <span className="text-foreground-muted">开启</span>
                    )}
                  </td>
                  <td className="px-3 py-2.5 text-right text-foreground-muted" title={`代理 ${n(p?.total_items)} / 镜像 ${n(m?.total_items)}`}>
                    {formatCount(n(p?.total_items) + n(m?.total_items))}
                  </td>
                  <td className="px-3 py-2.5 text-right text-foreground-muted">{formatCount(n(p?.disk_file_count) + n(m?.disk_file_count))}</td>
                  <td className="px-3 py-2.5 text-right font-medium" title={`代理 ${formatBytes(n(p?.disk_size))} / 镜像 ${formatBytes(n(m?.disk_size))}`}>
                    {formatBytes(n(p?.disk_size) + n(m?.disk_size))}
                  </td>
                  <td className={cn("px-3 py-2.5 text-right", hits + misses > 0 ? "" : "text-foreground-subtle")}>{formatPct(rate)}</td>
                  <td className="px-3 py-2.5 text-right text-foreground-muted">
                    {formatCount(hits)} / {formatCount(misses)}
                  </td>
                  <td className="px-5 py-2.5 text-right text-foreground-muted">{formatBytes(n(p?.bytes_saved) + n(m?.bytes_saved))}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Card>
  )
}
