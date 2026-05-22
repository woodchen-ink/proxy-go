import React, { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Trash2 } from "lucide-react"

// RefererBanConfig 引用来源 host 黑名单 (与后端 schema 对齐)
export interface RefererBanConfig {
  Enabled: boolean
  Hosts: string[]
  BlockEmpty: boolean
}

interface RefererBanEditorProps {
  config: RefererBanConfig
  onUpdate: (next: RefererBanConfig) => void
  // emptyHint 自定义"尚未添加 host"的提示文案; 路径级和全局可以用不同语境
  emptyHint?: string
}

// RefererBanEditor 引用来源黑名单编辑器, 全局和路径级共用
// 行为契约:
// - host 输入按 Enter 添加, 重复条目自动忽略
// - 粘贴形式 (https://x.com:8080/) 在前端做基础清洗, 后端 Compile 会再 normalize 一次
// - 关闭 Enabled 时不清空 Hosts, 仅停止生效, 方便临时关闭再恢复
export default function RefererBanEditor({ config, onUpdate, emptyHint }: RefererBanEditorProps) {
  const [draftHost, setDraftHost] = useState("")

  const update = (patch: Partial<RefererBanConfig>) => {
    onUpdate({ ...config, ...patch })
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
    if (config.Hosts.includes(cleaned)) {
      setDraftHost("")
      return
    }
    update({ Hosts: [...config.Hosts, cleaned] })
    setDraftHost("")
  }

  const removeHost = (h: string) => {
    update({ Hosts: config.Hosts.filter((x) => x !== h) })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Label>启用 Referer 黑名单</Label>
          <p className="text-sm text-muted-foreground">
            Referer host 命中名单时直接 403, 在缓存检查之前拦截
          </p>
        </div>
        <Switch
          checked={config.Enabled}
          onCheckedChange={(checked) => update({ Enabled: checked })}
        />
      </div>

      {config.Enabled && (
        <>
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
            <Label>禁止的 host</Label>
            <div className="flex gap-2">
              <Input
                value={draftHost}
                placeholder="bad.com (自动包含 *.bad.com 子域)"
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
              匹配按 host 后缀语义 (bad.com 同时拦截 foo.bad.com / a.b.bad.com),
              但不会误伤 evilbad.com
            </p>

            {config.Hosts.length > 0 ? (
              <div className="flex flex-wrap gap-2 pt-2">
                {config.Hosts.map((h) => (
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
                {emptyHint ?? "尚未添加任何 host"}
              </p>
            )}
          </div>
        </>
      )}
    </div>
  )
}
