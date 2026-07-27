"use client"

import type { ClusterInfo } from "../page"

/**
 * 标明累计类指标的聚合范围
 *
 * 多节点部署时 DNS 会把管理员分到任意一个节点, 这行说明当前页面的数字覆盖了哪些节点,
 * 以及哪些指标只属于当前这一个节点, 避免把单节点的 QPS / 内存误读成全局值。
 */
export default function ClusterBadge({ cluster }: { cluster?: ClusterInfo }) {
  if (!cluster || !cluster.node_id) return null

  const nodes = cluster.nodes || []
  const isMultiNode = nodes.length > 1

  // 单节点且链路正常时没有歧义, 不占版面
  if (!isMultiNode && !cluster.degraded) return null

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
      {cluster.degraded ? (
        <span className="inline-flex items-center gap-1.5 font-medium text-[hsl(var(--warning))]">
          <span
            aria-hidden
            className="size-1.5 rounded-full bg-[hsl(var(--warning))]"
          />
          仅当前节点数据
        </span>
      ) : (
        <span className="inline-flex items-center gap-1.5 font-medium text-foreground">
          <span
            aria-hidden
            className="size-1.5 rounded-full bg-[hsl(var(--success))]"
          />
          全 {nodes.length} 节点合计
        </span>
      )}

      <span aria-hidden>·</span>
      <span>
        本页来自 <span className="font-mono">{cluster.node_id}</span>
      </span>

      {isMultiNode && !cluster.degraded && (
        <>
          <span aria-hidden>·</span>
          <span className="font-mono">{nodes.join(" / ")}</span>
        </>
      )}

      <span aria-hidden>·</span>
      <span>
        {cluster.degraded
          ? "其他节点数据暂时拉取失败"
          : "QPS / 带宽 / 内存 / 最近请求仅为当前节点"}
      </span>
    </div>
  )
}
