import React, { useState, useEffect } from "react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { TimeInput } from "@/components/ui/time-input"
import { useToast } from "@/components/ui/use-toast"

interface CacheConfig {
  max_age: number
  cleanup_tick: number
  max_cache_size: number
}

interface PathCacheConfigDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  path: string
  cacheConfig: CacheConfig | null | undefined
  onSave: (config: CacheConfig | null) => void
}

export default function PathCacheConfigDialog({
  open,
  onOpenChange,
  path,
  cacheConfig,
  onSave,
}: PathCacheConfigDialogProps) {
  const [useCustomConfig, setUseCustomConfig] = useState(false)
  const [maxAge, setMaxAge] = useState(30)
  const [cleanupTick, setCleanupTick] = useState(5)
  const [maxCacheSize, setMaxCacheSize] = useState(10)
  const { toast } = useToast()

  useEffect(() => {
    if (cacheConfig) {
      setUseCustomConfig(true)
      setMaxAge(cacheConfig.max_age)
      setCleanupTick(cacheConfig.cleanup_tick)
      setMaxCacheSize(cacheConfig.max_cache_size)
    } else {
      setUseCustomConfig(false)
      // 使用默认值
      setMaxAge(30)
      setCleanupTick(5)
      setMaxCacheSize(10)
    }
  }, [cacheConfig, open])

  const handleSave = () => {
    if (!useCustomConfig) {
      onSave(null)
      onOpenChange(false)
      toast({
        title: "保存成功",
        description: "已切换到使用全局缓存配置",
      })
      return
    }

    // 验证输入
    if (maxAge < 0 || cleanupTick < 0 || maxCacheSize < 0) {
      toast({
        title: "验证失败",
        description: "所有值必须大于等于0",
        variant: "destructive",
      })
      return
    }

    if (cleanupTick > maxAge) {
      toast({
        title: "验证失败",
        description: "清理间隔不能大于最大缓存时间",
        variant: "destructive",
      })
      return
    }

    const config: CacheConfig = {
      max_age: maxAge,
      cleanup_tick: cleanupTick,
      max_cache_size: maxCacheSize,
    }

    onSave(config)
    onOpenChange(false)
    toast({
      title: "保存成功",
      description: "缓存配置已更新",
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>配置缓存 - {path}</DialogTitle>
          <DialogDescription>
            为此路径配置独立的缓存策略，或使用全局缓存配置
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* 是否使用自定义配置 */}
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>使用自定义缓存配置</Label>
              <div className="text-sm text-muted-foreground">
                关闭后将使用全局缓存配置
              </div>
            </div>
            <Switch
              checked={useCustomConfig}
              onCheckedChange={setUseCustomConfig}
            />
          </div>

          {useCustomConfig && (
            <>
              <div className="space-y-2">
                <Label htmlFor="max_age">最大缓存时间</Label>
                <TimeInput
                  id="max_age"
                  value={maxAge}
                  onChange={setMaxAge}
                  placeholder="30"
                  min={0}
                />
                <div className="text-xs text-muted-foreground">
                  缓存项在此时间后过期
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="cleanup_tick">清理间隔</Label>
                <TimeInput
                  id="cleanup_tick"
                  value={cleanupTick}
                  onChange={setCleanupTick}
                  placeholder="5"
                  min={0}
                />
                <div className="text-xs text-muted-foreground">
                  定期清理过期缓存的间隔时间
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_cache_size">最大缓存大小（GB）</Label>
                <Input
                  id="max_cache_size"
                  type="number"
                  min="0"
                  step="0.1"
                  value={maxCacheSize}
                  onChange={(e) => setMaxCacheSize(parseFloat(e.target.value) || 0)}
                  placeholder="10"
                />
                <div className="text-xs text-muted-foreground">
                  缓存总大小超过此值时将清理最旧的缓存
                </div>
              </div>
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button onClick={handleSave}>保存</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
