"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Rocket, Server, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useToast } from "@/components/ui/use-toast"
import { fetchApi } from "@/lib/api-client"
import { formatTime } from "@/lib/format"
import { postJSON, type NodeInfo, type ReleaseOverview, type Rollout, type RolloutKind } from "../types"
import { DokployDialog } from "./DokployDialog"

const STEP_CLASS: Record<string, string> = {
  pending: "text-muted-foreground",
  running: "text-[hsl(var(--warning))]",
  done: "text-[hsl(var(--success))]",
  skipped: "text-muted-foreground",
  failed: "text-destructive",
}

const STEP_LABEL: Record<string, string> = {
  pending: "等待",
  running: "进行中",
  done: "完成",
  skipped: "跳过",
  failed: "失败",
}

const ROLLOUT_LABEL: Record<string, string> = {
  running: "进行中",
  done: "已完成",
  failed: "已停止 (失败)",
  cancelled: "已取消",
}

// ReleaseCard 版本发布: 选版本与节点, 逐个滚动升级程序或重建容器, 实时看每个节点的进度
export function ReleaseCard({ nodes, onChange }: { nodes: NodeInfo[]; onChange: () => void }) {
  const { toast } = useToast()
  const [data, setData] = useState<ReleaseOverview | null>(null)
  const [version, setVersion] = useState("")
  const [picked, setPicked] = useState<Set<string> | null>(null)
  const [busy, setBusy] = useState(false)
  const [dokployOpen, setDokployOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetchApi("/admin/api/hub/releases")
      if (res.ok) setData(await res.json())
    } catch {
      /* 下一轮重试 */
    }
  }, [])

  const running = data?.rollouts.find((r) => r.status === "running")

  useEffect(() => {
    load()
    const timer = setInterval(load, running ? 3000 : 10_000)
    return () => clearInterval(timer)
  }, [load, running])

  const latest = data?.releases[0]?.version ?? ""
  const target = version || latest
  // 默认勾选全部在线节点; 用户改过就以用户为准
  const selected = useMemo(() => picked ?? new Set(nodes.filter((n) => n.online).map((n) => n.node_id)), [picked, nodes])

  const toggle = (id: string) => {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setPicked(next)
  }

  const start = async (kind: RolloutKind) => {
    const ids = nodes.map((n) => n.node_id).filter((id) => selected.has(id))
    if (ids.length === 0) return
    const what = kind === "binary" ? `把程序升级到 ${target}` : "逐个重建容器 (Dokploy 重新部署)"
    if (!confirm(`${what}, 共 ${ids.length} 个节点, 一次一个, 任一失败即停止。继续?`)) return
    setBusy(true)
    try {
      await postJSON(fetchApi, "/admin/api/hub/rollouts", { kind, version: target, nodes: ids })
      toast({ title: "已开始滚动发布" })
      load()
    } catch (e) {
      toast({ title: "发起失败", description: (e as Error).message, variant: "destructive" })
    } finally {
      setBusy(false)
    }
  }

  const cancel = async (r: Rollout) => {
    if (!confirm(`取消发布 #${r.id}? 正在处理的节点会继续完成当前这一步。`)) return
    try {
      await postJSON(fetchApi, `/admin/api/hub/rollouts/${r.id}/cancel`, {})
      load()
    } catch (e) {
      toast({ title: "取消失败", description: (e as Error).message, variant: "destructive" })
    }
  }

  const setAuto = async (on: boolean) => {
    try {
      await postJSON(fetchApi, "/admin/api/hub/releases/auto", { auto_binary: on })
      load()
    } catch (e) {
      toast({ title: "保存失败", description: (e as Error).message, variant: "destructive" })
    }
  }

  if (!data) return null
  const shown = running ?? data.rollouts[0]

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-3">
        <CardTitle>版本发布</CardTitle>
        <div className="flex flex-wrap items-center gap-4">
          <Label className="flex items-center gap-2 text-sm font-normal">
            <Switch checked={data.auto_binary} onCheckedChange={setAuto} />
            新版本自动升级程序
          </Label>
          <Button variant="outline" size="sm" onClick={() => setDokployOpen(true)}>
            <Server className="mr-2 size-4" />
            Dokploy 设置
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]">
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label>程序版本</Label>
              <Select value={target} onValueChange={setVersion} disabled={data.releases.length === 0}>
                <SelectTrigger>
                  <SelectValue placeholder="CI 还没有登记任何版本" />
                </SelectTrigger>
                <SelectContent>
                  {data.releases.map((r, i) => (
                    <SelectItem key={r.version} value={r.version}>
                      <span className="font-mono">{r.version}</span>
                      {i === 0 && <span className="ml-1 text-xs text-muted-foreground">(最新)</span>}
                      <span className="ml-2 text-xs text-muted-foreground">{r.title}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" disabled={busy || !!running || !target || selected.size === 0} onClick={() => start("binary")}>
                <Rocket className="mr-2 size-4" />
                滚动升级程序
              </Button>
              <Button size="sm" variant="outline" disabled={busy || !!running || selected.size === 0} onClick={() => start("container")}>
                滚动重建容器
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              升级程序: 节点在容器内换新程序, 每个节点中断约几秒。重建容器: 经 Dokploy 拉最新镜像重新部署, 用于基础镜像更新, 需先在 Dokploy 设置里登记节点对应的应用。
            </p>
          </div>

          <div className="space-y-1.5">
            <Label>节点 (按列表顺序逐个处理)</Label>
            <div className="max-h-56 space-y-1 overflow-y-auto rounded-md border p-2">
              {nodes.map((n) => (
                <label key={n.node_id} className="flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 text-xs hover:bg-accent/40">
                  <input type="checkbox" checked={selected.has(n.node_id)} onChange={() => toggle(n.node_id)} />
                  <span className="font-mono font-medium">{n.node_id}</span>
                  <span className={n.online ? "text-[hsl(var(--success))]" : "text-destructive"}>{n.online ? "在线" : "离线"}</span>
                  <span className="font-mono text-muted-foreground">{n.version || "-"}</span>
                  {n.version && target && n.version === target && <span className="text-muted-foreground">已是目标版本</span>}
                  <span className="ml-auto text-muted-foreground">{n.dokploy_app_id ? "Dokploy 已登记" : "Dokploy 未登记"}</span>
                </label>
              ))}
            </div>
          </div>
        </div>

        {shown && (
          <div className="rounded-md border p-3 text-sm">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="font-medium">
                #{shown.id} {shown.kind === "binary" ? `升级程序到 ${shown.version}` : "重建容器"} · {ROLLOUT_LABEL[shown.status]}
              </span>
              <span className="flex items-center gap-2 text-xs text-muted-foreground">
                {shown.source} · {formatTime(shown.created_at)}
                {shown.status === "running" && (
                  <Button variant="ghost" size="sm" className="h-7" onClick={() => cancel(shown)}>
                    <X className="mr-1 size-3.5" />
                    取消
                  </Button>
                )}
              </span>
            </div>
            {shown.message && <div className="mt-1 text-xs text-muted-foreground">{shown.message}</div>}
            <div className="mt-2 space-y-1">
              {shown.steps.map((s) => (
                <div key={s.node_id} className="flex gap-2 text-xs">
                  <span className="w-40 shrink-0 truncate font-mono">{s.node_id}</span>
                  <span className={`w-12 shrink-0 ${STEP_CLASS[s.status]}`}>{STEP_LABEL[s.status]}</span>
                  <span className="break-all text-muted-foreground">{s.message}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
      <DokployDialog
        open={dokployOpen}
        onClose={() => setDokployOpen(false)}
        servers={data.dokploy_servers}
        nodes={nodes}
        onChange={() => {
          load()
          onChange()
        }}
      />
    </Card>
  )
}
