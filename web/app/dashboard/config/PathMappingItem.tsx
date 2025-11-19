import React, { useState } from "react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Edit, Trash2, Database, Shield, FileText } from "lucide-react"
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

interface PathMappingItemProps {
  path: string
  mapping: PathMapping | string
  stats: PathStats | undefined
  isSystemPath?: boolean
  onEdit: (path: string) => void
  onDelete: (path: string) => void
  onCacheConfigUpdate: (path: string, config: CacheConfig | null) => void
  onExtensionMapEdit?: (path: string) => void
}

export default function PathMappingItem({
  path,
  mapping,
  stats,
  isSystemPath = false,
  onEdit,
  onDelete,
  onCacheConfigUpdate,
  onExtensionMapEdit,
}: PathMappingItemProps) {
  const [cacheDialogOpen, setCacheDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  // 处理字符串格式的映射
  const mappingObj: PathMapping =
    typeof mapping === "string"
      ? { DefaultTarget: mapping, Enabled: true }
      : mapping

  const isEnabled = mappingObj.Enabled !== false

  const handleCacheSave = (config: CacheConfig | null) => {
    onCacheConfigUpdate(path, config)
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
                    <Badge variant="outline" className="bg-blue-50 text-blue-700 border-blue-200">
                      <Shield className="h-3 w-3 mr-1" />
                      系统路径
                    </Badge>
                  )}
                  {!isEnabled && (
                    <Badge variant="secondary">已禁用</Badge>
                  )}
                  {mappingObj.RedirectMode && (
                    <Badge variant="outline">302重定向</Badge>
                  )}
                  {mappingObj.CacheConfig && (
                    <Badge variant="outline" className="bg-purple-50 text-purple-700 border-purple-200">
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
            <PathStatsCard stats={stats} />
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
