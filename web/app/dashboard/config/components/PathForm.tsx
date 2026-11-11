"use client"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"

export interface PathFormValue {
  path: string
  targets: string
  redirect: boolean
}

interface PathFormProps {
  title: string
  value: PathFormValue
  error: string | null
  saving: boolean
  onChange: (next: PathFormValue) => void
  onSubmit: () => void
  onCancel: () => void
}

// PathForm 新增与编辑路径共用的表单: 路径 / 回源目标 (一行一个) / 302 模式
export default function PathForm({ title, value, error, saving, onChange, onSubmit, onCancel }: PathFormProps) {
  return (
    <form
      className="max-w-2xl space-y-5 p-5"
      onSubmit={(e) => {
        e.preventDefault()
        onSubmit()
      }}
    >
      <h2 className="text-lg font-semibold">{title}</h2>
      <div className="space-y-2">
        <Label htmlFor="path-form-path">路径</Label>
        <Input
          id="path-form-path"
          value={value.path}
          onChange={(e) => onChange({ ...value, path: e.target.value })}
          placeholder="/example"
          className="font-mono"
          aria-invalid={error !== null}
          autoFocus
        />
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
      <div className="space-y-2">
        <Label htmlFor="path-form-targets">回源目标</Label>
        <Textarea
          id="path-form-targets"
          value={value.targets}
          onChange={(e) => onChange({ ...value, targets: e.target.value })}
          placeholder={"https://primary.example.com\nhttps://backup.example.com"}
          rows={3}
          className="font-mono text-sm"
        />
        <p className="text-sm text-foreground-muted">
          一行一个, 第一行为主源; 请求失败 (连接错误 / 404 / 5xx) 时按顺序回落到下一个源
        </p>
      </div>
      <label className="flex items-center justify-between gap-4">
        <span>
          <span className="block text-sm font-medium">302 重定向模式</span>
          <span className="block text-sm text-foreground-muted">不代理内容, 直接 302 跳到回源地址</span>
        </span>
        <Switch checked={value.redirect} onCheckedChange={(redirect) => onChange({ ...value, redirect })} />
      </label>
      <div className="flex gap-2">
        <Button type="submit" disabled={saving || error !== null}>
          保存
        </Button>
        <Button type="button" variant="outline" onClick={onCancel} disabled={saving}>
          取消
        </Button>
      </div>
    </form>
  )
}
