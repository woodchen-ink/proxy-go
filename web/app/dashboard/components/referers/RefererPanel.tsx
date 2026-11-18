"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { RefreshCw, Settings2 } from "lucide-react"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useToast } from "@/components/ui/use-toast"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { fetchApi } from "@/lib/api-client"
import ActiveTable from "./ActiveTable"
import HistoryTable from "./HistoryTable"
import RefererTrend from "./RefererTrend"
import SettingsDialog from "./SettingsDialog"
import type { ActiveHost, HostAction, RefererView } from "./types"

const COLORS = [1, 2, 3, 4, 5, 6].map((i) => `hsl(var(--chart-${i}))`)
type View = "external" | "own" | "history"
type SortKey = "requests" | "bytes" | "blocked" | "first_seen"

const SORTS: { key: SortKey; label: string }[] = [
  { key: "requests", label: "按请求" },
  { key: "bytes", label: "按流量" },
  { key: "blocked", label: "按拦截" },
  { key: "first_seen", label: "按首次出现" },
]

function sortHosts(rows: ActiveHost[], key: SortKey): ActiveHost[] {
  const out = [...rows]
  if (key === "first_seen") out.sort((a, b) => b.first_seen.localeCompare(a.first_seen) || b.requests - a.requests)
  else out.sort((a, b) => b[key] - a[key] || b.requests - a.requests)
  return out
}

