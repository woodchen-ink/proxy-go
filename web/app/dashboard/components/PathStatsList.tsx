"use client"

import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableFooter,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ChevronRight, RotateCcw } from "lucide-react"
import { useToast } from "@/components/ui/use-toast"
import { fetchApi } from "@/lib/api-client"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"

// PathNodeStat 单个路径在单个节点上的计数
// is_current 为 true 的那份是实时值, 其余最多滞后一个上报周期
interface PathNodeStat {
  node_id: string
  is_current: boolean
  request_count: number
  error_count: number
  error_rate: number
  bytes_transferred: number
  cache_hits: number
  cache_misses: number
  cache_hit_rate: number
  avg_latency: string
}

interface PathStats {
  path: string
  request_count: number
  error_count: number
  bytes_transferred: number
  avg_latency: string
  last_access_time: number
  status_2xx: number
  status_3xx: number
  status_4xx: number
  status_5xx: number
  cache_hits: number
  cache_misses: number
  cache_hit_rate: number
  bytes_saved: number
  nodes: PathNodeStat[]
}

// 节点分类色: 用图表分类色板而非语义色 —— 节点之间没有好坏之分, 只是不同类别
const NODE_COLORS = [
  "hsl(var(--chart-1))",
  "hsl(var(--chart-2))",
  "hsl(var(--chart-3))",
  "hsl(var(--chart-4))",
  "hsl(var(--chart-5))",
  "hsl(var(--chart-6))",
]

// 状态码分段 → 语义色 (成功/重定向中性/客户端警告/服务端错误), 用于逐行迷你堆叠条
const STATUS_SEGMENTS = [
  { key: "status_2xx", label: "2xx", color: "hsl(var(--success))" },
  { key: "status_3xx", label: "3xx", color: "hsl(var(--muted-foreground))" },
  { key: "status_4xx", label: "4xx", color: "hsl(var(--warning))" },
  { key: "status_5xx", label: "5xx", color: "hsl(var(--destructive))" },
] as const

