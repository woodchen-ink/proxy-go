"use client"

import { useCallback, useEffect, useState } from "react"
import { Plus, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useToast } from "@/components/ui/use-toast"
import { fetchApi } from "@/lib/api-client"
import { formatTime } from "@/lib/format"
import { BindingDialog } from "./components/BindingDialog"
import { CredentialsCard } from "./components/CredentialsCard"
import { type Binding, type BindingCheck, type Credential, type NodeOption, readError } from "./types"

// CheckCell 两处线上实际值与期望的对照; 读不到与不一致分开展示
function CheckCell({ check }: { check: BindingCheck | null }) {
  if (!check) return <span className="text-xs text-muted-foreground">未检查</span>
  const errors = [check.cf_error && `CF: ${check.cf_error}`, check.dnspod_error && `DNSPod: ${check.dnspod_error}`].filter(Boolean)
  return (
    <div className="space-y-0.5 text-xs">
      {errors.length > 0 ? (
        <div className="text-destructive">{errors.join("; ")}</div>
      ) : check.consistent ? (
        <div className="text-[hsl(var(--success))]">一致</div>
      ) : (
        <div className="text-[hsl(var(--warning))]">与配置不一致</div>
      )}
      <div className="font-mono text-muted-foreground">CF 回源 {check.cf_origin || "-"}</div>
      <div className="font-mono text-muted-foreground">DNSPod {check.dnspod_value || "-"}</div>
      <div className="text-muted-foreground">{formatTime(check.at)}</div>
    </div>
  )
}

