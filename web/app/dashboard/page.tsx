"use client"

import { useCallback, useMemo, useRef, useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"
import { usePolling } from "@/hooks/use-polling"
import { fetchApi } from "@/lib/api-client"
import MetricTiles from "./components/MetricTiles"
import StatusCodeChart from "./components/StatusCodeChart"
import LatencyChart from "./components/LatencyChart"
import PathTrendChart from "./components/PathTrendChart"
import PathTotalsChart from "./components/PathTotalsChart"
import PathStatsList from "./components/PathStatsList"
import RefererDailyChart from "./components/RefererDailyChart"
import CacheStatsTiles from "./components/CacheStatsTiles"
import ClusterBadge from "./components/ClusterBadge"
import NodeCompareTable from "./components/NodeCompareTable"
import NodeTrafficPanel from "./components/NodeTrafficPanel"
import NodeDetailDialog from "./components/NodeDetailDialog"
import RecentRequests, { type RequestLogRow } from "./components/RecentRequests"
import TopReferers, { type TopReferer } from "./components/TopReferers"
import { nodeColor, type NodesOverview } from "./node-types"

interface Metrics {
  uptime: string
  active_requests: number
  total_requests: number
  total_errors: number
  num_goroutine: number
  memory_usage: string
  avg_response_time: string
  requests_per_second: number
  bytes_per_second: number
  error_rate: number
  status_code_stats: Record<string, number>
  recent_requests: RequestLogRow[]
  latency_stats: {
    min: string
    max: string
    distribution: Record<string, number>
  }
  current_bandwidth: string
  total_bytes: number
  current_session_requests: number
  top_referers: TopReferer[]
  cluster?: ClusterInfo
}

// ClusterInfo 说明指标覆盖了哪些节点
// 中控汇总口径: 在线节点 (live_nodes) 为实时快照, 离线节点为其最后一次上报的累计值;
// QPS / 带宽 / 内存 / 最近请求只能来自在线节点
export interface ClusterInfo {
  node_id: string
  nodes: string[]
  live_nodes?: string[]
  hub?: boolean
  aggregated: boolean
  degraded: boolean
}

// 节点快照 15s 推一次, 轮询再快也拿不到更新的数据
const METRICS_POLL_MS = 10_000
const OVERVIEW_POLL_MS = 15_000

export default function DashboardPage() {
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [overview, setOverview] = useState<NodesOverview | null>(null)
  const [overviewError, setOverviewError] = useState<string | null>(null)
  const [statWindow, setStatWindow] = useState<"today" | "last24h">("today")
  const [selectedNode, setSelectedNode] = useState<string | null>(null)
  const [recentNode, setRecentNode] = useState("")
  const { toast } = useToast()
  const lastToastRef = useRef(0)

  const loadMetrics = useCallback(async () => {
    try {
      const qs = recentNode ? `?node=${encodeURIComponent(recentNode)}` : ""
      const res = await fetchApi(`/admin/api/metrics${qs}`)
      if (!res.ok) throw new Error("加载监控数据失败")
      setMetrics(await res.json())
      setError(null)
      return true
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载监控数据失败"
      setError(msg)
      if (Date.now() - lastToastRef.current > 30_000) {
        lastToastRef.current = Date.now()
        toast({ title: "错误", description: msg, variant: "destructive" })
      }
      return false
    }
  }, [recentNode, toast])

  const loadOverview = useCallback(async () => {
    try {
      const res = await fetchApi("/admin/api/nodes/overview")
      if (!res.ok) throw new Error("加载节点数据失败")
      setOverview(await res.json())
      setOverviewError(null)
      return true
    } catch (e) {
      setOverviewError(e instanceof Error ? e.message : "加载节点数据失败")
      return false
    }
  }, [])

  usePolling(loadMetrics, METRICS_POLL_MS, [recentNode])
  usePolling(loadOverview, OVERVIEW_POLL_MS)

  // 节点色按节点 id 排序后的序号分配, 表格 / 堆叠图 / 详情 / 最近请求同一节点同色
  const colors = useMemo(() => {
    const out: Record<string, string> = {}
    overview?.nodes.forEach((n, i) => (out[n.node_id] = nodeColor(i)))
    return out
  }, [overview])
  const nodeIds = overview?.nodes.map((n) => n.node_id) ?? []
  const detail = overview?.nodes.find((n) => n.node_id === selectedNode) ?? null

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">仪表盘</h1>
        <ClusterBadge cluster={metrics?.cluster} />
      </div>

      {!metrics && error ? (
        <div className="flex flex-col items-center justify-center gap-3 rounded-xl border bg-card py-16">
          <p className="font-medium text-destructive">{error}</p>
          <p className="text-sm text-foreground-muted">请检查中控与节点是否正常</p>
          <Button onClick={() => void loadMetrics()}>重试</Button>
        </div>
      ) : (
        <Tabs defaultValue="overview" className="space-y-5">
          <TabsList>
            <TabsTrigger value="overview">概览</TabsTrigger>
            <TabsTrigger value="traffic">流量</TabsTrigger>
            <TabsTrigger value="quality">质量</TabsTrigger>
            <TabsTrigger value="requests">请求</TabsTrigger>
            <TabsTrigger value="cache">缓存</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="mt-0 space-y-5">
            <MetricTiles m={metrics} overview={overview} />
            <NodeCompareTable data={overview} error={overviewError} window={statWindow} onWindowChange={setStatWindow} onSelect={setSelectedNode} />
            <PathStatsList />
          </TabsContent>

          <TabsContent value="traffic" className="mt-0 space-y-5">
            <NodeTrafficPanel colors={colors} />
            <PathTrendChart />
            <PathTotalsChart />
          </TabsContent>

          <TabsContent value="quality" className="mt-0 space-y-5">
            {metrics && (
              <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
                <StatusCodeChart stats={metrics.status_code_stats || {}} />
                <LatencyChart stats={metrics.latency_stats} />
              </div>
            )}
          </TabsContent>

          <TabsContent value="requests" className="mt-0 space-y-5">
            <RecentRequests
              rows={metrics?.recent_requests ?? []}
              nodes={nodeIds}
              node={recentNode}
              onNodeChange={setRecentNode}
              colors={colors}
            />
            <TopReferers rows={metrics?.top_referers ?? []} />
            <RefererDailyChart />
          </TabsContent>

          <TabsContent value="cache" className="mt-0 space-y-5">
            <CacheStatsTiles />
          </TabsContent>
        </Tabs>
      )}

      <NodeDetailDialog
        node={detail}
        color={detail ? colors[detail.node_id] : ""}
        speedLabels={overview?.speed_labels ?? []}
        onClose={() => setSelectedNode(null)}
      />
    </div>
  )
}