// RefererPanel 引用来源: 近 30 天外部 / 自有来源 + 30 天前出现过的历史来源 (长期保存), 可直接拉黑或标为自有
export default function RefererPanel() {
  const { toast } = useToast()
  const [data, setData] = useState<RefererView | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [view, setView] = useState<View>("external")
  const [sort, setSort] = useState<SortKey>("requests")
  const [query, setQuery] = useState("")
  const [chart, setChart] = useState<string[] | null>(null) // null = 未手动选择, 默认外部前 6
  const [busy, setBusy] = useState<string | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [confirmBan, setConfirmBan] = useState<string | null>(null)

  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const resp = await fetchApi("/admin/api/referers")
      if (!resp.ok) throw new Error("加载引用来源失败")
      setData((await resp.json()) as RefererView)
      setError(null)
      return true
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载失败")
      return false
    } finally {
      setLoading(false)
    }
  }, [])
  // 天级数据, 每次加载要读 40 天的 D1 行: 只在打开时加载一次, 需要时手动刷新, 不轮询
  useEffect(() => {
    load()
  }, [load])

  const external = useMemo(() => (data?.hosts ?? []).filter((h) => !h.is_own), [data])
  const own = useMemo(() => (data?.hosts ?? []).filter((h) => h.is_own), [data])
  const q = query.trim().toLowerCase()

  const chartHosts = useMemo(() => {
    const byHost = new Map((data?.hosts ?? []).map((h) => [h.host, h]))
    const picked = chart ?? external.filter((h) => h.daily.length > 0).slice(0, COLORS.length).map((h) => h.host)
    return picked.map((h) => byHost.get(h)).filter((h): h is ActiveHost => !!h && h.daily.length > 0)
  }, [data, chart, external])
  const colorOf = useCallback((host: string) => {
    const i = chartHosts.findIndex((h) => h.host === host)
    return i >= 0 ? COLORS[i] : undefined
  }, [chartHosts])

  const toggleChart = (host: string) => {
    const cur = chartHosts.map((h) => h.host)
    if (cur.includes(host)) setChart(cur.filter((h) => h !== host))
    else if (cur.length >= COLORS.length) toast({ title: "最多同时显示 6 个来源", description: "先点掉一个再加" })
    else setChart([...cur, host])
  }

  const post = async (url: string, body: unknown) => {
    const resp = await fetchApi(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
    const json = await resp.json().catch(() => null)
    if (!resp.ok) throw new Error(json?.error ?? json?.message ?? `请求失败 (${resp.status})`)
    return json
  }

  const onAction = async (a: HostAction) => {
    if (a.kind === "own" && !a.own && data && !data.settings.own_hosts.includes(a.host)) {
      toast({ title: "无法直接取消", description: "该来源由上级域名或 DNS 域名判定为自有, 请在设置里调整", variant: "destructive" })
      return
    }
    if (a.kind === "ban" && a.banned && confirmBan !== a.host) {
      setConfirmBan(a.host)
      return
    }
    setConfirmBan(null)
    setBusy(a.host)
    try {
      const res =
        a.kind === "ban"
          ? await post("/admin/api/referers/ban", { host: a.host, banned: a.banned })
          : await post("/admin/api/referers/settings", a.own ? { add_own: a.host } : { remove_own: a.host })
      toast({ title: "已更新", description: res?.message ?? (a.kind === "own" ? (a.own ? `${a.host} 已标为自有` : `${a.host} 已取消自有`) : undefined) })
      await load()
    } catch (e) {
      toast({ title: "操作失败", description: e instanceof Error ? e.message : String(e), variant: "destructive" })
    } finally {
      setBusy(null)
    }
  }

  const saveSettings = async (patch: { own_hosts: string[]; alert_threshold: number }) => {
    setSaving(true)
    try {
      await post("/admin/api/referers/settings", patch)
      setSettingsOpen(false)
      toast({ title: "设置已保存" })
      await load()
    } catch (e) {
      toast({ title: "保存失败", description: e instanceof Error ? e.message : String(e), variant: "destructive" })
    } finally {
      setSaving(false)
    }
  }

  const activeRows = useMemo(() => sortHosts((view === "own" ? own : external).filter((h) => !q || h.host.includes(q)), sort), [view, own, external, q, sort])
  const historyRows = useMemo(() => (data?.history ?? []).filter((h) => !q || h.host.includes(q)), [data, q])
  const newCount = external.filter((h) => h.is_new).length

  const tabs: { key: View; label: string; count: number }[] = [
    { key: "external", label: "外部", count: external.length },
    { key: "own", label: "自有", count: own.length },
    { key: "history", label: "30 天前", count: data?.history.length ?? 0 },
  ]

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-5 py-3">
        <h2 className="text-base font-semibold">
          引用来源 <span className="ml-1 text-sm font-normal text-foreground-subtle">按 Referer host · 长期保存</span>
        </h2>
        {newCount > 0 && <span className="rounded bg-warning-soft px-2 py-0.5 text-xs text-warning">近 {data?.new_days} 天新出现 {newCount} 个</span>}
        <div className="ml-auto flex items-center gap-2">
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索 host" className="h-8 w-40" />
          <Button variant="ghost" size="sm" className="h-8 px-2" onClick={() => load()} disabled={loading} title="刷新 (数据每 5 分钟更新)">
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          </Button>
          <Button variant="ghost" size="sm" className="h-8 px-2" onClick={() => setSettingsOpen(true)} disabled={!data} title="自有域名与告警">
            <Settings2 className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {error && !data ? (
        <div className="flex h-48 items-center justify-center text-sm text-destructive">{error}</div>
      ) : !data ? (
        <div className="flex h-48 items-center justify-center text-sm text-foreground-subtle">加载中...</div>
      ) : (
        <>
          {view !== "history" && (
            <div className="px-5 pt-4">
              <RefererTrend hosts={chartHosts} colorOf={colorOf} />
            </div>
          )}
          <div className="flex flex-wrap items-center gap-2 px-5 py-3">
            <div className="inline-flex rounded-md bg-muted p-0.5 text-sm">
              {tabs.map((t) => (
                <button
                  key={t.key}
                  type="button"
                  onClick={() => setView(t.key)}
                  className={`rounded px-3 py-1 ${view === t.key ? "bg-background font-medium shadow-sm" : "text-foreground-muted hover:text-foreground"}`}
                >
                  {t.label}{" "}
                  <span className="tabular-nums text-foreground-subtle">
                    {t.count}
                    {t.key === "history" && data?.history_truncated ? "+" : ""}
                  </span>
                </button>
              ))}
            </div>
            {view !== "history" && (
              <div className="ml-auto inline-flex gap-1 text-xs">
                {SORTS.map((s) => (
                  <button
                    key={s.key}
                    type="button"
                    onClick={() => setSort(s.key)}
                    className={`rounded px-2 py-1 ${sort === s.key ? "bg-brand-soft text-brand-strong" : "text-foreground-muted hover:bg-accent"}`}
                  >
                    {s.label}
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className="max-h-[32rem] overflow-auto border-t">
            {view === "history" ? (
              historyRows.length ? (
                <HistoryTable rows={historyRows} onAction={onAction} busy={busy} />
              ) : (
                <Empty text="暂无 30 天前的来源 (每天清理时把 35 天前的数据按 host 累加进长期记录)" />
              )
            ) : activeRows.length ? (
              <ActiveTable rows={activeRows} colorOf={colorOf} onToggleChart={toggleChart} onAction={onAction} busy={busy} />
            ) : (
              <Empty text="暂无数据 (只记录单日请求 ≥ 10 的来源)" />
            )}
          </div>
        </>
      )}

      <AlertDialog open={confirmBan !== null} onOpenChange={(v) => !v && setConfirmBan(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>拉黑 {confirmBan}?</AlertDialogTitle>
            <AlertDialogDescription>
              加入全局 Referer 黑名单, 全部节点立即生效: 从该站点 (含子域) 引用的所有资源都返回 403。之后可在列表里取消拉黑。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => confirmBan && onAction({ kind: "ban", host: confirmBan, banned: true })}
            >
              拉黑
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {data && <SettingsDialog open={settingsOpen} settings={data.settings} saving={saving} onClose={() => setSettingsOpen(false)} onSave={saveSettings} />}
    </Card>
  )
}

function Empty({ text }: { text: string }) {
  return <div className="flex h-32 items-center justify-center px-5 text-center text-sm text-foreground-subtle">{text}</div>
}
