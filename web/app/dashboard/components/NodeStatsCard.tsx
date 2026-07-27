"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { fetchApi } from "@/lib/api-client"

// NodeSummary 单节点累计概览
// is_current 那份来自本节点内存 (实时), 其余来自 D1 (reported_at 为其上报时刻)
interface NodeSummary {
  node_id: string
  is_current: boolean
  request_count: number
  error_count: number
  error_rate: number
  bytes_transferred: number
  bytes_saved: number
  cache_hit_rate: number
  avg_latency: string
  reported_at: number
}

// 与 PathStatsList 保持同一套节点分类色, 让两处的同一节点颜色一致
const NODE_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
  "hsl(var(--chart-6))",
]

function formatBytes(bytes: number): string {
  if (!bytes) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${Math.round((bytes / Math.pow(k, i)) * 100) / 100} ${sizes[i]}`
}

// formatAge 把上报时刻转成"多久之前", 让数据新鲜度一眼可见
function formatAge(sec: number): string {
  if (!sec) return "—"
  const diff = Math.floor(Date.now() / 1000) - sec
  if (diff < 0) return "刚刚"
  if (diff < 60) return `${diff} 秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}

/**
 * 各节点累计概览
 *
 * 与合计视图互补: 合计回答"整体如何", 这里回答"每个节点各自如何、流量有没有偏斜"。
 * 单节点部署时整卡不渲染 —— 只有一行等于把合计重复一遍。
 */
export default function NodeStatsCard() {
  const [nodes, setNodes] = useState<NodeSummary[]>([])
  const [loaded, setLoaded] = useState(false)

  const fetchNodes = useCallback(async () => {
    try {
      const resp = await fetchApi("/admin/api/node-stats")
      if (!resp.ok) return
      const data = await resp.json()
      setNodes(Array.isArray(data.nodes) ? data.nodes : [])
    } catch {
      // 失败静默: 这是补充视图, 不该盖掉主面板的错误提示
    } finally {
      setLoaded(true)
    }
  }, [])

  useEffect(() => {
    fetchNodes()
    // 其他节点的数据后端有 5 分钟缓存, 前端 60 秒轮询已足够跟上
    const timer = setInterval(fetchNodes, 60_000)
    return () => clearInterval(timer)
  }, [fetchNodes])

  const colorOf = useMemo(() => {
    const ordered = [...nodes.map((n) => n.node_id)].sort()
    const map = new Map(ordered.map((id, i) => [id, NODE_COLORS[i % NODE_COLORS.length]]))
    return (id: string) => map.get(id) ?? "hsl(var(--muted-foreground))"
  }, [nodes])

  const totals = useMemo(() => {
    const acc = nodes.reduce(
      (a, n) => {
        a.req += n.request_count
        a.err += n.error_count
        a.bytes += n.bytes_transferred
        a.saved += n.bytes_saved
        return a
      },
      { req: 0, err: 0, bytes: 0, saved: 0 }
    )
    return { ...acc, errRate: acc.req > 0 ? acc.err / acc.req : 0 }
  }, [nodes])

  // 单节点没有对比价值, 不占版面
  if (!loaded || nodes.length < 2) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          节点
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            ({nodes.length} 个, 累计)
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>节点</TableHead>
                <TableHead className="text-right">请求</TableHead>
                <TableHead className="text-right">占比</TableHead>
                <TableHead className="text-right">错误率</TableHead>
                <TableHead className="text-right">流量</TableHead>
                <TableHead className="text-right">节省</TableHead>
                <TableHead className="text-right">命中率</TableHead>
                <TableHead className="text-right">平均延迟</TableHead>
                <TableHead className="text-right">数据时点</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {nodes.map((n) => (
                <TableRow key={n.node_id}>
                  <TableCell className="font-mono text-xs font-medium">
                    <span
                      aria-hidden
                      className="mr-1.5 inline-block size-2 rounded-full align-middle"
                      style={{ background: colorOf(n.node_id) }}
                    />
                    {n.node_id}
                    {n.is_current && (
                      <span className="ml-1.5 text-[10px] font-normal text-muted-foreground">
                        本节点
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="text-right tabular-nums font-medium">
                    {n.request_count.toLocaleString()}
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-xs text-muted-foreground">
                    {totals.req > 0
                      ? `${((n.request_count / totals.req) * 100).toFixed(1)}%`
                      : "—"}
                  </TableCell>
                  <TableCell
                    className="text-right tabular-nums text-xs"
                    style={{
                      color:
                        n.error_rate > 0.05
                          ? "hsl(var(--destructive))"
                          : "hsl(var(--success))",
                    }}
                  >
                    {(n.error_rate * 100).toFixed(2)}%
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-xs">
                    {formatBytes(n.bytes_transferred)}
                  </TableCell>
                  <TableCell
                    className="text-right tabular-nums text-xs"
                    style={{ color: "hsl(var(--success))" }}
                  >
                    {formatBytes(n.bytes_saved)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-xs">
                    {(n.cache_hit_rate * 100).toFixed(1)}%
                  </TableCell>
                  <TableCell className="text-right tabular-nums text-xs text-muted-foreground">
                    {n.avg_latency}
                  </TableCell>
                  <TableCell className="text-right text-xs text-muted-foreground">
                    {n.is_current ? "实时" : formatAge(n.reported_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
            <TableFooter>
              <TableRow>
                <TableCell className="font-medium">合计</TableCell>
                <TableCell className="text-right tabular-nums font-medium">
                  {totals.req.toLocaleString()}
                </TableCell>
                <TableCell />
                <TableCell
                  className="text-right tabular-nums text-xs"
                  style={{
                    color:
                      totals.errRate > 0.05
                        ? "hsl(var(--destructive))"
                        : "hsl(var(--success))",
                  }}
                >
                  {(totals.errRate * 100).toFixed(2)}%
                </TableCell>
                <TableCell className="text-right tabular-nums text-xs">
                  {formatBytes(totals.bytes)}
                </TableCell>
                <TableCell
                  className="text-right tabular-nums text-xs"
                  style={{ color: "hsl(var(--success))" }}
                >
                  {formatBytes(totals.saved)}
                </TableCell>
                <TableCell colSpan={3} />
              </TableRow>
            </TableFooter>
          </Table>
        </div>
      </CardContent>
    </Card>
  )
}
