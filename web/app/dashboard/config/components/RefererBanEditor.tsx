import React, { useEffect, useState } from "react"
import { Plus, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { HelpTip } from "@/components/help-tip"
import { fetchApi } from "@/lib/api-client"
import { cn } from "@/lib/utils"

// RefererRule 一套引用来源规则 (与后端 RefererBanConfig 的规则字段对齐)
// Mode 为 "whitelist" 时只放行命中 Hosts 的 Referer; 省略或 "blacklist" 时为黑名单
export interface RefererRule {
  Enabled: boolean
  Mode?: "blacklist" | "whitelist"
  Hosts: string[]
  BlockEmpty: boolean
}

// RefererNodeOverride 例外规则 (JSON 字段名沿用 NodeOverrides): 用子路径和 / 或节点限定范围,
// 请求落在范围内时整体替换默认规则, 先命中先用; 两者都为空的条目不生效
export interface RefererNodeOverride extends RefererRule {
  Nodes: string[]
  Paths?: string[]
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

// InlineSwitch 紧凑开关: 标签 + 问号说明 + 开关, 多个并排成一行
function InlineSwitch({ label, help, checked, onChange }: { label: string; help: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="inline-flex h-8 items-center gap-2 text-sm">
      <span className="text-foreground-muted">{label}</span>
      <HelpTip>{help}</HelpTip>
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  )
}

// ChipInput 输入后回车添加的标签列表 (host / 子路径共用)
function ChipInput({
  values,
  onChange,
  placeholder,
  clean,
  mono = true,
}: {
  values: string[]
  onChange: (next: string[]) => void
  placeholder: string
  clean: (raw: string) => string
  mono?: boolean
}) {
  const [draft, setDraft] = useState("")
  const add = () => {
    const v = clean(draft)
    setDraft("")
    if (v && !values.includes(v)) onChange([...values, v])
  }
  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <Input
          value={draft}
          placeholder={placeholder}
          className={cn("h-9", mono && "font-mono")}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              add()
            }
          }}
        />
        <Button type="button" variant="outline" size="sm" className="h-9" onClick={add}>
          添加
        </Button>
      </div>
      {values.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {values.map((v) => (
            <span key={v} className={cn("inline-flex h-7 items-center gap-1 rounded-md border bg-card pl-2 pr-1 text-sm", mono && "font-mono")}>
              {v}
              <button
                type="button"
                onClick={() => onChange(values.filter((x) => x !== v))}
                className="rounded p-0.5 text-foreground-subtle hover:bg-accent hover:text-destructive"
                aria-label={`移除 ${v}`}
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

// RuleBody 一套规则的选项与 host 名单 (启用开关由调用方放在标题行)
function RuleBody({ rule, onChange, allowWhitelist }: { rule: RefererRule; onChange: (next: RefererRule) => void; allowWhitelist?: boolean }) {
  const hosts = Array.isArray(rule.Hosts) ? rule.Hosts : []
  const isWhitelist = allowWhitelist === true && rule.Mode === "whitelist"
  const update = (patch: Partial<RefererRule>) => onChange({ ...rule, Hosts: hosts, ...patch })
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-x-6">
        {allowWhitelist && (
          <InlineSwitch
            label="白名单"
            help="只有命中下方名单的 Referer 才放行, 其余 (含无法识别的) 一律 403; 名单为空时拒绝所有带 Referer 的请求。关闭时为黑名单: 命中名单才拒绝。"
            checked={isWhitelist}
            onChange={(v) => update({ Mode: v ? "whitelist" : "blacklist" })}
          />
        )}
        <InlineSwitch
          label="拦截空 Referer"
          help="没有 Referer 的请求 (直接打开链接、curl、部分 App / Telegram 预览) 也拒绝。资源只给自己站点引用时开启。"
          checked={rule.BlockEmpty}
          onChange={(v) => update({ BlockEmpty: v })}
        />
      </div>
      <ChipInput
        values={hosts}
        onChange={(next) => update({ Hosts: next })}
        clean={cleanHost}
        placeholder={isWhitelist ? "允许的 host, 如 example.com (含子域)" : "禁止的 host, 如 bad.com (含子域)"}
      />
    </div>
  )
}

interface RefererBanEditorProps {
  config: RefererBanConfig
  onUpdate: (next: RefererBanConfig) => void
  // allowWhitelist 是否支持白名单; 全局规则恒为黑名单
  allowWhitelist?: boolean
  // pathPrefix 当前路径映射前缀, 用于子路径输入的提示与补全; 全局规则不传
  pathPrefix?: string
  emptyHint?: string
}

// RefererBanEditor 引用来源规则编辑器, 全局和路径级共用: 默认规则 + 例外规则 (子路径 / 节点)
export default function RefererBanEditor({ config, onUpdate, allowWhitelist, pathPrefix }: RefererBanEditorProps) {
  const [nodes, setNodes] = useState<NodeOption[]>([])
  useEffect(() => {
    loadNodeOptions().then(setNodes)
  }, [])

  const overrides = config.NodeOverrides ?? []
  const setOverrides = (next: RefererNodeOverride[]) => onUpdate({ ...config, NodeOverrides: next })
  const patch = (i: number, next: RefererNodeOverride) => setOverrides(overrides.map((o, j) => (j === i ? next : o)))
  const label = allowWhitelist && config.Mode === "whitelist" ? "白名单" : "黑名单"

  // 子路径: 只填后半段时补上当前映射前缀; 统一以 / 开头
  const cleanPath = (raw: string) => {
    let p = raw.trim().replace(/^https?:\/\/[^/]+/, "")
    if (!p) return ""
    if (!p.startsWith("/")) p = "/" + p
    if (pathPrefix && pathPrefix !== "/" && !p.startsWith(pathPrefix + "/") && p !== pathPrefix && !p.startsWith(pathPrefix + "*")) {
      p = pathPrefix + p
    }
    return p
  }

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-semibold">Referer {label}</span>
            <HelpTip>
              {allowWhitelist
                ? "按引用来源 host 放行或拒绝, 命中时直接 403, 在缓存检查之前拦截。host 按后缀匹配: bad.com 同时匹配 a.bad.com, 但不匹配 evilbad.com。"
                : "全局规则, 对所有路径生效, 恒为黑名单。单个路径可在「路径映射」里另设规则。"}
            </HelpTip>
          </div>
          <Switch checked={config.Enabled} onCheckedChange={(v) => onUpdate({ ...config, Enabled: v })} />
        </div>
        {config.Enabled && <RuleBody rule={config} onChange={(r) => onUpdate({ ...config, ...r })} allowWhitelist={allowWhitelist} />}
      </div>

      <div className="space-y-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-semibold">例外规则</span>
            <span className="text-sm text-foreground-subtle tabular-nums">{overrides.length}</span>
            <HelpTip>
              对某些子路径 (如 /b2/random-video*) 或某些节点单独设一套规则, 命中范围的请求改用这一套, 不再走上面的默认规则; 按顺序先命中先用。子路径和节点都不填的规则不生效。
            </HelpTip>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() =>
              setOverrides([
                ...overrides,
                { Nodes: [], Paths: [], Enabled: true, Mode: allowWhitelist ? "whitelist" : "blacklist", Hosts: [], BlockEmpty: false },
              ])
            }
          >
            <Plus className="h-4 w-4" />
            添加
          </Button>
        </div>

        {overrides.map((o, i) => {
          const paths = o.Paths ?? []
          const picked = new Set(o.Nodes ?? [])
          const options = [...nodes, ...[...picked].filter((id) => !nodes.some((n) => n.id === id)).map((id) => ({ id, label: `${id} (已不在节点列表)` }))]
          const scoped = paths.length > 0 || picked.size > 0
          return (
            <div key={i} className="space-y-3 rounded-lg border bg-card p-3">
              <div className="flex items-center justify-between gap-3">
                <span className="text-sm font-medium">
                  例外 {i + 1}
                  {!scoped && <span className="ml-2 font-normal text-warning">未设范围, 不生效</span>}
                </span>
                <div className="flex items-center gap-2">
                  <label className="inline-flex items-center gap-2 text-sm text-foreground-muted">
                    检查 Referer
                    <Switch checked={o.Enabled} onCheckedChange={(v) => patch(i, { ...o, Enabled: v })} />
                  </label>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8 text-destructive hover:text-destructive"
                    aria-label={`删除例外 ${i + 1}`}
                    onClick={() => setOverrides(overrides.filter((_, j) => j !== i))}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>

              <div className="grid gap-3 lg:grid-cols-2">
                <div className="space-y-1.5">
                  <div className="flex items-center gap-1.5 text-sm text-foreground-muted">
                    子路径
                    <HelpTip>末尾 * 为前缀匹配: /b2/random-video* 匹配 /b2/random-video.mp4 和 /b2/random-video/1.mp4。不带 * 时匹配该路径及其下级。不填 = 不限路径。</HelpTip>
                  </div>
                  <ChipInput
                    values={paths}
                    onChange={(next) => patch(i, { ...o, Paths: next })}
                    clean={cleanPath}
                    placeholder={pathPrefix && pathPrefix !== "/" ? `${pathPrefix}/random-video*` : "/b2/random-video*"}
                  />
                </div>
                <div className="space-y-1.5">
                  <div className="flex items-center gap-1.5 text-sm text-foreground-muted">
                    节点
                    <HelpTip>只在选中的节点上生效; 不选 = 全部节点。</HelpTip>
                  </div>
                  {options.length === 0 ? (
                    <p className="text-sm text-foreground-subtle">还没有节点接入中控</p>
                  ) : (
                    <div className="flex flex-wrap gap-1.5">
                      {options.map((n) => {
                        const on = picked.has(n.id)
                        return (
                          <button
                            key={n.id}
                            type="button"
                            aria-pressed={on}
                            onClick={() => patch(i, { ...o, Nodes: on ? (o.Nodes ?? []).filter((x) => x !== n.id) : [...(o.Nodes ?? []), n.id] })}
                            className={cn(
                              "h-7 rounded-full border px-3 text-sm transition-colors",
                              on ? "border-primary bg-primary text-primary-foreground" : "text-foreground-muted hover:bg-accent hover:text-foreground",
                            )}
                          >
                            {n.label}
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>

              {o.Enabled ? (
                <RuleBody rule={o} onChange={(r) => patch(i, { ...o, ...r })} allowWhitelist={allowWhitelist} />
              ) : (
                <p className="text-sm text-foreground-subtle">范围内的请求不做 Referer 检查 (即使默认规则开着)</p>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