export default function DnsPage() {
  const { toast } = useToast()
  const [credentials, setCredentials] = useState<Credential[]>([])
  const [bindings, setBindings] = useState<Binding[]>([])
  const [nodes, setNodes] = useState<NodeOption[]>([])
  const [editing, setEditing] = useState<Binding | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)
  const [targets, setTargets] = useState<Record<number, string>>({})

  const load = useCallback(async () => {
    const [c, b, n] = await Promise.all([
      fetchApi("/admin/api/dns/credentials").then((r) => r.json()),
      fetchApi("/admin/api/dns/bindings").then((r) => r.json()),
      fetchApi("/admin/api/hub/nodes").then((r) => r.json()),
    ])
    setCredentials(c.credentials || [])
    setBindings(b.bindings || [])
    setNodes(n.nodes || [])
  }, [])

  useEffect(() => {
    load().catch(() => {})
    const timer = setInterval(() => load().catch(() => {}), 30_000)
    return () => clearInterval(timer)
  }, [load])

  const check = async (b: Binding) => {
    setBusy(b.id)
    try {
      const res = await fetchApi(`/admin/api/dns/bindings/${b.id}/check`, { method: "POST" })
      if (!res.ok) throw new Error(await readError(res))
      await load()
    } catch (e) {
      toast({ title: "检查失败", description: e instanceof Error ? e.message : String(e), variant: "destructive" })
    } finally {
      setBusy(null)
    }
  }

  const switchTo = async (b: Binding) => {
    const target = targets[b.id]
    if (!target) return
    const origin = nodes.find((n) => n.node_id === target)?.origin_host
    if (!confirm(`把 ${b.hostname} 切到 ${target} (${origin})?\n会同时修改 CF SaaS 回源与 DNSPod「${b.record_line}」线路记录, 并把该节点设为首选。`)) return
    setBusy(b.id)
    try {
      const res = await fetchApi(`/admin/api/dns/bindings/${b.id}/switch`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ node_id: target }),
      })
      if (!res.ok) throw new Error(await readError(res))
      const data = await res.json()
      toast({
        title: data.errors?.length ? "部分失败" : "已切换",
        description: `CF ${data.cf} · DNSPod ${data.dnspod}${data.errors?.length ? ` · ${data.errors.join("; ")}` : ""}`,
        variant: data.errors?.length ? "destructive" : undefined,
      })
      await load()
    } catch (e) {
      toast({ title: "切换失败", description: e instanceof Error ? e.message : String(e), variant: "destructive" })
    } finally {
      setBusy(null)
    }
  }

  const remove = async (b: Binding) => {
    if (!confirm(`删除绑定 ${b.hostname}? 只删中控里的记录, 不改线上解析。`)) return
    const res = await fetchApi(`/admin/api/dns/bindings/${b.id}`, { method: "DELETE" })
    if (!res.ok) toast({ title: "删除失败", description: await readError(res), variant: "destructive" })
    load()
  }

  const nodeState = (id: string) => {
    const n = nodes.find((x) => x.node_id === id)
    if (!n) return <span className="text-muted-foreground">未接入</span>
    return n.online ? <span className="text-[hsl(var(--success))]">在线</span> : <span className="text-destructive">离线</span>
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>DNS 绑定</CardTitle>
            <p className="mt-1 text-sm text-muted-foreground">
              每个访问域名的 CF SaaS 回源与 DNSPod 线路记录同时指向当前节点; 节点故障时自动切到池内下一个健康节点, 并推送企业微信通知。
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => load()}>
              <RefreshCw className="mr-2 size-4" />
              刷新
            </Button>
            <Button
              size="sm"
              onClick={() => {
                setEditing(null)
                setDialogOpen(true)
              }}
            >
              <Plus className="mr-2 size-4" />
              新建绑定
            </Button>
          </div>
        </CardHeader>
        <CardContent className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>访问域名</TableHead>
                <TableHead>当前节点</TableHead>
                <TableHead>节点池</TableHead>
                <TableHead>线上状态</TableHead>
                <TableHead>自动</TableHead>
                <TableHead className="min-w-[260px]">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {bindings.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    还没有绑定
                  </TableCell>
                </TableRow>
              )}
              {bindings.map((b) => (
                <TableRow key={b.id} className={b.enabled ? "" : "opacity-60"}>
                  <TableCell>
                    <div className="font-medium">{b.hostname}</div>
                    <div className="text-xs text-muted-foreground">
                      {b.saas_zone_name} · DNSPod {b.dnspod_domain} {b.record_name}/{b.record_line}
                    </div>
                  </TableCell>
                  <TableCell className="text-xs">
                    <div className="font-mono">{b.active_node}</div>
                    <div>{nodeState(b.active_node)}</div>
                  </TableCell>
                  <TableCell className="space-y-0.5 text-xs">
                    {b.pool.map((id, i) => (
                      <div key={id} className="font-mono">
                        {i + 1}. {id} {nodeState(id)}
                      </div>
                    ))}
                  </TableCell>
                  <TableCell>
                    <CheckCell check={b.last_check} />
                  </TableCell>
                  <TableCell className="text-xs">
                    <div>切换 {b.auto_failover ? "开" : "关"}</div>
                    <div>切回 {b.auto_failback ? "开" : "关"}</div>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-2">
                      <Select value={targets[b.id] || ""} onValueChange={(v) => setTargets((t) => ({ ...t, [b.id]: v }))}>
                        <SelectTrigger className="h-8 w-40 text-xs">
                          <SelectValue placeholder="切换到..." />
                        </SelectTrigger>
                        <SelectContent>
                          {nodes
                            .filter((n) => n.origin_host)
                            .map((n) => (
                              <SelectItem key={n.node_id} value={n.node_id}>
                                {n.node_id}
                              </SelectItem>
                            ))}
                        </SelectContent>
                      </Select>
                      <Button size="sm" className="h-8" disabled={!targets[b.id] || busy === b.id} onClick={() => switchTo(b)}>
                        切换
                      </Button>
                      <Button size="sm" variant="outline" className="h-8" disabled={busy === b.id} onClick={() => check(b)}>
                        检查
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-8"
                        onClick={() => {
                          setEditing(b)
                          setDialogOpen(true)
                        }}
                      >
                        编辑
                      </Button>
                      <Button size="sm" variant="ghost" className="h-8" onClick={() => remove(b)}>
                        删除
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <CredentialsCard credentials={credentials} onChange={() => load()} />

      <BindingDialog
        open={dialogOpen}
        binding={editing}
        credentials={credentials}
        nodes={nodes}
        onClose={() => setDialogOpen(false)}
        onSaved={() => {
          setDialogOpen(false)
          load()
        }}
      />
    </div>
  )
}
