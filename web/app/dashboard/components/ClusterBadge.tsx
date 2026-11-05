"use client"

import type { ClusterInfo } from "../page"

/**
 * 标明指标的聚合范围
 *
 * 中控汇总全部节点: 在线节点用实时快照, 离线节点用最后一次上报的累计值;
 * QPS / 带宽 / 内存 / 最近请求这类瞬时量只有在线节点才有。
 */
export default function ClusterBadge({ cluster }: { cluster?: ClusterInfo }) {
  if (!cluster) return null

  const nodes = cluster.nodes || []
  const live = cluster.live_nodes || []
  if (nodes.length <= 1 && live.length === nodes.length) return null
  const offline = nodes.length - live.length

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
      <span className="inline-flex items-center gap-1.5 font-medium text-foreground">
        <span aria-hidden className="size-1.5 rounded-full bg-[hsl(var(--success))]" />
        全 {nodes.length} 节点合计
      </span>
      <span aria-hidden>·</span>
      <span>
        {live.length} 个在线 (实时)
        {offline > 0 && (
          <span className="text-[hsl(var(--warning))]"> · {offline} 个离线 (最后上报值)</span>
        )}
      </span>
      <span aria-hidden>·</span>
      <span>QPS / 带宽 / 内存 / 最近请求仅含在线节点</span>
    </div>
  )
}
