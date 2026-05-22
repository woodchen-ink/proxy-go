import React from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Shield } from "lucide-react"
import Link from "next/link"
import RefererBanEditor, { RefererBanConfig } from "./RefererBanEditor"

interface SecurityConfig {
  IPBan: {
    Enabled: boolean
    ErrorThreshold: number
    WindowMinutes: number
    BanDurationMinutes: number
    CleanupIntervalMinutes: number
  }
  RefererBan?: RefererBanConfig
}

interface SecurityConfigPanelProps {
  config: SecurityConfig | null
  onUpdate: (next: SecurityConfig) => void
}

const DEFAULT_IPBAN = {
  Enabled: false,
  ErrorThreshold: 10,
  WindowMinutes: 5,
  BanDurationMinutes: 5,
  CleanupIntervalMinutes: 1,
}

const DEFAULT_REFERER: RefererBanConfig = {
  Enabled: false,
  Hosts: [],
  BlockEmpty: false,
}

export default function SecurityConfigPanel({ config, onUpdate }: SecurityConfigPanelProps) {
  const ipBan = config?.IPBan ?? DEFAULT_IPBAN
  const refererBan = config?.RefererBan ?? DEFAULT_REFERER
  const base: SecurityConfig = { IPBan: ipBan, RefererBan: refererBan }

  const updateIPBan = (patch: Partial<typeof ipBan>) => {
    onUpdate({ ...base, IPBan: { ...ipBan, ...patch } })
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>IP 封禁策略</CardTitle>
          <Button variant="outline" asChild>
            <Link href="/dashboard/security">
              <Shield className="w-4 h-4 mr-2" />
              安全管理
            </Link>
          </Button>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <Label>启用 IP 封禁</Label>
              <p className="text-sm text-muted-foreground">
                当 IP 频繁访问不存在的资源时自动封禁
              </p>
            </div>
            <Switch
              checked={ipBan.Enabled}
              onCheckedChange={(checked) => updateIPBan({ Enabled: checked })}
            />
          </div>

          {ipBan.Enabled && (
            <>
              <div className="space-y-2">
                <Label>404 错误阈值</Label>
                <Input
                  type="number"
                  min={1}
                  max={100}
                  value={ipBan.ErrorThreshold}
                  onChange={(e) => updateIPBan({ ErrorThreshold: parseInt(e.target.value) || 10 })}
                />
                <p className="text-sm text-muted-foreground">
                  在指定时间窗口内，IP 访问不存在资源的次数超过此值将被封禁
                </p>
              </div>

              <div className="space-y-2">
                <Label>统计窗口时间（分钟）</Label>
                <Input
                  type="number"
                  min={1}
                  max={60}
                  value={ipBan.WindowMinutes}
                  onChange={(e) => updateIPBan({ WindowMinutes: parseInt(e.target.value) || 5 })}
                />
                <p className="text-sm text-muted-foreground">统计 404 错误的时间窗口长度</p>
              </div>

              <div className="space-y-2">
                <Label>封禁时长（分钟）</Label>
                <Input
                  type="number"
                  min={1}
                  max={1440}
                  value={ipBan.BanDurationMinutes}
                  onChange={(e) => updateIPBan({ BanDurationMinutes: parseInt(e.target.value) || 5 })}
                />
                <p className="text-sm text-muted-foreground">IP 被封禁的持续时间</p>
              </div>

              <div className="space-y-2">
                <Label>清理间隔（分钟）</Label>
                <Input
                  type="number"
                  min={1}
                  max={60}
                  value={ipBan.CleanupIntervalMinutes}
                  onChange={(e) =>
                    updateIPBan({ CleanupIntervalMinutes: parseInt(e.target.value) || 1 })
                  }
                />
                <p className="text-sm text-muted-foreground">清理过期记录的间隔时间</p>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>引用来源 (Referer) 黑名单</CardTitle>
          <p className="text-sm font-normal text-muted-foreground">
            全局规则, 对所有路径生效; 单独路径可在"路径映射"中追加额外规则
          </p>
        </CardHeader>
        <CardContent>
          <RefererBanEditor
            config={refererBan}
            onUpdate={(next) => onUpdate({ ...base, RefererBan: next })}
          />
        </CardContent>
      </Card>
    </div>
  )
}
