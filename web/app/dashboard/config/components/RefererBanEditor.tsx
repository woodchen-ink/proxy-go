import React, { useEffect, useState } from "react"
import { Plus, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { fetchApi } from "@/lib/api-client"
import { cn } from "@/lib/utils"

// RefererRule 一套引用来源规则 (与后端 RefererBanConfig 的规则字段对齐)
// Mode 为 "whitelist" 时只放行命中 Hosts 的 Referer, 其余 (含未知 host) 一律拒绝; 省略或 "blacklist" 时为传统黑名单
export interface RefererRule {
  Enabled: boolean
  Mode?: "blacklist" | "whitelist"
  Hosts: string[]
  BlockEmpty: boolean
}

// RefererNodeOverride 只对 Nodes 中的节点生效的规则, 命中时整体替换默认规则 (先命中先用)
export interface RefererNodeOverride extends RefererRule {
  Nodes: string[]
}

export interface RefererBanConfig extends RefererRule {
  NodeOverrides?: RefererNodeOverride[]
}

interface NodeOption {
  id: string
  label: string
}

// 节点列表整页共用一次请求
let nodeOptionsPromise: Promise<NodeOption[]> | null = null
function loadNodeOptions(): Promise<NodeOption[]> {
  nodeOptionsPromise ??= fetchApi("/admin/api/hub/nodes")
    .then((r) => (r.ok ? r.json() : { nodes: [] }))
    .then((d: { nodes?: { node_id: string; note?: string }[] }) =>
      (d.nodes ?? []).map((n) => ({ id: n.node_id, label: n.note ? `${n.node_id} · ${n.note}` : n.node_id })),
    )
    .catch(() => {
      nodeOptionsPromise = null
      return []
    })
  return nodeOptionsPromise
}

function cleanHost(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, "")
    .replace(/^\*\./, "")
    .replace(/[\/:?#].*$/, "")
    .replace(/\.+$/, "")
}

// RuleFields 一套规则的开关与名单, 默认规则与节点覆盖共用
function RuleFields({
  rule,
  onChange,
  allowWhitelist,
  emptyHint,
  title,
  description,
}: {
  rule: RefererRule
  onChange: (next: RefererRule) => void
  allowWhitelist?: boolean
  emptyHint?: string
  title: React.ReactNode
  description?: string
}) {
  const [draft, setDraft] = useState("")
  const hosts = Array.isArray(rule.Hosts) ? rule.Hosts : []
  const isWhitelist = allowWhitelist === true && rule.Mode === "whitelist"
  const update = (patch: Partial<RefererRule>) => onChange({ ...rule, Hosts: hosts, ...patch })

  const addHost = () => {
    const h = cleanHost(draft)
    setDraft("")
    if (h && !hosts.includes(h)) update({ Hosts: [...hosts, h] })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <Label className="font-semibold">{title}</Label>
          <p className="text-sm text-foreground-muted">
            {description ??
              (isWhitelist ? "仅放行命中名单的 Referer, 其余一律 403, 在缓存检查之前拦截" : "Referer host 命中名单时直接 403, 在缓存检查之前拦截")}
          </p>
        </div>
        <Switch checked={rule.Enabled} onCheckedChange={(checked) => update({ Enabled: checked })} />
      </div>

      {rule.Enabled && (
        <>
          {allowWhitelist && (
            <div className="flex items-center justify-between gap-4">
              <div>
                <Label>白名单模式</Label>
                <p className="text-sm text-foreground-muted">只有命中下方 host 名单的 Referer 才放行, 用于资源仅供自己站点引用的场景</p>
              </div>
              <Switch checked={isWhitelist} onCheckedChange={(checked) => update({ Mode: checked ? "whitelist" : "blacklist" })} />
            </div>
          )}

          <div className="flex items-center justify-between gap-4">
            <div>
              <Label>拦截空 Referer</Label>
              <p className="text-sm text-foreground-muted">默认放行 (避免误伤 curl / 直接访问 / Telegram 预览), 资源仅供站内引用时开启</p>
            </div>
            <Switch checked={rule.BlockEmpty} onCheckedChange={(checked) => update({ BlockEmpty: checked })} />
          </div>

          <div className="space-y-2">
            <Label>{isWhitelist ? "允许的 host" : "禁止的 host"}</Label>
            <div className="flex gap-2">
              <Input
                value={draft}
                placeholder={isWhitelist ? "example.com (含 *.example.com 子域)" : "bad.com (含 *.bad.com 子域)"}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault()
                    addHost()
                  }
                }}
              />
              <Button type="button" onClick={addHost} variant="outline">
                添加
              </Button>
            </div>
            {hosts.length > 0 ? (
              <div className="flex flex-wrap gap-2 pt-1">
                {hosts.map((h) => (
                  <Badge key={h} variant="outline" className="gap-1 font-mono">
                    {h}
                    <button
                      type="button"
                      onClick={() => update({ Hosts: hosts.filter((x) => x !== h) })}
                      className="ml-1 text-foreground-muted hover:text-destructive"
                      aria-label={`移除 ${h}`}
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            ) : (
              <p className="text-sm text-foreground-subtle">
                {isWhitelist ? "名单为空: 拒绝所有带 Referer 的请求" : (emptyHint ?? "尚未添加 host")}
              </p>
            )}
          </div>
        </>
      )}
    </div>
  )
}

interface RefererBanEditorProps {
  config: RefererBanConfig
  onUpdate: (next: RefererBanConfig) => void
  // emptyHint 自定义"尚未添加 host"的提示文案; 路径级和全局可以用不同语境
  emptyHint?: string
  // allowWhitelist 是否展示黑/白名单切换; 全局安全策略场景传 false, 仅路径级支持白名单
  allowWhitelist?: boolean
}

// RefererBanEditor 引用来源黑/白名单编辑器, 全局和路径级共用
// - 默认规则对所有节点生效; "按节点覆盖"里每条指定若干节点, 命中的节点整体改用该条规则 (先命中先用)
// - 关闭 Enabled 时不清空 Hosts, 仅停止生效
// - 白名单模式下 Hosts 语义反转为"仅允许", BlockEmpty 语义不变 (是否拒绝空 Referer)
export default function RefererBanEditor({ config, onUpdate, emptyHint, allowWhitelist }: RefererBanEditorProps) {
  const [nodes, setNodes] = useState<NodeOption[]>([])
  useEffect(() => {
    loadNodeOptions().then(setNodes)
  }, [])

  const overrides = config.NodeOverrides ?? []
  const label = allowWhitelist && config.Mode === "whitelist" ? "白名单" : "黑名单"
  const setOverrides = (next: RefererNodeOverride[]) => onUpdate({ ...config, NodeOverrides: next })
  const patchOverride = (i: number, next: RefererNodeOverride) => setOverrides(overrides.map((o, j) => (j === i ? next : o)))

  // 同一节点只应出现在一条覆盖里 (多条命中时只有第一条生效), 已被前面占用的节点标出来
  const claimedBefore = (i: number) => new Set(overrides.slice(0, i).flatMap((o) => o.Nodes ?? []))

  return (
    <div className="space-y-5">
      <RuleFields
        rule={config}
        onChange={(r) => onUpdate({ ...config, ...r })}
        allowWhitelist={allowWhitelist}
        emptyHint={emptyHint}
        title={`Referer ${label}`}
      />

      <div className="space-y-3 border-t pt-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <span className="text-sm font-semibold">
              按节点覆盖 <span className="font-normal text-foreground-subtle tabular-nums">{overrides.length}</span>
            </span>
            <p className="text-sm text-foreground-muted">选中的节点改用这一套规则, 不再走上面的默认规则; 其他节点不受影响</p>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() =>
              setOverrides([...overrides, { Nodes: [], Enabled: true, Mode: config.Mode ?? "blacklist", Hosts: [...(config.Hosts ?? [])], BlockEmpty: config.BlockEmpty }])
            }
          >
            <Plus className="h-4 w-4" />
            添加
          </Button>
        </div>

        {overrides.map((o, i) => {
          const picked = new Set(o.Nodes ?? [])
          const claimed = claimedBefore(i)
          // 已登记过但不在当前节点列表里的 id (节点被删除 / 改名) 也要能看到并取消
          const options = [...nodes, ...[...picked].filter((id) => !nodes.some((n) => n.id === id)).map((id) => ({ id, label: `${id} (已不在节点列表)` }))]
          return (
            <div key={i} className="space-y-4 rounded-lg border p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="space-y-2">
                  <Label>生效节点</Label>
                  {options.length === 0 ? (
                    <p className="text-sm text-foreground-subtle">还没有节点接入中控</p>
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {options.map((n) => {
                        const on = picked.has(n.id)
                        const shadowed = !on && claimed.has(n.id)
                        return (
                          <button
                            key={n.id}
                            type="button"
                            aria-pressed={on}
                            title={shadowed ? "该节点已在前面的覆盖里, 这里选了也不会生效" : undefined}
                            onClick={() =>
                              patchOverride(i, { ...o, Nodes: on ? (o.Nodes ?? []).filter((x) => x !== n.id) : [...(o.Nodes ?? []), n.id] })
                            }
                            className={cn(
                              "h-7 rounded-full border px-3 text-sm transition-colors",
                              on ? "border-primary bg-primary text-primary-foreground" : "text-foreground-muted hover:bg-accent hover:text-foreground",
                              shadowed && "opacity-50",
                            )}
                          >
                            {n.label}
                          </button>
                        )
                      })}
                    </div>
                  )}
                  {picked.size === 0 && <p className="text-sm text-warning">未选节点, 这条覆盖不会生效</p>}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 shrink-0 text-destructive hover:text-destructive"
                  aria-label="删除这条覆盖"
                  onClick={() => setOverrides(overrides.filter((_, j) => j !== i))}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
              <RuleFields
                rule={o}
                onChange={(r) => patchOverride(i, { ...o, ...r })}
                allowWhitelist={allowWhitelist}
                title="在这些节点上启用"
                description="关闭则这些节点不做 Referer 检查 (即使默认规则开着)"
              />
            </div>
          )
        })}
      </div>
    </div>
  )
}
