"use client"

import { useEffect, useState } from "react"
import { Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useToast } from "@/components/ui/use-toast"
import { fetchApi } from "@/lib/api-client"
import { postJSON, type DokployApp, type DokployServer, type NodeInfo } from "../types"

// DokployDialog 登记 Dokploy 实例 (API Key 只写不读) 与每个节点对应的应用, 供"滚动重建容器"使用
export function DokployDialog({
  open,
  onClose,
  servers,
  nodes,
  onChange,
}: {
  open: boolean
  onClose: () => void
  servers: DokployServer[]
  nodes: NodeInfo[]
  onChange: () => void
}) {
  const { toast } = useToast()
  const [form, setForm] = useState({ name: "", url: "", apiKey: "" })
  const [apps, setApps] = useState<Record<number, DokployApp[] | string>>({})

  // 打开时拉一遍每个实例的应用列表 (下拉用); 失败的记下原因展示
  useEffect(() => {
    if (!open) return
    for (const s of servers) {
      fetchApi(`/admin/api/hub/dokploy/${s.id}/apps`)
        .then(async (r) => {
          const d = await r.json().catch(() => ({}))
          setApps((a) => ({ ...a, [s.id]: r.ok ? d.apps || [] : d.error || `HTTP ${r.status}` }))
        })
        .catch((e) => setApps((a) => ({ ...a, [s.id]: String(e) })))
    }
  }, [open, servers])

  const fail = (title: string, e: unknown) => toast({ title, description: (e as Error).message, variant: "destructive" })

  const addServer = async () => {
    try {
      await postJSON(fetchApi, "/admin/api/hub/dokploy", form)
      setForm({ name: "", url: "", apiKey: "" })
      toast({ title: "已保存 Dokploy 实例" })
      onChange()
    } catch (e) {
      fail("保存失败", e)
    }
  }

  const removeServer = async (s: DokployServer) => {
    if (!confirm(`删除 Dokploy 实例 ${s.name}?`)) return
    try {
      await postJSON(fetchApi, `/admin/api/hub/dokploy/${s.id}`, {}, "DELETE")
      onChange()
    } catch (e) {
      fail("删除失败", e)
    }
  }

  const bind = async (n: NodeInfo, serverId: number, app?: DokployApp) => {
    try {
      await postJSON(fetchApi, `/admin/api/hub/nodes/${encodeURIComponent(n.node_id)}/dokploy`, {
        server_id: serverId,
        app_id: app?.id ?? "",
        app_type: app?.type ?? "application",
      })
      toast({ title: "已保存", description: `${n.node_id} → ${app ? app.name : "未登记"}` })
      onChange()
    } catch (e) {
      fail("保存失败", e)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Dokploy 设置</DialogTitle>
        </DialogHeader>

        <section className="space-y-3">
          <h3 className="text-sm font-medium">实例</h3>
          {servers.length === 0 && <p className="text-xs text-muted-foreground">还没有登记 Dokploy 实例。</p>}
          {servers.map((s) => (
            <div key={s.id} className="flex items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm">
              <span>
                <span className="font-medium">{s.name}</span>
                <span className="ml-2 font-mono text-xs text-muted-foreground">{s.url}</span>
                {typeof apps[s.id] === "string" && <span className="ml-2 text-xs text-destructive">{apps[s.id] as string}</span>}
              </span>
              <Button variant="ghost" size="icon" className="size-8" onClick={() => removeServer(s)} aria-label="删除实例">
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
          <div className="grid gap-2 sm:grid-cols-[1fr_1.4fr_1.4fr_auto] sm:items-end">
            <div className="space-y-1">
              <Label className="text-xs">名称</Label>
              <Input value={form.name} placeholder="如 香港" onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">地址</Label>
              <Input value={form.url} placeholder="https://dokploy.example.com" onChange={(e) => setForm({ ...form, url: e.target.value })} />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">API Key</Label>
              <Input type="password" value={form.apiKey} placeholder="Profile → API Keys" onChange={(e) => setForm({ ...form, apiKey: e.target.value })} />
            </div>
            <Button onClick={addServer} disabled={!form.name || !form.url || !form.apiKey}>
              添加
            </Button>
          </div>
        </section>

        <section className="space-y-2">
          <h3 className="text-sm font-medium">节点对应的应用</h3>
          {nodes.map((n) => (
            <div key={n.node_id} className="grid items-center gap-2 text-sm sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]">
              <span className="truncate font-mono text-xs">{n.node_id}</span>
              <AppPicker servers={servers} apps={apps} node={n} onPick={(serverId, app) => bind(n, serverId, app)} />
              <Button variant="ghost" size="sm" className="h-8" disabled={!n.dokploy_app_id} onClick={() => bind(n, 0)}>
                解除
              </Button>
            </div>
          ))}
        </section>
      </DialogContent>
    </Dialog>
  )
}

// AppPicker 先选实例下的应用; 值形如 `<serverId>:<appId>`, 一个下拉就能同时定实例与应用
function AppPicker({
  servers,
  apps,
  node,
  onPick,
}: {
  servers: DokployServer[]
  apps: Record<number, DokployApp[] | string>
  node: NodeInfo
  onPick: (serverId: number, app: DokployApp) => void
}) {
  const value = node.dokploy_server_id && node.dokploy_app_id ? `${node.dokploy_server_id}:${node.dokploy_app_id}` : ""
  return (
    <Select
      value={value}
      onValueChange={(v) => {
        const [sid, ...rest] = v.split(":")
        const appId = rest.join(":")
        const list = apps[Number(sid)]
        const app = Array.isArray(list) ? list.find((a) => a.id === appId) : undefined
        if (app) onPick(Number(sid), app)
      }}
    >
      <SelectTrigger className="h-8 text-xs">
        <SelectValue placeholder="选择 Dokploy 应用" />
      </SelectTrigger>
      <SelectContent>
        {servers.map((s) => {
          const list = apps[s.id]
          if (!Array.isArray(list)) return null
          return list.map((a) => (
            <SelectItem key={`${s.id}:${a.id}`} value={`${s.id}:${a.id}`}>
              {s.name} / {a.project ? `${a.project} / ` : ""}
              {a.name}
              {a.type === "compose" && " (compose)"}
            </SelectItem>
          ))
        })}
      </SelectContent>
    </Select>
  )
}
