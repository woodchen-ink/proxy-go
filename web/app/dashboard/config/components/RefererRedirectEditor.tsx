import React, { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Plus, Trash2 } from "lucide-react"

// RefererRedirectRule 单条重定向规则 (与后端 schema 对齐)
export interface RefererRedirectRule {
  Hosts: string[]
  Target: string
}

// RefererRedirectConfig 路径级 Referer 302 重定向配置 (与后端 schema 对齐)
export interface RefererRedirectConfig {
  Enabled: boolean
  Rules: RefererRedirectRule[]
}

interface RefererRedirectEditorProps {
  config: RefererRedirectConfig
  onUpdate: (next: RefererRedirectConfig) => void
}

// cleanHost 与后端 Compile 一致地做基础归一 (去 scheme / 通配符 / 端口 / 路径), 后端会再 normalize 一次
function cleanHost(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, "")
    .replace(/^\*\./, "")
    .replace(/[\/:?#].*$/, "")
    .replace(/\.+$/, "")
}

// RefererRedirectEditor 路径级 Referer 重定向编辑器
// 行为契约:
// - 多组规则按顺序匹配, 第一组命中的决定目标 (先命中先跳), 与后端一致
// - 每组 = 一组 host 名单 (Enter 添加, 重复忽略) + 一个目标前缀
// - 命中重定向优先于 RefererBan; 想让某 host 走重定向时, 不要同时把它加进黑名单
// - 关闭 Enabled 时不清空规则, 仅停止生效
export default function RefererRedirectEditor({ config, onUpdate }: RefererRedirectEditorProps) {
  // Rules 在 D1 / JSON 反序列化后可能为 null, 统一归一为 []
  const rules = Array.isArray(config.Rules) ? config.Rules : []
  // 每组规则各自维护一个 host 草稿输入
  const [draftHosts, setDraftHosts] = useState<Record<number, string>>({})

  const updateRules = (nextRules: RefererRedirectRule[]) => {
    onUpdate({ ...config, Rules: nextRules })
  }

  const addRule = () => {
    updateRules([...rules, { Hosts: [], Target: "" }])
  }

  const removeRule = (idx: number) => {
    updateRules(rules.filter((_, i) => i !== idx))
  }

  const setRuleTarget = (idx: number, target: string) => {
    updateRules(rules.map((r, i) => (i === idx ? { ...r, Target: target } : r)))
  }

  const addHost = (idx: number) => {
    const cleaned = cleanHost(draftHosts[idx] ?? "")
    setDraftHosts((d) => ({ ...d, [idx]: "" }))
    if (!cleaned) return
    const rule = rules[idx]
    const hosts = Array.isArray(rule.Hosts) ? rule.Hosts : []
    if (hosts.includes(cleaned)) return
    updateRules(rules.map((r, i) => (i === idx ? { ...r, Hosts: [...hosts, cleaned] } : r)))
  }

  const removeHost = (idx: number, host: string) => {
    const hosts = Array.isArray(rules[idx].Hosts) ? rules[idx].Hosts : []
    updateRules(
      rules.map((r, i) => (i === idx ? { ...r, Hosts: hosts.filter((h) => h !== host) } : r))
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label>启用 Referer 重定向</Label>
          <p className="text-sm text-muted-foreground">
            Referer host 命中规则时 302 分流到另一个目标前缀 (换 host 去前缀, 保留子路径与 query),
            在缓存检查之前生效, 优先于黑名单
          </p>
        </div>
        <Switch
          checked={config.Enabled}
          onCheckedChange={(checked) => onUpdate({ ...config, Rules: rules, Enabled: checked })}
        />
      </div>

      {config.Enabled && (
        <div className="space-y-4">
          {rules.map((rule, idx) => {
            const hosts = Array.isArray(rule.Hosts) ? rule.Hosts : []
            return (
              <div key={idx} className="space-y-3 rounded-lg border border-border p-3">
                <div className="flex items-center justify-between">
                  <Label className="text-sm">规则 {idx + 1}</Label>
                  <button
                    type="button"
                    onClick={() => removeRule(idx)}
                    className="text-muted-foreground hover:text-destructive"
                    aria-label={`删除规则 ${idx + 1}`}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>

                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">目标前缀</Label>
                  <Input
                    value={rule.Target}
                    placeholder="https://cdn2.example.com"
                    onChange={(e) => setRuleTarget(idx, e.target.value)}
                  />
                </div>

                <div className="space-y-1.5">
                  <Label className="text-xs text-muted-foreground">命中的 host</Label>
                  <div className="flex gap-2">
                    <Input
                      value={draftHosts[idx] ?? ""}
                      placeholder="site.com (自动包含 *.site.com 子域)"
                      onChange={(e) =>
                        setDraftHosts((d) => ({ ...d, [idx]: e.target.value }))
                      }
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault()
                          addHost(idx)
                        }
                      }}
                    />
                    <Button type="button" onClick={() => addHost(idx)} variant="outline">
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
                            onClick={() => removeHost(idx, h)}
                            className="ml-1 text-muted-foreground hover:text-destructive"
                            aria-label={`移除 ${h}`}
                          >
                            <Trash2 className="h-3 w-3" />
                          </button>
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      尚未添加 host, 该规则不会生效
                    </p>
                  )}
                </div>
              </div>
            )
          })}

          <Button type="button" variant="outline" size="sm" onClick={addRule} className="gap-1">
            <Plus className="h-4 w-4" />
            添加规则
          </Button>
          <p className="text-xs text-muted-foreground">
            多组规则按顺序匹配, 第一条命中的决定目标。host 走后缀语义 (site.com 同时命中 a.site.com),
            空 Referer 不分流
          </p>
        </div>
      )}
    </div>
  )
}
