import React from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

interface ExtensionRuleDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editingRule: {
    index: number
    extensions: string
    target: string
    sizeThreshold: number
    maxSize: number
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB'
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB'
    domains: string
  } | null
  newRule: {
    extensions: string
    target: string
    redirectMode: boolean
    sizeThreshold: number
    maxSize: number
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB'
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB'
    domains: string
  }
  onNewRuleChange: (rule: any) => void
  onSubmit: () => void
}

export default function ExtensionRuleDialog({
  open,
  onOpenChange,
  editingRule,
  newRule,
  onNewRuleChange,
  onSubmit
}: ExtensionRuleDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editingRule ? "编辑扩展名规则" : "添加扩展名规则"}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>扩展名</Label>
            <Input
              value={newRule.extensions}
              onChange={(e) => onNewRuleChange({ ...newRule, extensions: e.target.value })}
              placeholder="jpg,png,webp"
            />
            <p className="text-sm text-muted-foreground">
              多个扩展名用逗号分隔，不需要包含点号。使用星号 * 表示匹配所有未指定的扩展名。
            </p>
          </div>
          <div className="space-y-2">
            <Label>目标 URL</Label>
            <Input
              value={newRule.target}
              onChange={(e) => onNewRuleChange({ ...newRule, target: e.target.value })}
              placeholder="https://example.com"
            />
          </div>
          <div className="space-y-2">
            <Label>限制域名（可选）</Label>
            <Input
              value={newRule.domains}
              onChange={(e) => onNewRuleChange({ ...newRule, domains: e.target.value })}
              placeholder="a.com,b.com"
            />
            <p className="text-sm text-muted-foreground">
              指定该规则适用的域名，多个域名用逗号分隔。留空表示适用于所有域名。
            </p>
          </div>
          <div className="flex items-center justify-between">
            <Label>使用302跳转</Label>
            <Switch
              checked={newRule.redirectMode}
              onCheckedChange={(checked) => onNewRuleChange({ ...newRule, redirectMode: checked })}
            />
          </div>
          <p className="text-sm text-muted-foreground">
            启用后，匹配此扩展名的请求将302跳转到目标URL，而不是代理转发
          </p>
          <div className="grid gap-4">
            <div className="grid gap-2">
              <Label htmlFor="ruleSizeThreshold">最小阈值</Label>
              <div className="flex gap-2">
                <Input
                  id="ruleSizeThreshold"
                  type="number"
                  value={newRule.sizeThreshold}
                  onChange={(e) => onNewRuleChange({
                    ...newRule,
                    sizeThreshold: Number(e.target.value),
                  })}
                />
                <select
                  className="w-24 rounded-md border border-input bg-background px-3"
                  value={newRule.sizeThresholdUnit}
                  onChange={(e) => onNewRuleChange({
                    ...newRule,
                    sizeThresholdUnit: e.target.value as 'B' | 'KB' | 'MB' | 'GB',
                  })}
                >
                  <option value="B">B</option>
                  <option value="KB">KB</option>
                  <option value="MB">MB</option>
                  <option value="GB">GB</option>
                </select>
              </div>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="ruleMaxSize">最大阈值</Label>
              <div className="flex gap-2">
                <Input
                  id="ruleMaxSize"
                  type="number"
                  value={newRule.maxSize}
                  onChange={(e) => onNewRuleChange({
                    ...newRule,
                    maxSize: Number(e.target.value),
                  })}
                />
                <select
                  className="w-24 rounded-md border border-input bg-background px-3"
                  value={newRule.maxSizeUnit}
                  onChange={(e) => onNewRuleChange({
                    ...newRule,
                    maxSizeUnit: e.target.value as 'B' | 'KB' | 'MB' | 'GB',
                  })}
                >
                  <option value="B">B</option>
                  <option value="KB">KB</option>
                  <option value="MB">MB</option>
                  <option value="GB">GB</option>
                </select>
              </div>
            </div>
          </div>
          <Button onClick={onSubmit}>
            {editingRule ? "保存" : "添加"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
