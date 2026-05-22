"use client"

// CDNCacheManagement: CDN provider 管理 + 快捷 purge 单页主控
// 单一职责: 围绕"provider 列表 + 当前活动 provider + purging 状态"这一组紧耦合状态
// 编排列表渲染 / 快捷按钮 / 4 个 purge 子页 / 编辑、删除、结果 3 个 modal
// 拆分判定: 编辑表单已独立到 [[CDNProviderEditor]], 重复 tab 已抽到 [[PurgeTab]]
// 剩余各部分共享 providers / activeProvider / purging / lastResult, 继续拆需要把 4 段状态全部往下透传
// 拆碎后调用方反而要在多个文件间跳读, 故保留单文件; 新增功能时如新增的状态不依赖现有状态, 必须拆走

import { useCallback, useEffect, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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
import { CheckCircle2, Cloud, Plus, Trash2, Edit, Eraser, AlertTriangle } from "lucide-react"
import CDNProviderEditor, {
  CDNProvider,
  CDNProviderType,
  emptyCredentialsFor,
} from "./CDNProviderEditor"

type PurgeType = "all" | "urls" | "prefixes" | "hosts" | "tags"

interface PurgeResult {
  success: boolean
  provider_id: string
  provider: string
  job_id?: string
  message?: string
  raw?: unknown
}

interface PurgeResponse {
  result?: PurgeResult
  error?: string
}

const PROVIDER_TYPE_LABEL: Record<CDNProviderType, string> = {
  cloudflare: "Cloudflare",
  edgeone: "腾讯云 EdgeOne",
}

// 生成新 provider, 默认 cloudflare 模板 + 关闭状态
function newProvider(type: CDNProviderType = "cloudflare"): CDNProvider {
  return {
    ID: typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : Date.now().toString(),
    Name: "",
    Type: type,
    Enabled: false,
    Credentials: emptyCredentialsFor(type),
  }
}

// 把多行 / 逗号分隔的字符串拆成 trim 后的非空数组
function splitTargets(text: string): string[] {
  return text
    .split(/[\n,]/)
    .map((t) => t.trim())
    .filter((t) => t.length > 0)
}

// 当前页面 origin / host, SSR 阶段返回空字符串, 实际使用全部在客户端
function currentHost(): string {
  if (typeof window === "undefined") return ""
  return window.location.hostname
}

function currentOrigin(): string {
  if (typeof window === "undefined") return ""
  return window.location.origin
}

// 对纯路径 (以 / 开头) 补全为带当前 origin 的完整 URL, 完整 URL 原样保留
function normalizePrefix(raw: string): string {
  const t = raw.trim()
  if (!t) return ""
  if (t.startsWith("/")) {
    const origin = currentOrigin()
    if (!origin) return t
    return origin.replace(/\/+$/, "") + t
  }
  return t
}

// CDNCacheManagement 负责 provider 列表展示 + 启停 + 编辑入口 + 快捷 purge
// 编辑表单本身拆到 [[CDNProviderEditor]] 以保持本文件单一职责
export default function CDNCacheManagement() {
  const { toast } = useToast()
  const router = useRouter()

  const [providers, setProviders] = useState<CDNProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const [editorOpen, setEditorOpen] = useState(false)
  const [editingProvider, setEditingProvider] = useState<CDNProvider | null>(null)
  const [editorIsNew, setEditorIsNew] = useState(false)

  const [deletingId, setDeletingId] = useState<string | null>(null)

  const [purgeUrls, setPurgeUrls] = useState("")
  const [purgePrefixes, setPurgePrefixes] = useState("")
  const [purgeHosts, setPurgeHosts] = useState("")
  const [purgeTags, setPurgeTags] = useState("")
  const [purging, setPurging] = useState<PurgeType | null>(null)
  const [purgeAllConfirmOpen, setPurgeAllConfirmOpen] = useState(false)
  const [lastResult, setLastResult] = useState<PurgeResult | null>(null)
  const [resultDialogOpen, setResultDialogOpen] = useState(false)

  const activeProvider = useMemo(() => providers.find((p) => p.Enabled) || null, [providers])

  const handleUnauthorized = useCallback(() => {
    localStorage.removeItem("token")
    router.push("/login")
  }, [router])

  const fetchProviders = useCallback(async () => {
    try {
      const res = await fetch("/admin/api/cdn/providers")
      if (res.status === 401) {
        handleUnauthorized()
        return
      }
      if (!res.ok) throw new Error("获取 CDN 配置失败")
      const data = await res.json()
      setProviders(Array.isArray(data.providers) ? data.providers : [])
    } catch (err) {
      toast({
        title: "错误",
        description: err instanceof Error ? err.message : "获取 CDN 配置失败",
        variant: "destructive",
      })
    } finally {
      setLoading(false)
    }
  }, [handleUnauthorized, toast])

  useEffect(() => {
    fetchProviders()
  }, [fetchProviders])

  // saveAll 用整表覆盖语义保存, 后端做唯一启用 + 字段合法性校验
  const saveAll = useCallback(
    async (next: CDNProvider[]) => {
      setSaving(true)
      try {
        const res = await fetch("/admin/api/cdn/providers", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ providers: next }),
        })
        if (res.status === 401) {
          handleUnauthorized()
          return false
        }
        if (!res.ok) {
          const text = await res.text()
          throw new Error(text || "保存 CDN 配置失败")
        }
        const data = await res.json()
        setProviders(Array.isArray(data.providers) ? data.providers : next)
        return true
      } catch (err) {
        toast({
          title: "保存失败",
          description: err instanceof Error ? err.message : "保存 CDN 配置失败",
          variant: "destructive",
        })
        return false
      } finally {
        setSaving(false)
      }
    },
    [handleUnauthorized, toast],
  )

  const openCreate = () => {
    setEditingProvider(newProvider("cloudflare"))
    setEditorIsNew(true)
    setEditorOpen(true)
  }

  const openEdit = (p: CDNProvider) => {
    setEditingProvider({ ...p, Credentials: { ...p.Credentials } })
    setEditorIsNew(false)
    setEditorOpen(true)
  }

  // submitEditor 提交编辑结果: 必填检查 + 启用唯一化, 通过后整表上传
  const submitEditor = async () => {
    if (!editingProvider) return
    if (!editingProvider.Name.trim()) {
      toast({ title: "错误", description: "请填写配置名称", variant: "destructive" })
      return
    }

    const required = editingProvider.Type === "cloudflare"
      ? ["apiToken", "zoneId"]
      : ["secretId", "secretKey", "zoneId"]
    for (const k of required) {
      if (!editingProvider.Credentials[k]?.trim()) {
        toast({ title: "错误", description: `请填写 ${k}`, variant: "destructive" })
        return
      }
    }

    const next = editorIsNew
      ? [...providers, editingProvider]
      : providers.map((p) => (p.ID === editingProvider.ID ? editingProvider : p))

    const normalized = editingProvider.Enabled
      ? next.map((p) => ({ ...p, Enabled: p.ID === editingProvider.ID }))
      : next

    const ok = await saveAll(normalized)
    if (ok) {
      setEditorOpen(false)
      setEditingProvider(null)
      toast({ title: "已保存", description: editorIsNew ? "已新增 CDN 配置" : "已更新 CDN 配置" })
    }
  }

  const toggleEnabled = async (id: string, enabled: boolean) => {
    const next = providers.map((p) => ({
      ...p,
      Enabled: p.ID === id ? enabled : enabled ? false : p.Enabled,
    }))
    await saveAll(next)
  }

  const confirmDelete = async () => {
    if (!deletingId) return
    const next = providers.filter((p) => p.ID !== deletingId)
    const ok = await saveAll(next)
    if (ok) toast({ title: "已删除" })
    setDeletingId(null)
  }

  // runPurge 调用后端 /admin/api/cdn/purge, 后端按当前启用的 provider 路由
  const runPurge = async (type: PurgeType, targets: string[] = []) => {
    if (!activeProvider) {
      toast({ title: "错误", description: "请先启用一个 CDN 配置", variant: "destructive" })
      return
    }
    setPurging(type)
    try {
      const res = await fetch("/admin/api/cdn/purge", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type, targets }),
      })
      if (res.status === 401) {
        handleUnauthorized()
        return
      }
      const data: PurgeResponse = await res.json()
      const result: PurgeResult = data.result ?? {
        success: false,
        provider_id: activeProvider.ID,
        provider: activeProvider.Type,
        message: data.error || "清理失败",
      }
      setLastResult(result)
      setResultDialogOpen(true)
      if (result.success) {
        toast({ title: "清理成功", description: result.job_id ? `任务 ID: ${result.job_id}` : "缓存已清理" })
      } else {
        toast({ title: "清理失败", description: result.message || "未知错误", variant: "destructive" })
      }
    } catch (err) {
      toast({
        title: "请求失败",
        description: err instanceof Error ? err.message : "请求失败",
        variant: "destructive",
      })
    } finally {
      setPurging(null)
    }
  }

  const purgeListType = async (type: Exclude<PurgeType, "all">, text: string) => {
    let targets = splitTargets(text)
    // 主机 / 前缀 留空时自动补当前站点
    if (targets.length === 0 && (type === "hosts" || type === "prefixes")) {
      const host = currentHost()
      if (type === "hosts" && host) targets = [host]
      if (type === "prefixes") {
        const origin = currentOrigin()
        if (origin) targets = [origin + "/"]
      }
    }
    if (targets.length === 0) {
      toast({ title: "错误", description: "请至少输入一个目标", variant: "destructive" })
      return
    }
    // 前缀: 把以 / 开头的相对路径补成带当前 origin 的完整 URL
    if (type === "prefixes") {
      targets = targets.map(normalizePrefix).filter((t) => t.length > 0)
    }
    await runPurge(type, targets)
  }

  // purgeCurrentHost 一键清理当前页面 host 的全部缓存
  const purgeCurrentHost = async () => {
    const host = currentHost()
    if (!host) {
      toast({ title: "错误", description: "无法识别当前 host", variant: "destructive" })
      return
    }
    await runPurge("hosts", [host])
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-10 text-sm text-muted-foreground">
        加载中...
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Cloud className="h-6 w-6 text-muted-foreground" />
          <h2 className="text-xl font-semibold tracking-tight">CDN 缓存清理</h2>
        </div>
        <Button onClick={openCreate} variant="outline" size="sm" disabled={saving}>
          <Plus className="mr-2 h-4 w-4" />
          新增配置
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">配置列表</CardTitle>
          <CardDescription>
            支持多套配置, 仅启用 1 个生效。配置随主配置一起同步到 D1, 不存浏览器。
          </CardDescription>
        </CardHeader>
        <CardContent>
          {providers.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">
              暂无 CDN 配置, 点击右上角 &ldquo;新增配置&rdquo; 添加
            </p>
          ) : (
            <div className="space-y-2">
              {providers.map((p) => (
                <ProviderRow
                  key={p.ID}
                  provider={p}
                  saving={saving}
                  onToggle={(enabled) => toggleEnabled(p.ID, enabled)}
                  onEdit={() => openEdit(p)}
                  onDelete={() => setDeletingId(p.ID)}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <CardTitle className="text-base">快捷清理</CardTitle>
              <CardDescription>
                {activeProvider ? (
                  <>
                    当前启用:{" "}
                    <span className="font-medium text-foreground">{activeProvider.Name}</span>{" "}
                    ({PROVIDER_TYPE_LABEL[activeProvider.Type]})
                  </>
                ) : (
                  <span className="inline-flex items-center gap-1 text-warning">
                    <AlertTriangle className="h-3 w-3" />
                    未启用任何 CDN 配置, 无法触发清理
                  </span>
                )}
              </CardDescription>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                onClick={purgeCurrentHost}
                disabled={!activeProvider || purging !== null}
              >
                <Eraser className="mr-2 h-4 w-4" />
                清理当前 host
              </Button>
              <Button
                variant="outline"
                onClick={() => setPurgeAllConfirmOpen(true)}
                disabled={!activeProvider || purging !== null}
              >
                <Eraser className="mr-2 h-4 w-4" />
                一键清理全部
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="urls" className="w-full">
            <TabsList className="grid w-full grid-cols-4">
              <TabsTrigger value="urls">按 URL</TabsTrigger>
              <TabsTrigger value="prefixes">按目录/前缀</TabsTrigger>
              <TabsTrigger value="hosts">按 Host</TabsTrigger>
              <TabsTrigger value="tags">按 Tag</TabsTrigger>
            </TabsList>

            <PurgeTab
              value="urls"
              placeholder={"https://example.com/file1.jpg\nhttps://example.com/file2.css"}
              text={purgeUrls}
              onText={setPurgeUrls}
              disabled={!activeProvider || purging !== null}
              loading={purging === "urls"}
              onSubmit={() => purgeListType("urls", purgeUrls)}
              submitLabel="清理指定 URL"
            />
            <PurgeTab
              value="prefixes"
              placeholder={"留空则清理当前站点根; 也可输入 /static/ 或完整 URL, 每行一个"}
              text={purgePrefixes}
              onText={setPurgePrefixes}
              disabled={!activeProvider || purging !== null}
              loading={purging === "prefixes"}
              onSubmit={() => purgeListType("prefixes", purgePrefixes)}
              submitLabel="清理指定前缀"
              hint="以 / 开头的相对路径会自动补全为当前 origin"
            />
            <PurgeTab
              value="hosts"
              placeholder={"留空则清理当前 host; 也可输入其他 host, 每行一个"}
              text={purgeHosts}
              onText={setPurgeHosts}
              disabled={!activeProvider || purging !== null}
              loading={purging === "hosts"}
              onSubmit={() => purgeListType("hosts", purgeHosts)}
              submitLabel="清理指定主机"
            />
            <PurgeTab
              value="tags"
              placeholder={"tag1\ntag2"}
              text={purgeTags}
              onText={setPurgeTags}
              disabled={!activeProvider || purging !== null}
              loading={purging === "tags"}
              onSubmit={() => purgeListType("tags", purgeTags)}
              submitLabel="清理指定标签"
            />
          </Tabs>
        </CardContent>
      </Card>

      <CDNProviderEditor
        open={editorOpen}
        onOpenChange={(open) => {
          setEditorOpen(open)
          if (!open) setEditingProvider(null)
        }}
        provider={editingProvider}
        isNew={editorIsNew}
        saving={saving}
        onChange={setEditingProvider}
        onSubmit={submitEditor}
      />

      <AlertDialog open={!!deletingId} onOpenChange={(open) => !open && setDeletingId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 CDN 配置?</AlertDialogTitle>
            <AlertDialogDescription>
              删除后无法恢复, 关联的凭据会一并从服务端 / D1 中移除。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={purgeAllConfirmOpen} onOpenChange={setPurgeAllConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>清理该域名下全部缓存?</AlertDialogTitle>
            <AlertDialogDescription>
              将使用 <span className="font-medium">{activeProvider?.Name}</span> 调用厂商接口, 清理整站缓存, 可能短期内增加回源压力。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                setPurgeAllConfirmOpen(false)
                await runPurge("all")
              }}
            >
              确认清理
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={resultDialogOpen} onOpenChange={setResultDialogOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>{lastResult?.success ? "清理成功" : "清理失败"}</DialogTitle>
            <DialogDescription>
              {lastResult?.success
                ? lastResult?.job_id
                  ? `任务 ID: ${lastResult.job_id}`
                  : "厂商已接收清理请求"
                : lastResult?.message || "未知错误"}
            </DialogDescription>
          </DialogHeader>
          <div className="rounded-md bg-muted p-3">
            <p className="mb-2 text-xs text-muted-foreground">原始响应</p>
            <pre className="max-h-[40vh] overflow-auto text-xs">
              {JSON.stringify(lastResult?.raw ?? lastResult, null, 2)}
            </pre>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ProviderRow 单条 provider 列表项
function ProviderRow({
  provider,
  saving,
  onToggle,
  onEdit,
  onDelete,
}: {
  provider: CDNProvider
  saving: boolean
  onToggle: (enabled: boolean) => void
  onEdit: () => void
  onDelete: () => void
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-card p-3">
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{provider.Name || "(未命名)"}</span>
          <Badge variant="outline">{PROVIDER_TYPE_LABEL[provider.Type]}</Badge>
          {provider.Enabled && (
            <Badge className="bg-success/15 text-success hover:bg-success/15">
              <CheckCircle2 className="mr-1 h-3 w-3" />
              启用中
            </Badge>
          )}
        </div>
        <div className="truncate font-mono text-xs text-muted-foreground">
          Zone: {provider.Credentials.zoneId || "(未设置)"}
        </div>
      </div>
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-2">
          <Switch checked={provider.Enabled} onCheckedChange={onToggle} disabled={saving} />
          <span className="text-xs text-muted-foreground">启用</span>
        </div>
        <Button variant="outline" size="sm" onClick={onEdit} disabled={saving}>
          <Edit className="mr-1 h-3 w-3" />
          编辑
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="text-destructive hover:text-destructive"
          onClick={onDelete}
          disabled={saving}
        >
          <Trash2 className="mr-1 h-3 w-3" />
          删除
        </Button>
      </div>
    </div>
  )
}

// PurgeTab 渲染单个 purge 子页 (textarea + 提交按钮 + 可选提示)
function PurgeTab({
  value,
  placeholder,
  text,
  onText,
  disabled,
  loading,
  onSubmit,
  submitLabel,
  hint,
}: {
  value: string
  placeholder: string
  text: string
  onText: (v: string) => void
  disabled: boolean
  loading: boolean
  onSubmit: () => void
  submitLabel: string
  hint?: string
}) {
  return (
    <TabsContent value={value} className="space-y-3 pt-3">
      <Textarea
        placeholder={placeholder}
        value={text}
        onChange={(e) => onText(e.target.value)}
        className="min-h-[140px] font-mono text-sm"
      />
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      <Button className="w-full" disabled={disabled} onClick={onSubmit}>
        {loading ? "清理中..." : submitLabel}
      </Button>
    </TabsContent>
  )
}
