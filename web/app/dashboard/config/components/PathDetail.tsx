"use client"

import type { ReactNode } from "react"
import { Pencil, Plus, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { HelpTip } from "@/components/help-tip"
import RefererBanEditor from "./RefererBanEditor"
import RefererRedirectEditor from "./RefererRedirectEditor"
import { convertBytesToUnit, getMappingTargets } from "../utils"
import type { ExtRuleConfig, PathMapping, RefererBanConfig, RefererRedirectConfig } from "../types"

export interface PathDetailActions {
  onEdit: () => void
  onDelete: () => void
  onToggleEnabled: (enabled: boolean) => void
  onToggleCFImageOpt: (enabled: boolean) => void
  onConfigureCache: () => void
  onClearCache: () => void
  onClearCDN: () => void
  onRefererBan: (next: RefererBanConfig) => void
  onRefererRedirect: (next: RefererRedirectConfig) => void
  onAddExtRule: () => void
  onEditExtRule: (index: number, rule: ExtRuleConfig) => void
  onDeleteExtRule: (index: number) => void
}

interface PathDetailProps extends PathDetailActions {
  path: string
  mapping: PathMapping
  isSystemPath: boolean
  saving: boolean
}

// Section 详情区内的一个分组: 标题行 (标题 + 行尾动作) 与内容, 分组之间由父级分隔线隔开
function Section({ title, actions, children }: { title?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="px-5 py-4">
      {(title || actions) && (
        <div className="mb-3 flex min-h-8 items-center justify-between gap-3">
          <h3 className="text-sm font-semibold">{title}</h3>
          {actions && <div className="flex flex-wrap items-center gap-1">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  )
}

function formatSize(bytes: number): string {
  const { value, unit } = convertBytesToUnit(bytes)
  return `${value} ${unit}`
}

// PathDetail 单个路径的配置详情: 标题行放路径与启用开关, 其下按 回源 / 选项 / 缓存 / Referer / 扩展名 分组
export default function PathDetail(props: PathDetailProps) {
  const { path, mapping, isSystemPath, saving } = props
  const enabled = mapping.Enabled !== false
  const targets = getMappingTargets(mapping)
  const rules = mapping.ExtensionMap ?? []
  const cache = mapping.CacheConfig

  return (
    <div className="flex min-h-0 flex-col">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b px-5 py-3">
        <div className="flex min-w-0 items-center gap-3">
          <h2 className="truncate font-mono text-lg font-semibold">{path}</h2>
          {!enabled && <Badge variant="secondary">已停用</Badge>}
        </div>
        <div className="flex items-center gap-1">
          <label className="mr-2 flex items-center gap-2 text-sm text-foreground-muted">
            启用
            <Switch checked={enabled} onCheckedChange={props.onToggleEnabled} disabled={saving} />
          </label>
          <Button variant="ghost" size="sm" onClick={props.onEdit} disabled={saving}>
            <Pencil className="h-4 w-4" />
            编辑
          </Button>
          {!isSystemPath && (
            <Button
              variant="ghost"
              size="sm"
              onClick={props.onDelete}
              disabled={saving}
              className="text-destructive hover:text-destructive"
            >
              <Trash2 className="h-4 w-4" />
              删除
            </Button>
          )}
        </div>
      </header>

      <div className="min-h-0 flex-1 divide-y overflow-y-auto overscroll-contain">
        <Section
          title={
            <>
              回源目标
              {targets.length > 1 && (
                <span className="ml-2 font-normal text-foreground-subtle">{targets.length} 个源 · 失败按序回落</span>
              )}
            </>
          }
          actions={mapping.RedirectMode ? <Badge variant="outline">302 重定向</Badge> : undefined}
        >
          <ol className="space-y-1.5">
            {(targets.length ? targets : [mapping.DefaultTarget]).map((t, i) => (
              <li key={i} className="flex items-baseline gap-3 text-sm">
                {targets.length > 1 && (
                  <span className="w-10 shrink-0 text-foreground-subtle">{i === 0 ? "主源" : `备 ${i}`}</span>
                )}
                <span className="break-all font-mono">{t}</span>
              </li>
            ))}
          </ol>
        </Section>

        <Section>
          <div className="flex items-center justify-between gap-6">
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-semibold">Cloudflare Images 优化</span>
              <HelpTip>
                图片按 Accept 格式 (avif / webp …) + 浏览器分桶缓存, 用于源站按设备返回不同格式的场景 (如 Cloudflare Images)。关闭时全部设备共享一份缓存, 命中率更高。
              </HelpTip>
            </div>
            <Switch checked={!!mapping.CFImageOpt} onCheckedChange={props.onToggleCFImageOpt} />
          </div>
        </Section>

        <Section
          title="缓存"
          actions={
            <>
              <Button variant="ghost" size="sm" onClick={props.onConfigureCache}>
                配置
              </Button>
              <Button variant="ghost" size="sm" onClick={props.onClearCache}>
                清理本地缓存
              </Button>
              <Button variant="ghost" size="sm" onClick={props.onClearCDN}>
                清理 CDN 缓存
              </Button>
            </>
          }
        >
          {cache ? (
            <dl className="grid grid-cols-3 gap-4 text-sm">
              <div>
                <dt className="text-foreground-muted">最大缓存时间</dt>
                <dd className="mt-0.5 font-medium tabular-nums">{cache.max_age} 分钟</dd>
              </div>
              <div>
                <dt className="text-foreground-muted">清理间隔</dt>
                <dd className="mt-0.5 font-medium tabular-nums">{cache.cleanup_tick} 分钟</dd>
              </div>
              <div>
                <dt className="text-foreground-muted">最大缓存大小</dt>
                <dd className="mt-0.5 font-medium tabular-nums">{cache.max_cache_size} GB</dd>
              </div>
            </dl>
          ) : (
            <p className="text-sm text-foreground-muted">使用全局缓存配置</p>
          )}
        </Section>

        <Section>
          <RefererBanEditor
            config={mapping.RefererBan ?? { Enabled: false, Mode: "blacklist", Hosts: [], BlockEmpty: false }}
            onUpdate={props.onRefererBan}
            allowWhitelist
            pathPrefix={path}
          />
        </Section>

        <Section>
          <RefererRedirectEditor
            config={mapping.RefererRedirect ?? { Enabled: false, Rules: [] }}
            onUpdate={props.onRefererRedirect}
          />
        </Section>

        <Section
          title={
            <>
              扩展名规则
              <span className="ml-2 font-normal text-foreground-subtle tabular-nums">{rules.length}</span>
            </>
          }
          actions={
            <Button variant="ghost" size="sm" onClick={props.onAddExtRule}>
              <Plus className="h-4 w-4" />
              添加
            </Button>
          }
        >
          {rules.length === 0 ? (
            <p className="text-sm text-foreground-muted">按文件类型 / 大小 / 域名分流到其他回源, 暂无规则</p>
          ) : (
            <ul className="divide-y rounded-lg border">
              {rules.map((rule, i) => (
                <li key={i} className="group flex items-start gap-3 px-3 py-2.5">
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-sm font-medium">{rule.Extensions}</span>
                      {rule.RedirectMode && <Badge variant="outline">302</Badge>}
                    </div>
                    <div className="break-all font-mono text-sm text-foreground-muted">{rule.Target}</div>
                    {(rule.Domains || rule.SizeThreshold > 0 || rule.MaxSize > 0) && (
                      <div className="text-sm text-foreground-subtle">
                        {rule.Domains && <>域名 {rule.Domains}</>}
                        {rule.Domains && (rule.SizeThreshold > 0 || rule.MaxSize > 0) && " · "}
                        {(rule.SizeThreshold > 0 || rule.MaxSize > 0) && (
                          <>
                            大小 {rule.SizeThreshold ? formatSize(rule.SizeThreshold) : "0"} ~{" "}
                            {rule.MaxSize ? formatSize(rule.MaxSize) : "不限"}
                          </>
                        )}
                      </div>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8"
                      aria-label="编辑规则"
                      onClick={() => props.onEditExtRule(i, rule)}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-destructive hover:text-destructive"
                      aria-label="删除规则"
                      onClick={() => props.onDeleteExtRule(i)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Section>
      </div>
    </div>
  )
}
