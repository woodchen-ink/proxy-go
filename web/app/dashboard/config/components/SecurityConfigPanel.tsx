import React from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Shield } from "lucide-react"
import Link from "next/link"

interface SecurityConfig {
  IPBan: {
    Enabled: boolean
    ErrorThreshold: number
    WindowMinutes: number
    BanDurationMinutes: number
    CleanupIntervalMinutes: number
  }
}

interface SecurityConfigPanelProps {
  config: SecurityConfig | null
  onUpdate: (field: keyof SecurityConfig['IPBan'], value: boolean | number) => void
}

export default function SecurityConfigPanel({ config, onUpdate }: SecurityConfigPanelProps) {
  return (
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
            checked={config?.IPBan?.Enabled || false}
            onCheckedChange={(checked) => onUpdate('Enabled', checked)}
          />
        </div>

        {config?.IPBan?.Enabled && (
          <>
            <div className="space-y-2">
              <Label>404 错误阈值</Label>
              <Input
                type="number"
                min={1}
                max={100}
                value={config?.IPBan?.ErrorThreshold || 10}
                onChange={(e) => onUpdate('ErrorThreshold', parseInt(e.target.value) || 10)}
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
                value={config?.IPBan?.WindowMinutes || 5}
                onChange={(e) => onUpdate('WindowMinutes', parseInt(e.target.value) || 5)}
              />
              <p className="text-sm text-muted-foreground">
                统计 404 错误的时间窗口长度
              </p>
            </div>

            <div className="space-y-2">
              <Label>封禁时长（分钟）</Label>
              <Input
                type="number"
                min={1}
                max={1440}
                value={config?.IPBan?.BanDurationMinutes || 5}
                onChange={(e) => onUpdate('BanDurationMinutes', parseInt(e.target.value) || 5)}
              />
              <p className="text-sm text-muted-foreground">
                IP 被封禁的持续时间
              </p>
            </div>

            <div className="space-y-2">
              <Label>清理间隔（分钟）</Label>
              <Input
                type="number"
                min={1}
                max={60}
                value={config?.IPBan?.CleanupIntervalMinutes || 1}
                onChange={(e) => onUpdate('CleanupIntervalMinutes', parseInt(e.target.value) || 1)}
              />
              <p className="text-sm text-muted-foreground">
                清理过期记录的间隔时间
              </p>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}
