import React, { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Trash2 } from "lucide-react"

// RefererBanConfig 引用来源 host 黑/白名单 (与后端 schema 对齐)
// Mode 为 "whitelist" 时只放行命中 Hosts 的 Referer, 其余 (含未知 host) 一律拒绝; 省略或 "blacklist" 时为传统黑名单
export interface RefererBanConfig {
  Enabled: boolean
  Mode?: "blacklist" | "whitelist"
  Hosts: string[]
  BlockEmpty: boolean
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
// 行为契约:
// - host 输入按 Enter 添加, 重复条目自动忽略
// - 粘贴形式 (https://x.com:8080/) 在前端做基础清洗, 后端 Compile 会再 normalize 一次
// - 关闭 Enabled 时不清空 Hosts, 仅停止生效, 方便临时关闭再恢复
// - 白名单模式下 Hosts 语义反转为"仅允许", BlockEmpty 语义不变 (是否拒绝空 Referer)
export default function RefererBanEditor({ config, onUpdate, emptyHint, allowWhitelist }: RefererBanEditorProps) {
  const [draftHost, setDraftHost] = useState("")

  // Hosts 在 D1 / JSON 反序列化后可能是 null (空数组序列化为 null 的常见副作用), 统一归一为 []
  const hosts = Array.isArray(config.Hosts) ? config.Hosts : []
  const isWhitelist = allowWhitelist === true && config.Mode === "whitelist"

  const update = (patch: Partial<RefererBanConfig>) => {
    onUpdate({ ...config, Hosts: hosts, ...patch })
  }

  const addHost = () => {
    const raw = draftHost.trim().toLowerCase()
    if (!raw) return
    const cleaned = raw
      .replace(/^https?:\/\//, "")
      .replace(/^\*\./, "")
      .replace(/[\/:?#].*$/, "")
      .replace(/\.+$/, "")
    if (!cleaned) return
    if (hosts.includes(cleaned)) {
      setDraftHost("")
      return
    }
    update({ Hosts: [...hosts, cleaned] })
    setDraftHost("")
  }

  const removeHost = (h: string) => {
    update({ Hosts: hosts.filter((x) => x !== h) })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label>启用 Referer {isWhitelist ? "白名单" : "黑名单"}</Label>
          <p className="text-sm text-muted-foreground">
            {isWhitelist
              ? "仅放行命中名单的 Referer, 其余一律 403, 在缓存检查之前拦截"
              : "Referer host 命中名单时直接 403, 在缓存检查之前拦截"}
          </p>
        </div>
        <Switch
          checked={config.Enabled}
          onCheckedChange={(checked) => update({ Enabled: checked })}
        />
      </div>

      {config.Enabled && (
        <>
          {allowWhitelist && (
            <div className="flex items-center justify-between">
              <div>
                <Label>白名单模式</Label>
                <p className="text-sm text-muted-foreground">
                  开启后语义反转: 只有命中下方 host 名单的 Referer 才放行, 用于资源仅供自己站点引用的强隔离场景
                </p>
              </div>
              <Switch
                checked={isWhitelist}
                onCheckedChange={(checked) =>
                  update({ Mode: checked ? "whitelist" : "blacklist" })
                }
              />
            </div>
          )}

          <div className="flex items-center justify-between">
            <div>
              <Label>同时拦截空 Referer</Label>
              <p className="text-sm text-muted-foreground">
                默认放行 (避免误伤 curl / 直接访问 / Telegram 预览). 仅在资源仅供站内引用时开启
              </p>
            </div>
            <Switch
              checked={config.BlockEmpty}
              onCheckedChange={(checked) => update({ BlockEmpty: checked })}
            />
          </div>

          <div className="space-y-2">
            <Label>{isWhitelist ? "允许的 host" : "禁止的 host"}</Label>
            <div className="flex gap-2">
              <Input
                value={draftHost}
                placeholder={
                  isWhitelist
                    ? "example.com (自动包含 *.example.com 子域)"
                    : "bad.com (自动包含 *.bad.com 子域)"
                }
                onChange={(e) => setDraftHost(e.target.value)}
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
            <p className="text-xs text-muted-foreground">
              {isWhitelist
                ? "匹配按 host 后缀语义 (example.com 同时放行 foo.example.com / a.b.example.com), 但不会误放 evilexample.com; 名单为空时拒绝所有带 Referer 的请求"
                : "匹配按 host 后缀语义 (bad.com 同时拦截 foo.bad.com / a.b.bad.com), 但不会误伤 evilbad.com"}
            </p>

            {hosts.length > 0 ? (
              <div className="flex flex-wrap gap-2 pt-2">
                {hosts.map((h) => (
                  <Badge key={h} variant="outline" className="gap-1 font-mono">
                    {h}
                    <button
                      type="button"
                      onClick={() => removeHost(h)}
                      className="ml-1 text-muted-foreground hover:text-destructive"
                      aria-label={`移除 ${h}`}
                    >
                      <Trash2 className="h-3 w-3" />
                    </button>
                  </Badge>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                {isWhitelist
                  ? (emptyHint ?? "尚未添加任何 host, 开启白名单后将拒绝所有带 Referer 的请求")
                  : (emptyHint ?? "尚未添加任何 host")}
              </p>
            )}
          </div>
        </>
      )}
    </div>
  )
}