function formatBytes(bytes: number): string {
  if (!bytes) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${Math.round((bytes / Math.pow(k, i)) * 100) / 100} ${sizes[i]}`
}

// hitColor / errColor 命中率与错误率的分档语义色 (与旧 badge 三档一致)
function hitColor(rate: number): string {
  if (rate > 70) return "hsl(var(--success))"
  if (rate > 40) return "hsl(var(--warning))"
  return "hsl(var(--destructive))"
}

// StatusDistBar 单行状态码分布迷你堆叠条; 段宽按各区间占比, title 悬浮给出精确计数
function StatusDistBar({ s }: { s: PathStats }) {
  const segs = STATUS_SEGMENTS.map((seg) => ({ ...seg, count: s[seg.key] }))
  const total = segs.reduce((sum, seg) => sum + seg.count, 0)
  if (total === 0) return <span className="text-xs text-muted-foreground">—</span>
  const title = segs
    .filter((seg) => seg.count > 0)
    .map((seg) => `${seg.label}: ${seg.count.toLocaleString()}`)
    .join("   ")
  return (
    <div
      className="flex h-2 w-full min-w-[88px] overflow-hidden rounded-full bg-muted"
      title={title}
    >
      {segs.map(
        (seg) =>
          seg.count > 0 && (
            <div
              key={seg.key}
              style={{ width: `${(seg.count / total) * 100}%`, background: seg.color }}
            />
          )
      )}
    </div>
  )
}

// NodeSplitBar 该路径的请求量在各节点间的占比迷你堆叠条
//
// 常驻显示而非藏在展开里: 多节点部署最常见的问题就是"流量没按预期分流",
// 合计数字完全看不出偏斜, 必须让占比在列表上一眼可见
function NodeSplitBar({
  nodes,
  colorOf,
}: {
  nodes: PathNodeStat[]
  colorOf: (nodeID: string) => string
}) {
  const active = nodes.filter((n) => n.request_count > 0)
  const total = active.reduce((sum, n) => sum + n.request_count, 0)
  if (total === 0) return <span className="text-xs text-muted-foreground">—</span>

  const title = active
    .map(
      (n) =>
        `${n.node_id}: ${n.request_count.toLocaleString()} (${((n.request_count / total) * 100).toFixed(1)}%)`
    )
    .join("   ")

  return (
    <div
      className="flex h-2 w-full min-w-[72px] overflow-hidden rounded-full bg-muted"
      title={title}
    >
      {active.map((n) => (
        <div
          key={n.node_id}
          style={{
            width: `${(n.request_count / total) * 100}%`,
            background: colorOf(n.node_id),
          }}
        />
      ))}
    </div>
  )
}

// CacheMeter 缓存命中率迷你进度条 + 百分比, 颜色随命中率分档
function CacheMeter({ rate, has }: { rate: number; has: boolean }) {
  const color = hitColor(rate)
  return (
    <div className="flex items-center justify-end gap-2">
      <div className="h-1.5 w-12 overflow-hidden rounded-full bg-muted">
        <div className="h-full" style={{ width: `${rate}%`, background: color }} />
      </div>
      <span className="tabular-nums text-xs" style={{ color }}>
        {has ? rate.toFixed(1) : "0.0"}%
      </span>
    </div>
  )
}

// PathStatsList 首页逐路径累计统计: 从 /admin/api/path-stats 拉取按前缀聚合的统计,
// 以紧凑表格逐路径展示 (状态分布迷你堆叠条 + 缓存命中率迷你条 + 数值列), 尾行给出合计。
// 支持单路径重置与一键重置全部, 复用后端 /admin/api/path-stats/reset[-all]。
export default function PathStatsList() {
  const [stats, setStats] = useState<PathStats[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const failureCount = useRef(0)
  const lastErrorAt = useRef(0)
  const { toast } = useToast()

  const fetchStats = useCallback(async (): Promise<boolean> => {
    try {
      const resp = await fetchApi("/admin/api/path-stats")
      if (!resp.ok) throw new Error("加载路径统计失败")
      const json = (await resp.json()) as { path_stats: PathStats[] }
      setStats(json.path_stats || [])
      setError(null)
      return true
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载路径统计失败"
      setError(msg)
      const now = Date.now()
      if (now - lastErrorAt.current > 30_000) {
        lastErrorAt.current = now
        toast({ title: "错误", description: msg, variant: "destructive" })
      }
      return false
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const tick = async () => {
      if (cancelled) return
      const ok = await fetchStats()
      if (cancelled) return
      if (ok) failureCount.current = 0
      else failureCount.current = Math.min(failureCount.current + 1, 5)
      const delay = ok ? 30_000 : Math.min(1000 * 2 ** failureCount.current, 60_000)
      timer = setTimeout(tick, delay)
    }
    tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [fetchStats])

  // resetPath 重置单个路径统计, 成功后立即刷新; 失败 toast
  const resetPath = useCallback(
    async (path: string) => {
      try {
        const resp = await fetchApi("/admin/api/path-stats/reset", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ path }),
        })
        if (!resp.ok) throw new Error("重置统计失败")
        toast({ title: "成功", description: `已重置 ${path} 的统计` })
        await fetchStats()
      } catch (e) {
        toast({
          title: "错误",
          description: e instanceof Error ? e.message : "重置统计失败",
          variant: "destructive",
        })
      }
    },
    [toast, fetchStats]
  )

  // resetAll 一键重置全部路径统计
  const resetAll = useCallback(async () => {
    try {
      const resp = await fetchApi("/admin/api/path-stats/reset-all", { method: "POST" })
      if (!resp.ok) throw new Error("重置失败")
      toast({ title: "成功", description: "所有路径统计已重置" })
      await fetchStats()
    } catch (e) {
      toast({
        title: "错误",
        description: e instanceof Error ? e.message : "重置失败",
        variant: "destructive",
      })
    }
  }, [toast, fetchStats])

  // 只展示有请求量的路径, 按请求数降序
  const visible = useMemo(
    () =>
      [...stats].filter((s) => s.request_count > 0).sort((a, b) => b.request_count - a.request_count),
    [stats]
  )

  // 节点 → 颜色: 按节点 id 排序后固定分配, 保证同一节点在所有行里颜色一致,
  // 且不随请求量排序变化而跳色
  const colorOf = useMemo(() => {
    const ids = new Set<string>()
    stats.forEach((s) => (s.nodes || []).forEach((n) => ids.add(n.node_id)))
    const ordered = [...ids].sort()
    const map = new Map(ordered.map((id, i) => [id, NODE_COLORS[i % NODE_COLORS.length]]))
    return (nodeID: string) => map.get(nodeID) ?? "hsl(var(--muted-foreground))"
  }, [stats])

  // 是否为多节点部署: 单节点时整列都是 100%, 没有展示价值, 直接不渲染该列
  const isMultiNode = useMemo(() => {
    const ids = new Set<string>()
    stats.forEach((s) => (s.nodes || []).forEach((n) => ids.add(n.node_id)))
    return ids.size > 1
  }, [stats])

  const [expanded, setExpanded] = useState<string | null>(null)

  // 合计行: 请求 / 错误 / 流量 / 节省累加, 命中率按命中数与缓存请求数重新求比
  const totals = useMemo(() => {
    const acc = visible.reduce(
      (a, s) => {
        a.req += s.request_count
        a.err += s.error_count
        a.bytes += s.bytes_transferred
        a.saved += s.bytes_saved
        a.hits += s.cache_hits
        a.cacheReq += s.cache_hits + s.cache_misses
        return a
      },
      { req: 0, err: 0, bytes: 0, saved: 0, hits: 0, cacheReq: 0 }
    )
    return {
      ...acc,
      hitRate: acc.cacheReq > 0 ? (acc.hits / acc.cacheReq) * 100 : 0,
      errRate: acc.req > 0 ? (acc.err / acc.req) * 100 : 0,
    }
  }, [visible])

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>
          路径统计
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            (累计, {visible.length} 条)
          </span>
        </CardTitle>
        {visible.length > 0 && (
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="ghost" size="sm" className="h-7 px-2 text-muted-foreground">
                <RotateCcw className="h-3.5 w-3.5" />
                <span className="ml-1.5 text-xs">重置全部</span>
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>重置所有路径统计?</AlertDialogTitle>
                <AlertDialogDescription>
                  将清零所有路径的累计请求、错误、状态码与缓存统计, 此操作无法撤销。
                  多节点部署时只影响当前节点, 其他节点的累计值需分别登录各自节点重置。
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>取消</AlertDialogCancel>
                <AlertDialogAction onClick={resetAll}>确认重置</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </CardHeader>
      <CardContent>
        {loading && stats.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-muted-foreground">加载中...</div>
        ) : error && stats.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-destructive">{error}</div>
        ) : visible.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-muted-foreground">
            暂无统计数据
          </div>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>路径</TableHead>
                  <TableHead className="text-right">总请求</TableHead>
                  {isMultiNode && <TableHead className="min-w-[80px]">节点分布</TableHead>}
                  <TableHead className="min-w-[120px]">状态分布</TableHead>
                  <TableHead className="text-right">缓存命中率</TableHead>
                  <TableHead className="text-right">错误率</TableHead>
                  <TableHead className="text-right">流量</TableHead>
                  <TableHead className="text-right">节省</TableHead>
                  <TableHead className="text-right">平均延迟</TableHead>
                  <TableHead className="w-8" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map((s) => {
                  const errRate = s.request_count > 0 ? (s.error_count / s.request_count) * 100 : 0
                  const cacheReq = s.cache_hits + s.cache_misses
                  const nodes = s.nodes || []
                  const isOpen = expanded === s.path
                  return (
                    <Fragment key={s.path}>
                    <TableRow
                      className={`group ${isMultiNode ? "cursor-pointer" : ""}`}
                      onClick={
                        isMultiNode ? () => setExpanded(isOpen ? null : s.path) : undefined
                      }
                    >
                      <TableCell
                        className="max-w-[180px] truncate font-mono text-xs font-medium"
                        title={s.path}
                      >
                        {isMultiNode && (
                          <ChevronRight
                            aria-hidden
                            className={`mr-1 inline h-3 w-3 shrink-0 text-muted-foreground transition-transform ${
                              isOpen ? "rotate-90" : ""
                            }`}
                          />
                        )}
                        {s.path}
                      </TableCell>
                      <TableCell className="text-right tabular-nums font-medium">
                        {s.request_count.toLocaleString()}
                      </TableCell>
                      {isMultiNode && (
                        <TableCell>
                          <NodeSplitBar nodes={nodes} colorOf={colorOf} />
                        </TableCell>
                      )}
                      <TableCell>
                        <StatusDistBar s={s} />
                      </TableCell>
                      <TableCell>
                        <CacheMeter rate={s.cache_hit_rate * 100} has={cacheReq > 0} />
                      </TableCell>
                      <TableCell className="text-right">
                        <span
                          className="tabular-nums text-xs"
                          style={{
                            color: errRate > 5 ? "hsl(var(--destructive))" : "hsl(var(--success))",
                          }}
                        >
                          {errRate.toFixed(2)}%
                        </span>
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-xs">
                        {formatBytes(s.bytes_transferred)}
                      </TableCell>
                      <TableCell
                        className="text-right tabular-nums text-xs"
                        style={{ color: "hsl(var(--success))" }}
                      >
                        {formatBytes(s.bytes_saved)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-xs text-muted-foreground">
                        {s.avg_latency}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(e) => {
                            e.stopPropagation()
                            resetPath(s.path)
                          }}
                          aria-label={`重置 ${s.path} 统计`}
                          className="h-7 w-7 p-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 [@media(pointer:coarse)]:opacity-100"
                        >
                          <RotateCcw className="h-3.5 w-3.5" />
                        </Button>
                      </TableCell>
                    </TableRow>

                    {/* 展开后逐节点列出精确数字; 迷你条只给占比, 这里给可读的原始值 */}
                    {isMultiNode &&
                      isOpen &&
                      nodes.map((n) => (
                        <TableRow key={`${s.path}::${n.node_id}`} className="bg-muted/30">
                          <TableCell className="pl-8 font-mono text-xs text-muted-foreground">
                            <span
                              aria-hidden
                              className="mr-1.5 inline-block size-2 rounded-full align-middle"
                              style={{ background: colorOf(n.node_id) }}
                            />
                            {n.node_id}
                            {n.is_current && (
                              <span className="ml-1.5 text-[10px] text-muted-foreground">
                                (本节点·实时)
                              </span>
                            )}
                          </TableCell>
                          <TableCell className="text-right tabular-nums text-xs">
                            {n.request_count.toLocaleString()}
                          </TableCell>
                          <TableCell className="text-right tabular-nums text-xs text-muted-foreground">
                            {s.request_count > 0
                              ? `${((n.request_count / s.request_count) * 100).toFixed(1)}%`
                              : "—"}
                          </TableCell>
                          <TableCell />
                          <TableCell className="text-right tabular-nums text-xs">
                            {(n.cache_hit_rate * 100).toFixed(1)}%
                          </TableCell>
                          <TableCell className="text-right tabular-nums text-xs">
                            {(n.error_rate * 100).toFixed(2)}%
                          </TableCell>
                          <TableCell className="text-right tabular-nums text-xs">
                            {formatBytes(n.bytes_transferred)}
                          </TableCell>
                          <TableCell />
                          <TableCell className="text-right tabular-nums text-xs text-muted-foreground">
                            {n.avg_latency}
                          </TableCell>
                          <TableCell />
                        </TableRow>
                      ))}
                    </Fragment>
                  )
                })}
              </TableBody>
              <TableFooter>
                <TableRow>
                  <TableCell className="font-medium">合计</TableCell>
                  <TableCell className="text-right tabular-nums font-medium">
                    {totals.req.toLocaleString()}
                  </TableCell>
                  {/* 节点分布列在合计行留空: 各路径占比不同, 汇总成一根条会误导 */}
                  {isMultiNode && <TableCell />}
                  <TableCell />
                  <TableCell className="text-right tabular-nums text-xs" style={{ color: hitColor(totals.hitRate) }}>
                    {totals.hitRate.toFixed(1)}%
                  </TableCell>
                  <TableCell
                    className="text-right tabular-nums text-xs"
                    style={{
                      color: totals.errRate > 5 ? "hsl(var(--destructive))" : "hsl(var(--success))",
                    }}
                  >
                    {totals.errRate.toFixed(2)}%
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
                  <TableCell colSpan={2} />
                </TableRow>
              </TableFooter>
            </Table>
            <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <span>状态分布:</span>
              {STATUS_SEGMENTS.map((seg) => (
                <span key={seg.key} className="flex items-center gap-1.5">
                  <span
                    className="inline-block h-2 w-2 rounded-full"
                    style={{ background: seg.color }}
                  />
                  {seg.label}
                </span>
              ))}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

export type { PathStats }
