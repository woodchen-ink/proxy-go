import React, { useState } from "react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Edit, Trash2, Database, Shield, FileText, ChevronDown, ChevronUp, Eraser } from "lucide-react"
import PathStatsCard from "./PathStatsCard"
import PathCacheConfigDialog from "./PathCacheConfigDialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"

interface CacheConfig {
  max_age: number
  cleanup_tick: number
  max_cache_size: number
}

interface PathMapping {
  DefaultTarget: string
  ExtensionMap?: any[]
  RedirectMode?: boolean
  Enabled?: boolean
  CacheConfig?: CacheConfig
}

interface PathStats {
  path: string
  request_count: number
  error_count: number
  bytes_transferred: number
  avg_latency: string
  last_access_time: number
  status_2xx: number
  status_3xx: number
  status_4xx: number
  status_5xx: number
  cache_hits: number
  cache_misses: number
  cache_hit_rate: number
  bytes_saved: number
}

interface ExtRuleConfig {
  Extensions: string
  Target: string
  SizeThreshold?: number
  MaxSize?: number
  RedirectMode?: boolean
  Domains?: string
}

interface PathMappingItemProps {
  path: string
  mapping: PathMapping | string
  stats: PathStats | undefined
  isSystemPath?: boolean
  onEdit: (path: string) => void
  onDelete: (path: string) => void
  onToggleEnabled: (path: string, enabled: boolean) => void
  onCacheConfigUpdate: (path: string, config: CacheConfig | null) => void
  onExtensionMapEdit?: (path: string) => void
  onExtensionRuleEdit?: (path: string, index?: number, rule?: ExtRuleConfig) => void
  onExtensionRuleDelete?: (path: string, index: number) => void
  onClearCache?: (path: string) => void
  onResetStats?: (path: string) => Promise<void>
}

