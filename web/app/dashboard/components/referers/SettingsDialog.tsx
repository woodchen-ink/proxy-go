"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import type { RefererSettings } from "./types"

interface Props {
  open: boolean
  settings: RefererSettings
  saving: boolean
  onClose: () => void
  onSave: (patch: { own_hosts: string[]; alert_threshold: number }) => void
}

// SettingsDialog 自有域名 (后缀匹配, 不告警、单独归类) 与新来源告警阈值
export default function SettingsDialog({ open, settings, saving, onClose, onSave }: Props) {
  const [own, setOwn] = useState("")
  const [threshold, setThreshold] = useState("")

  useEffect(() => {
    if (!open) return
    setOwn(settings.own_hosts.join("\n"))
    setThreshold(String(settings.alert_threshold))
  }, [open, settings])

  const submit = () => {
    const hosts = own
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    onSave({ own_hosts: hosts, alert_threshold: Math.max(0, parseInt(threshold, 10) || 0) })
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>引用来源设置</DialogTitle>
          <DialogDescription>自有域名不参与新来源告警, 在列表里单独归类。条目按后缀匹配, czl.net 同时覆盖它的全部子域。</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="referer-own">自有域名 (每行一个)</Label>
            <Textarea id="referer-own" rows={6} value={own} onChange={(e) => setOwn(e.target.value)} className="font-mono text-sm" placeholder={"woodchen.ink\nsunai.net"} />
            {settings.auto_own_hosts.length > 0 && (
              <p className="text-xs text-foreground-subtle">已从 DNS 域名自动带出: {settings.auto_own_hosts.join(", ")}</p>
            )}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="referer-threshold">新来源告警阈值</Label>
            <Input id="referer-threshold" type="number" min={0} value={threshold} onChange={(e) => setThreshold(e.target.value)} className="w-40" />
            <p className="text-xs text-foreground-subtle">首次出现的外部来源近两天请求达到该值时推企业微信, 每个来源只推一次; 每小时检查一次, 填 0 关闭。</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={submit} disabled={saving}>
            {saving ? "保存中..." : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
