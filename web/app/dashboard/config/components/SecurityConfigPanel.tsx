import React from "react"
import Link from "next/link"
import { Plus, Shield, Trash2 } from "lucide-react"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { HelpTip } from "@/components/help-tip"
import RefererBanEditor, { RefererBanConfig } from "./RefererBanEditor"
import type { SecurityConfig, TrustedProxy } from "../types"

type IPBanConfig = SecurityConfig["IPBan"]

interface SecurityConfigPanelProps {
  config: SecurityConfig | null
  onUpdate: (next: SecurityConfig) => void
}

const DEFAULT_IPBAN: IPBanConfig = { Enabled: false, ErrorThreshold: 10, WindowMinutes: 5, BanDurationMinutes: 5, CleanupIntervalMinutes: 1 }
const DEFAULT_REFERER: RefererBanConfig = { Enabled: false, Hosts: [], BlockEmpty: false }
// 与节点 config.DefaultTrustedProxies 一致
const DEFAULT_TRUSTED: TrustedProxy[] = [{ Name: "EdgeOne", Header: "EO-Client-IP", CIDRs: [] }]

const CIDR_RE = /^[0-9a-fA-F:.]+(\/\d{1,3})?$/

function Section({ title, help, actions, children }: { title: string; help: React.ReactNode; actions?: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center justify-between gap-3 border-b px-5 py-3">
        <div className="flex items-center gap-1.5">
          <h2 className="text-base font-semibold">{title}</h2>
          <HelpTip>{help}</HelpTip>
        </div>
        {actions}
      </div>
      <div className="px-5 py-4">{children}</div>
    </Card>
  )
}

function NumberField({ label, help, value, min, max, onChange }: { label: string; help: string; value: number; min: number; max: number; onChange: (v: number) => void }) {
  return (
    <label className="space-y-1.5">
      <span className="flex items-center gap-1.5 text-sm text-foreground-muted">
        {label}
        <HelpTip>{help}</HelpTip>
      </span>
      <Input type="number" min={min} max={max} value={value} onChange={(e) => onChange(parseInt(e.target.value) || min)} />
    </label>
  )
}

