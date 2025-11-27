import React from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Plus } from "lucide-react"

interface PathDialogFormProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editingPathData: {
    path: string
    defaultTarget: string
    redirectMode: boolean
    sizeThreshold: number
    maxSize: number
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB'
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB'
  } | null
  newPathData: {
    path: string
    defaultTarget: string
    redirectMode: boolean
    extensionMap: Record<string, string>
    sizeThreshold: number
    maxSize: number
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB'
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB'
  }
  onNewPathDataChange: (data: any) => void
  onEditingPathDataChange: (data: any) => void
  onSubmit: () => void
  onOpenAddDialog: () => void
}

export default function PathDialogForm({
  open,
  onOpenChange,
  editingPathData,
  newPathData,
  onNewPathDataChange,
  onEditingPathDataChange,
  onSubmit,
  onOpenAddDialog
}: PathDialogFormProps) {
  const data = editingPathData || newPathData
  const isEditing = !!editingPathData

  const handleChange = (field: string, value: any) => {
    if (editingPathData) {
      onEditingPathDataChange({ ...editingPathData, [field]: value })
    } else {
      onNewPathDataChange({ ...newPathData, [field]: value })
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button
          onClick={onOpenAddDialog}
          style={{ backgroundColor: '#C08259', color: '#F8F7F6' }}
          className="hover:opacity-90"
        >
          <Plus className="w-4 h-4 mr-2" />
          添加路径
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEditing ? "编辑路径映射" : "添加路径映射"}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>路径 (如: /images)</Label>
            <Input
              value={data.path}
              onChange={(e) => handleChange('path', e.target.value)}
              placeholder="/example"
            />
            <p className="text-sm text-muted-foreground">
              请输入需要代理的路径
            </p>
          </div>
          <div className="space-y-2">
            <Label>默认目标</Label>
            <Input
              value={data.defaultTarget}
              onChange={(e) => handleChange('defaultTarget', e.target.value)}
              placeholder="https://example.com"
            />
            <p className="text-sm text-muted-foreground">
              默认的回源地址，所有请求都会转发到这个地址
            </p>
          </div>
          <div className="flex items-center justify-between">
            <Label>使用302跳转</Label>
            <Switch
              checked={data.redirectMode}
              onCheckedChange={(checked) => handleChange('redirectMode', checked)}
            />
          </div>
          <p className="text-sm text-muted-foreground">
            启用后，访问此路径时将302跳转到目标URL，而不是代理转发
          </p>
          <Button onClick={onSubmit}>
            {isEditing ? "保存" : "添加"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
