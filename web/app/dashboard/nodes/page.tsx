"use client"

import { useCallback, useEffect, useState } from "react"
import { RefreshCw, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { PageHeader } from "@/components/page-header"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useToast } from "@/components/ui/use-toast"
import { fetchApi } from "@/lib/api-client"
import { formatAge, formatBps, formatTime } from "@/lib/format"
import { ReleaseCard } from "./components/ReleaseCard"
import type { NodeInfo } from "./types"

interface CommandInfo {
  seq: number
  type: string
  payload: Record<string, unknown>
  source: string
  created_at: number
  acks: { node_id: string; ok: boolean; message: string; acked_at: number }[]
}

interface EventInfo {
  id: number
  at: number
  level: "info" | "warning" | "error"
  kind: string
  subject: string
  message: string
}

const COMMAND_LABELS: Record<string, string> = {
  cache_clear: "清理缓存",
  cache_enable: "缓存开关",
  pulse: "配置 / 统计同步",
  unban: "解封 IP",
}

const LEVEL_CLASS: Record<string, string> = {
  info: "text-muted-foreground",
  warning: "text-warning",
  error: "text-destructive",
}

// NodesPage 中控视角的节点总览: 在线状态 / 回源主机名登记 / 版本发布 / 指令回执 / 事件日志
export default function NodesPage() {
  const { toast } = useToast()
  const [nodes, setNodes] = useState<NodeInfo[]>([])
  const [configVersion, setConfigVersion] = useState(0)
  const [commands, setCommands] = useState<CommandInfo[]>([])
  const [events, setEvents] = useState<EventInfo[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const [n, c, e] = await Promise.all([
        fetchApi("/admin/api/hub/nodes").then((r) => r.json()),
        fetchApi("/admin/api/hub/commands?limit=30").then((r) => r.json()),
        fetchApi("/admin/api/hub/events?limit=100").then((r) => r.json()),
      ])
      setNodes(n.nodes || [])
      setConfigVersion(n.config_version || 0)
      setCommands(c.commands || [])
      setEvents(e.events || [])
    } catch {
      /* 401 已由 fetchApi 处理, 网络错误下一轮重试 */
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const timer = setInterval(load, 10_000)
    return () => clearInterval(timer)
  }, [load])

  const saveOrigin = async (nodeId: string) => {
    const value = drafts[nodeId]
    if (value === undefined) return
    const res = await fetchApi(`/admin/api/hub/nodes/${encodeURIComponent(nodeId)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ origin_host: value }),
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) {
      toast({ title: "保存失败", description: data.error, variant: "destructive" })
      return
    }
    toast({ title: "已保存", description: `${nodeId} 的回源主机名已更新` })
    setDrafts((d) => {
      const next = { ...d }
      delete next[nodeId]
      return next
    })
    load()
  }

  const removeNode = async (nodeId: string) => {
    if (!confirm(`移除节点 ${nodeId}? 它若重新连上中控会自动回来。`)) return
    const res = await fetchApi(`/admin/api/hub/nodes/${encodeURIComponent(nodeId)}`, { method: "DELETE" })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) toast({ title: "移除失败", description: data.error, variant: "destructive" })
    load()
  }

  if (loading) {
    return <div className="py-20 text-center text-muted-foreground">加载中...</div>
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="节点"
        description="在线状态、回源主机名与配置同步; 各节点承载的流量与下载速度见仪表盘"
        actions={
          <Button variant="outline" size="sm" onClick={load}>
            <RefreshCw className="size-4" />
            刷新
          </Button>
        }
      />
      <Card className="overflow-hidden">
        <CardContent className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>节点</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>出口 IP</TableHead>
                <TableHead className="min-w-[260px]">回源主机名</TableHead>
                <TableHead>回源探测</TableHead>
                <TableHead className="text-right">QPS</TableHead>
                <TableHead className="text-right">带宽</TableHead>
                <TableHead>配置</TableHead>
                <TableHead>版本</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {nodes.length === 0 && (
                <TableRow>
                  <TableCell colSpan={10} className="py-8 text-center text-muted-foreground">
                    还没有节点连上中控。节点升级到新版本后会自动出现在这里。
                  </TableCell>
                </TableRow>
              )}
              {nodes.map((n) => {
                const draft = drafts[n.node_id]
                const configSynced = !!n.live && n.live.config_version >= configVersion
                return (
                  <TableRow key={n.node_id}>
                    <TableCell className="font-mono text-xs font-medium">
                      {n.node_id}
                      {n.note && <div className="font-sans text-muted-foreground">{n.note}</div>}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs">
                      <span
                        aria-hidden
                        className={`mr-1.5 inline-block size-2 rounded-full ${n.online ? "bg-success" : "bg-destructive"}`}
                      />
                      {n.online ? "在线" : `离线 ${formatAge(n.state_since)}`}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{n.ipv4 || n.ipv6 || "-"}</TableCell>
                    <TableCell>
                      <div className="flex gap-2">
                        <Input
                          className="h-8 font-mono text-xs"
                          placeholder="如 rn-22dc3.20200511.xyz"
                          value={draft ?? n.origin_host}
                          onChange={(e) => setDrafts((d) => ({ ...d, [n.node_id]: e.target.value }))}
                        />
                        {draft !== undefined && draft !== n.origin_host && (
                          <Button size="sm" className="h-8" onClick={() => saveOrigin(n.node_id)}>
                            保存
                          </Button>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs">
                      {!n.probe ? (
                        <span className="text-muted-foreground">-</span>
                      ) : n.probe.ok ? (
                        <span className="text-success">正常</span>
                      ) : (
                        <span className="text-destructive" title={n.probe.error}>
                          失败
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-xs">
                      {n.live ? n.live.requests_per_second.toFixed(2) : "-"}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-xs">
                      {n.live ? formatBps(n.live.current_bandwidth_bps) : "-"}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-xs">
                      {!n.live ? "-" : configSynced ? (
                        <span className="text-success">已同步</span>
                      ) : (
                        <span className="text-warning">待同步</span>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{n.version || "-"}</TableCell>
                    <TableCell>
                      {!n.online && (
                        <Button variant="ghost" size="icon" className="size-8" onClick={() => removeNode(n.node_id)} aria-label="移除节点">
                          <Trash2 className="size-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <ReleaseCard nodes={nodes} onChange={load} />

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">最近指令</CardTitle>
          </CardHeader>
          <CardContent className="max-h-[480px] space-y-3 overflow-y-auto text-sm">
            {commands.length === 0 && <div className="text-muted-foreground">暂无</div>}
            {commands.map((c) => {
              const failed = c.acks.filter((a) => !a.ok)
              return (
                <div key={c.seq} className="border-b pb-2 last:border-0">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">
                      #{c.seq} {COMMAND_LABELS[c.type] || c.type}
                    </span>
                    <span className="text-xs text-muted-foreground">{formatTime(c.created_at)}</span>
                  </div>
                  <div className="mt-0.5 break-all font-mono text-xs text-muted-foreground">
                    {JSON.stringify(c.payload)} · {c.source}
                  </div>
                  <div className="mt-1 text-xs">
                    {c.acks.length} 个节点已执行
                    {failed.length > 0 && (
                      <span className="text-destructive">
                        , 失败: {failed.map((f) => `${f.node_id} (${f.message})`).join(", ")}
                      </span>
                    )}
                  </div>
                </div>
              )
            })}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">事件</CardTitle>
          </CardHeader>
          <CardContent className="max-h-[480px] space-y-2 overflow-y-auto text-sm">
            {events.length === 0 && <div className="text-muted-foreground">暂无</div>}
            {events.map((e) => (
              <div key={e.id} className="border-b pb-2 last:border-0">
                <div className="flex items-center justify-between gap-2">
                  <span className={`font-medium ${LEVEL_CLASS[e.level] || ""}`}>
                    {e.subject && <span className="font-mono">{e.subject} · </span>}
                    {e.kind}
                  </span>
                  <span className="text-xs text-muted-foreground">{formatTime(e.at)}</span>
                </div>
                <div className="mt-0.5 break-all text-xs text-muted-foreground">{e.message}</div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
