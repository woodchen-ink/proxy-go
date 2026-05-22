"use client"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

export type CDNProviderType = "cloudflare" | "edgeone"

export interface CDNProvider {
  ID: string
  Name: string
  Type: CDNProviderType
  Enabled: boolean
  Credentials: Record<string, string>
}

// 按 type 还原默认凭据字段, 切换类型时丢弃旧字段
export function emptyCredentialsFor(type: CDNProviderType): Record<string, string> {
  if (type === "cloudflare") return { apiToken: "", zoneId: "" }
  return { secretId: "", secretKey: "", zoneId: "" }
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  provider: CDNProvider | null
  isNew: boolean
  saving: boolean
  onChange: (p: CDNProvider) => void
  onSubmit: () => void
}

// CDNProviderEditor 单一职责: 编辑 / 新增一条 CDN provider 配置
// 校验由父组件负责 (统一 toast 风格), 这里只渲染表单
export default function CDNProviderEditor({
  open,
  onOpenChange,
  provider,
  isNew,
  saving,
  onChange,
  onSubmit,
}: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{isNew ? "新增 CDN 配置" : "编辑 CDN 配置"}</DialogTitle>
          <DialogDescription>
            配置保存在服务端 (并随主配置同步到 D1), 不写入浏览器本地存储。
          </DialogDescription>
        </DialogHeader>

        {provider && (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>厂商</Label>
              <Select
                value={provider.Type}
                onValueChange={(v) =>
                  onChange({
                    ...provider,
                    Type: v as CDNProviderType,
                    Credentials: emptyCredentialsFor(v as CDNProviderType),
                  })
                }
                disabled={!isNew}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cloudflare">Cloudflare</SelectItem>
                  <SelectItem value="edgeone">腾讯云 EdgeOne</SelectItem>
                </SelectContent>
              </Select>
              {!isNew && (
                <p className="text-xs text-muted-foreground">
                  厂商类型保存后不可修改, 如需切换请删除后重新创建
                </p>
              )}
            </div>

            <div className="space-y-2">
              <Label>配置名称</Label>
              <Input
                value={provider.Name}
                onChange={(e) => onChange({ ...provider, Name: e.target.value })}
                placeholder="例如: 主站 Cloudflare"
              />
            </div>

            {provider.Type === "cloudflare" ? (
              <CloudflareFields provider={provider} onChange={onChange} />
            ) : (
              <EdgeOneFields provider={provider} onChange={onChange} />
            )}

            <div className="flex items-center justify-between rounded-md border p-3">
              <div>
                <Label className="text-sm">启用此配置</Label>
                <p className="text-xs text-muted-foreground">
                  启用后其他配置会自动停用 (仅允许 1 个启用)
                </p>
              </div>
              <Switch
                checked={provider.Enabled}
                onCheckedChange={(checked) => onChange({ ...provider, Enabled: checked })}
              />
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            取消
          </Button>
          <Button onClick={onSubmit} disabled={saving}>
            {saving ? "保存中..." : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// CloudflareFields 渲染 Cloudflare 凭据 (apiToken + zoneId)
function CloudflareFields({ provider, onChange }: { provider: CDNProvider; onChange: (p: CDNProvider) => void }) {
  const setCred = (k: string, v: string) =>
    onChange({ ...provider, Credentials: { ...provider.Credentials, [k]: v } })
  return (
    <>
      <div className="space-y-2">
        <Label>API Token</Label>
        <Input
          type="password"
          value={provider.Credentials.apiToken || ""}
          onChange={(e) => setCred("apiToken", e.target.value)}
          placeholder="在 https://dash.cloudflare.com/profile/api-tokens 创建"
        />
        <p className="text-xs text-muted-foreground">
          权限要求: Zone - Cache Purge - Purge; Zone - Zone - Read
        </p>
      </div>
      <div className="space-y-2">
        <Label>Zone ID</Label>
        <Input
          value={provider.Credentials.zoneId || ""}
          onChange={(e) => setCred("zoneId", e.target.value)}
          placeholder="Cloudflare 域名概览页可获取"
        />
      </div>
    </>
  )
}

// EdgeOneFields 渲染腾讯云 EdgeOne 凭据 (secretId + secretKey + zoneId)
function EdgeOneFields({ provider, onChange }: { provider: CDNProvider; onChange: (p: CDNProvider) => void }) {
  const setCred = (k: string, v: string) =>
    onChange({ ...provider, Credentials: { ...provider.Credentials, [k]: v } })
  return (
    <>
      <div className="space-y-2">
        <Label>SecretId</Label>
        <Input
          value={provider.Credentials.secretId || ""}
          onChange={(e) => setCred("secretId", e.target.value)}
          placeholder="腾讯云控制台 CAM 获取"
        />
      </div>
      <div className="space-y-2">
        <Label>SecretKey</Label>
        <Input
          type="password"
          value={provider.Credentials.secretKey || ""}
          onChange={(e) => setCred("secretKey", e.target.value)}
          placeholder="腾讯云控制台 CAM 获取"
        />
      </div>
      <div className="space-y-2">
        <Label>ZoneId</Label>
        <Input
          value={provider.Credentials.zoneId || ""}
          onChange={(e) => setCred("zoneId", e.target.value)}
          placeholder="EdgeOne 站点 ID, 如 zone-xxxx"
        />
      </div>
    </>
  )
}