export default function SecurityConfigPanel({ config, onUpdate }: SecurityConfigPanelProps) {
  const ipBan = config?.IPBan ?? DEFAULT_IPBAN
  const refererBan = config?.RefererBan ?? DEFAULT_REFERER
  const trusted = config?.TrustedProxies ?? DEFAULT_TRUSTED
  // 保留 Security 下的其他字段, 只替换改动的那一项
  const base: SecurityConfig = { ...(config ?? {}), IPBan: ipBan, RefererBan: refererBan }

  const updateIPBan = (patch: Partial<IPBanConfig>) => onUpdate({ ...base, IPBan: { ...ipBan, ...patch } })
  const setTrusted = (next: TrustedProxy[]) => onUpdate({ ...base, TrustedProxies: next })
  const patchTrusted = (i: number, p: Partial<TrustedProxy>) => setTrusted(trusted.map((t, j) => (j === i ? { ...t, ...p } : t)))

  return (
    <div className="space-y-5">
      <Section
        title="IP 封禁"
        help="同一 IP 在统计窗口内访问不存在的资源 (404) 超过阈值即被封禁, 封禁是各节点本地状态。"
        actions={
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" asChild>
              <Link href="/dashboard/security">
                <Shield className="h-4 w-4" />
                封禁记录
              </Link>
            </Button>
            <Switch checked={ipBan.Enabled} onCheckedChange={(v) => updateIPBan({ Enabled: v })} aria-label="启用 IP 封禁" />
          </div>
        }
      >
        {ipBan.Enabled ? (
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <NumberField label="404 阈值 (次)" help="统计窗口内 404 次数超过此值即封禁" value={ipBan.ErrorThreshold} min={1} max={100} onChange={(v) => updateIPBan({ ErrorThreshold: v })} />
            <NumberField label="统计窗口 (分钟)" help="累计 404 次数的时间窗口" value={ipBan.WindowMinutes} min={1} max={60} onChange={(v) => updateIPBan({ WindowMinutes: v })} />
            <NumberField label="封禁时长 (分钟)" help="封禁持续多久后自动解除" value={ipBan.BanDurationMinutes} min={1} max={1440} onChange={(v) => updateIPBan({ BanDurationMinutes: v })} />
            <NumberField label="清理间隔 (分钟)" help="清理过期计数与封禁记录的间隔" value={ipBan.CleanupIntervalMinutes} min={1} max={60} onChange={(v) => updateIPBan({ CleanupIntervalMinutes: v })} />
          </div>
        ) : (
          <p className="text-sm text-foreground-subtle">未启用</p>
        )}
      </Section>

      <Section
        title="可信 CDN 入口"
        help="决定怎么识别访客真实 IP (封禁 / 统计 / 日志都用它)。只有请求确实来自某 CDN 的回源网段, 才采信该 CDN 声明访客 IP 的请求头; 否则直连节点的人可以伪造请求头绕过封禁。Cloudflare 网段已内置。"
        actions={
          <Button variant="ghost" size="sm" onClick={() => setTrusted([...trusted, { Name: "", Header: "", CIDRs: [] }])}>
            <Plus className="h-4 w-4" />
            添加
          </Button>
        }
      >
        <ul className="divide-y rounded-lg border">
          <li className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2.5 text-sm">
            <span className="font-medium">Cloudflare</span>
            <span className="font-mono text-foreground-muted">CF-Connecting-IP</span>
            <span className="text-foreground-subtle">官方回源网段已内置, 无需配置</span>
          </li>
          {trusted.map((t, i) => {
            const bad = t.CIDRs.filter((c) => !CIDR_RE.test(c))
            return (
              <li key={i} className="space-y-2 px-3 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Input className="h-9 w-36" placeholder="名称, 如 EdgeOne" value={t.Name} onChange={(e) => patchTrusted(i, { Name: e.target.value })} />
                  <Input
                    className="h-9 w-48 font-mono"
                    placeholder="请求头, 如 EO-Client-IP"
                    value={t.Header}
                    onChange={(e) => patchTrusted(i, { Header: e.target.value.trim() })}
                  />
                  {t.CIDRs.length === 0 ? (
                    <span className="rounded-md bg-warning-soft px-1.5 py-0.5 text-xs text-warning">未填网段: 仅凭请求头, 可被伪造</span>
                  ) : (
                    <span className="text-sm text-foreground-subtle tabular-nums">{t.CIDRs.length} 个网段</span>
                  )}
                  <Button
                    variant="ghost"
                    size="icon"
                    className="ml-auto h-8 w-8 text-destructive hover:text-destructive"
                    aria-label="删除这个入口"
                    onClick={() => setTrusted(trusted.filter((_, j) => j !== i))}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
                <Textarea
                  rows={2}
                  className="font-mono text-sm"
                  placeholder="回源 IP 网段, 一行或逗号分隔一个, 如 43.175.0.0/16 (在 CDN 控制台的回源 IP 网段里查看)"
                  value={t.CIDRs.join("\n")}
                  onChange={(e) =>
                    patchTrusted(i, {
                      CIDRs: e.target.value
                        .split(/[\s,]+/)
                        .map((s) => s.trim())
                        .filter(Boolean),
                    })
                  }
                />
                {bad.length > 0 && <p className="text-sm text-destructive">格式不对, 节点会忽略这个入口: {bad.join(", ")}</p>}
              </li>
            )
          })}
        </ul>
      </Section>

      <Section title="全局 Referer" help="对所有路径生效, 恒为黑名单。单个路径可在「路径映射」里另设规则 (支持白名单和子路径例外)。">
        <RefererBanEditor config={refererBan} onUpdate={(next) => onUpdate({ ...base, RefererBan: next })} />
      </Section>
    </div>
  )
}