export default function PathMappingItem({
  path,
  mapping,
  stats,
  isSystemPath = false,
  onEdit,
  onDelete,
  onToggleEnabled,
  onCacheConfigUpdate,
  onExtensionMapEdit,
  onExtensionRuleEdit,
  onExtensionRuleDelete,
  onClearCache,
  onResetStats,
}: PathMappingItemProps) {
  const [cacheDialogOpen, setCacheDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [extensionRulesOpen, setExtensionRulesOpen] = useState(false)

  // 处理字符串格式的映射
  const mappingObj: PathMapping =
    typeof mapping === "string"
      ? { DefaultTarget: mapping, Enabled: true }
      : mapping

  const isEnabled = mappingObj.Enabled !== false

  const handleCacheSave = (config: CacheConfig | null) => {
    onCacheConfigUpdate(path, config)
  }

  // 格式化字节大小
  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return "0 B"
    const k = 1024
    const sizes = ["B", "KB", "MB", "GB"]
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
  }

  return (
    <>
      <Card className={!isEnabled ? "opacity-60" : ""}>
        <CardContent className="p-4">
          <div className="space-y-3">
            {/* 标题行 */}
            <div className="flex items-start justify-between">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-mono font-semibold text-lg">{path}</span>
                  {isSystemPath && (
                    <Badge variant="outline" style={{
                      backgroundColor: '#F4E8E0',
                      color: '#C08259',
                      borderColor: '#C08259'
                    }}>
                      <Shield className="h-3 w-3 mr-1" />
                      系统路径
                    </Badge>
                  )}
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={isEnabled}
                      onCheckedChange={(checked) => onToggleEnabled(path, checked)}
                      className="data-[state=checked]:bg-[#518751]"
                    />
                    <span className="text-xs text-muted-foreground">
                      {isEnabled ? "已启用" : "已禁用"}
                    </span>
                  </div>
                  {mappingObj.RedirectMode && (
                    <Badge variant="outline">302重定向</Badge>
                  )}
                  {mappingObj.CacheConfig && (
                    <Badge variant="outline" style={{
                      backgroundColor: '#F4E8E0',
                      color: '#C08259',
                      borderColor: '#C08259'
                    }}>
                      <Database className="h-3 w-3 mr-1" />
                      自定义缓存
                    </Badge>
                  )}
                </div>
                <div className="text-sm text-muted-foreground mt-1 break-all">
                  目标: {mappingObj.DefaultTarget}
                </div>
                {mappingObj.ExtensionMap && mappingObj.ExtensionMap.length > 0 && (
                  <div className="text-xs text-muted-foreground mt-1">
                    扩展名规则: {mappingObj.ExtensionMap.length} 条
                  </div>
                )}
              </div>

              {/* 操作按钮 */}
              <div className="flex items-center gap-2 ml-4">
                {onExtensionMapEdit && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onExtensionMapEdit(path)}
                    title="扩展名规则"
                  >
                    <FileText className="h-4 w-4" />
                  </Button>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setCacheDialogOpen(true)}
                  title="配置缓存"
                >
                  <Database className="h-4 w-4" />
                </Button>
                {onClearCache && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => onClearCache(path)}
                    title="清理此路径缓存"
                    className="text-orange-600 hover:text-orange-700 hover:bg-orange-50"
                  >
                    <Eraser className="h-4 w-4" />
                  </Button>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => onEdit(path)}
                  title="编辑"
                >
                  <Edit className="h-4 w-4" />
                </Button>
                {!isSystemPath && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setDeleteDialogOpen(true)}
                    className="text-destructive hover:text-destructive"
                    title="删除"
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                )}
              </div>
            </div>

            {/* 统计信息 */}
            <PathStatsCard
              stats={stats}
              onReset={onResetStats ? () => onResetStats(path) : undefined}
            />

            {/* 扩展名规则列表 */}
            {mappingObj.ExtensionMap && mappingObj.ExtensionMap.length > 0 && (
              <Collapsible open={extensionRulesOpen} onOpenChange={setExtensionRulesOpen}>
                <CollapsibleTrigger asChild>
                  <Button variant="ghost" size="sm" className="w-full justify-start px-0 hover:bg-transparent">
                    {extensionRulesOpen ? (
                      <ChevronUp className="h-4 w-4 mr-2" />
                    ) : (
                      <ChevronDown className="h-4 w-4 mr-2" />
                    )}
                    <span className="text-sm font-medium">
                      扩展名规则 ({mappingObj.ExtensionMap.length} 条)
                    </span>
                  </Button>
                </CollapsibleTrigger>
                <CollapsibleContent className="mt-2 space-y-2">
                  {mappingObj.ExtensionMap.map((rule, index) => (
                    <div
                      key={index}
                      className="border rounded-md p-3 bg-muted/50 space-y-2"
                    >
                      <div className="flex items-start justify-between">
                        <div className="flex-1 space-y-1">
                          <div className="flex items-center gap-2">
                            <Badge variant="outline" className="font-mono text-xs">
                              {rule.Extensions}
                            </Badge>
                            {rule.RedirectMode && (
                              <Badge variant="secondary" className="text-xs">
                                302重定向
                              </Badge>
                            )}
                          </div>
                          <div className="text-xs text-muted-foreground break-all">
                            目标: {rule.Target}
                          </div>
                          {rule.Domains && (
                            <div className="text-xs text-muted-foreground">
                              域名限制: {rule.Domains}
                            </div>
                          )}
                          {(rule.SizeThreshold || rule.MaxSize) && (
                            <div className="text-xs text-muted-foreground">
                              大小范围: {rule.SizeThreshold ? formatBytes(rule.SizeThreshold) : "0"} - {rule.MaxSize ? formatBytes(rule.MaxSize) : "无限制"}
                            </div>
                          )}
                        </div>
                        <div className="flex gap-1 ml-2">
                          {onExtensionRuleEdit && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => onExtensionRuleEdit(path, index, rule)}
                              className="h-8 w-8 p-0"
                            >
                              <Edit className="h-3 w-3" />
                            </Button>
                          )}
                          {onExtensionRuleDelete && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => onExtensionRuleDelete(path, index)}
                              className="h-8 w-8 p-0 text-destructive hover:text-destructive"
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          )}
                        </div>
                      </div>
                    </div>
                  ))}
                </CollapsibleContent>
              </Collapsible>
            )}
          </div>
        </CardContent>
      </Card>

      {/* 缓存配置对话框 */}
      <PathCacheConfigDialog
        open={cacheDialogOpen}
        onOpenChange={setCacheDialogOpen}
        path={path}
        cacheConfig={mappingObj.CacheConfig}
        onSave={handleCacheSave}
      />

      {/* 删除确认对话框 */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除路径映射 <span className="font-mono font-semibold">{path}</span> 吗？
              <br />
              此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                onDelete(path)
                setDeleteDialogOpen(false)
              }}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
