"use client"

import React, { useEffect, useState, useCallback, useRef } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Download, RefreshCw, Plus, Circle, CheckCircle2, Edit, Trash2, Database, FileText, Eraser } from "lucide-react"
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
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import PathStatsCard from "./components/PathStatsCard"
import PathCacheConfigDialog from "./components/PathCacheConfigDialog"
import SecurityConfigPanel from "./components/SecurityConfigPanel"
import ExtensionRuleDialog from "./components/ExtensionRuleDialog"
import CacheManagement from "./components/CacheManagement"
import { convertToBytes, convertBytesToUnit } from "./utils"

interface ExtRuleConfig {
  Extensions: string;
  Target: string;
  SizeThreshold: number;
  MaxSize: number;
  RedirectMode?: boolean;
  Domains?: string;
}

interface CacheConfig {
  max_age: number;
  cleanup_tick: number;
  max_cache_size: number;
}

interface PathMapping {
  DefaultTarget: string
  ExtensionMap?: ExtRuleConfig[]
  SizeThreshold?: number
  MaxSize?: number
  RedirectMode?: boolean
  Enabled?: boolean
  CacheConfig?: CacheConfig
}

interface CompressionConfig {
  Enabled: boolean
  Level: number
}

interface SecurityConfig {
  IPBan: {
    Enabled: boolean
    ErrorThreshold: number
    WindowMinutes: number
    BanDurationMinutes: number
    CleanupIntervalMinutes: number
  }
}

interface PathStats {
  path: string;
  request_count: number;
  error_count: number;
  bytes_transferred: number;
  avg_latency: string;
  last_access_time: number;
  status_2xx: number;
  status_3xx: number;
  status_4xx: number;
  status_5xx: number;
  cache_hits: number;
  cache_misses: number;
  cache_hit_rate: number;
  bytes_saved: number;
}

interface Config {
  MAP: Record<string, PathMapping | string>
  Compression: {
    Gzip: CompressionConfig
    Brotli: CompressionConfig
  }
  Security: SecurityConfig
}

export default function ConfigPage() {
  const [config, setConfig] = useState<Config | null>(null)
  const [pathStats, setPathStats] = useState<PathStats[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { toast } = useToast()
  const router = useRouter()

  // 选中的路径
  const [selectedPath, setSelectedPath] = useState<string | null>(null)

  // 删除确认
  const [deletingPath, setDeletingPath] = useState<string | null>(null)

  // 缓存配置对话框
  const [cacheDialogOpen, setCacheDialogOpen] = useState(false)

  // 编辑状态
  const [isEditingPath, setIsEditingPath] = useState(false)
  const [editedTarget, setEditedTarget] = useState("")
  const [editedRedirectMode, setEditedRedirectMode] = useState(false)

  // 添加路径状态
  const [isAddingPath, setIsAddingPath] = useState(false)
  const [newPath, setNewPath] = useState("")
  const [newTarget, setNewTarget] = useState("")
  const [newRedirectMode, setNewRedirectMode] = useState(false)

  // 扩展名规则相关
  const [extensionRuleDialogOpen, setExtensionRuleDialogOpen] = useState(false)
  const [editingExtensionRule, setEditingExtensionRule] = useState<{
    index: number,
    extensions: string;
    target: string;
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
    domains: string;
  } | null>(null)
  const [newExtensionRule, setNewExtensionRule] = useState<{
    extensions: string;
    target: string;
    redirectMode: boolean;
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
    domains: string;
  }>({
    extensions: "",
    target: "",
    redirectMode: false,
    sizeThreshold: 0,
    maxSize: 0,
    sizeThresholdUnit: 'MB',
    maxSizeUnit: 'MB',
    domains: "",
  })
  const [deletingExtensionRule, setDeletingExtensionRule] = useState<{path: string, index: number} | null>(null)

  const isInitialLoadRef = useRef(true)
  const isConfigFromApiRef = useRef(true)
  const saveTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  const fetchConfig = useCallback(async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/config/get", {
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        }
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) {
        throw new Error("获取配置失败")
      }

      const data = await response.json()

      if (!data.Security) {
        data.Security = {
          IPBan: {
            Enabled: false,
            ErrorThreshold: 10,
            WindowMinutes: 5,
            BanDurationMinutes: 5,
            CleanupIntervalMinutes: 1
          }
        }
      }

      isConfigFromApiRef.current = true
      setConfig(data)

      // 自动选中第一个路径
      if (data.MAP && Object.keys(data.MAP).length > 0 && !selectedPath) {
        setSelectedPath(Object.keys(data.MAP)[0])
      }

      // 获取路径统计
      try {
        const statsResponse = await fetch("/admin/api/path-stats", {
          headers: {
            'Authorization': `Bearer ${token}`,
            'Content-Type': 'application/json'
          }
        })
        if (statsResponse.ok) {
          const statsData = await statsResponse.json()
          setPathStats(statsData.path_stats || [])
        }
      } catch (error) {
        console.error("获取路径统计失败:", error)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "获取配置失败"
      toast({
        title: "错误",
        description: message,
        variant: "destructive",
      })
    } finally {
      setLoading(false)
    }
  }, [router, toast, selectedPath])

  const updateConfig = useCallback((newConfig: Config) => {
    isConfigFromApiRef.current = false
    setConfig(newConfig)
  }, [])

  useEffect(() => {
    fetchConfig()
  }, [fetchConfig])

  const handleSave = useCallback(async () => {
    if (!config) return

    setSaving(true)
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/config/save", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify(config),
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.message || "保存配置失败")
      }

      toast({
        title: "成功",
        description: "配置已自动保存",
      })
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "保存配置失败",
        variant: "destructive",
      })
    } finally {
      setSaving(false)
    }
  }, [config, router, toast])

  useEffect(() => {
    if (isInitialLoadRef.current || !config || isConfigFromApiRef.current) {
      isInitialLoadRef.current = false
      isConfigFromApiRef.current = false
      return
    }

    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current)
    }

    saveTimeoutRef.current = setTimeout(() => {
      handleSave()
    }, 1000)

    return () => {
      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current)
      }
    }
  }, [config, handleSave])

  const exportConfig = () => {
    if (!config) return
    const blob = new Blob([JSON.stringify(config, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'proxy-config.json'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  const pullConfigFromD1 = async () => {
    try {
      const response = await fetch('/admin/api/config/pull', {
        method: 'POST',
      })

      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(errorText || '从 D1 拉取配置失败')
      }

      const pulledConfig = await response.json()
      isConfigFromApiRef.current = true
      setConfig(pulledConfig)

      toast({
        title: "成功",
        description: "已从 D1 拉取最新配置",
      })
    } catch (error) {
      toast({
        title: "错误",
        description: error instanceof Error ? error.message : "从 D1 拉取配置失败",
        variant: "destructive",
      })
    }
  }

  const handleToggleEnabled = (path: string, enabled: boolean) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]

    if (typeof mapping === 'string') {
      newConfig.MAP[path] = {
        DefaultTarget: mapping,
        Enabled: enabled,
      }
    } else {
      newConfig.MAP[path] = {
        ...mapping,
        Enabled: enabled,
      }
    }

    updateConfig(newConfig)
  }

  const handleSelectPath = (path: string) => {
    setSelectedPath(path)
    setIsAddingPath(false)
    setIsEditingPath(false)
  }

  const handleStartAddPath = () => {
    setIsAddingPath(true)
    setSelectedPath(null)
    setNewPath("")
    setNewTarget("")
    setNewRedirectMode(false)
  }

  const handleAddPath = () => {
    if (!config || !newPath || !newTarget) {
      toast({
        title: "错误",
        description: "路径和目标不能为空",
        variant: "destructive",
      })
      return
    }

    if (config.MAP[newPath]) {
      toast({
        title: "错误",
        description: "路径已存在",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    newConfig.MAP[newPath] = {
      DefaultTarget: newTarget,
      RedirectMode: newRedirectMode,
      Enabled: true,
      ExtensionMap: []
    }
    updateConfig(newConfig)
    setSelectedPath(newPath)
    setIsAddingPath(false)
    setNewPath("")
    setNewTarget("")
    setNewRedirectMode(false)
  }

  const handleStartEditPath = () => {
    if (!selectedPath || !config) return
    const mapping = config.MAP[selectedPath]
    if (typeof mapping === 'string') {
      setEditedTarget(mapping)
      setEditedRedirectMode(false)
    } else {
      setEditedTarget(mapping.DefaultTarget)
      setEditedRedirectMode(mapping.RedirectMode || false)
    }
    setIsEditingPath(true)
  }

  const handleSaveEditPath = () => {
    if (!selectedPath || !config || !editedTarget) {
      toast({
        title: "错误",
        description: "目标不能为空",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    const mapping = newConfig.MAP[selectedPath]

    if (typeof mapping === 'string') {
      newConfig.MAP[selectedPath] = {
        DefaultTarget: editedTarget,
        RedirectMode: editedRedirectMode,
        Enabled: true,
      }
    } else {
      newConfig.MAP[selectedPath] = {
        ...mapping,
        DefaultTarget: editedTarget,
        RedirectMode: editedRedirectMode,
      }
    }

    updateConfig(newConfig)
    setIsEditingPath(false)
  }

  const handleDeletePath = () => {
    if (!selectedPath) return
    setDeletingPath(selectedPath)
  }

  const confirmDeletePath = () => {
    if (!config || !deletingPath) return
    const newConfig = { ...config }
    delete newConfig.MAP[deletingPath]
    updateConfig(newConfig)

    const paths = Object.keys(newConfig.MAP)
    setSelectedPath(paths.length > 0 ? paths[0] : null)
    setDeletingPath(null)
  }

  const handleCacheConfigUpdate = (path: string, cacheConfig: CacheConfig | null) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]

    if (typeof mapping === 'string') {
      newConfig.MAP[path] = {
        DefaultTarget: mapping,
        Enabled: true,
        CacheConfig: cacheConfig || undefined,
      }
    } else {
      newConfig.MAP[path] = {
        ...mapping,
        CacheConfig: cacheConfig || undefined,
      }
    }

    updateConfig(newConfig)
  }

  const handleClearPathCache = async (path: string) => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/cache/clear-by-path", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify({
          type: "all",
          path_prefix: path
        }),
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (!response.ok) {
        throw new Error("清理缓存失败")
      }

      const result = await response.json()

      toast({
        title: "清理成功",
        description: result.message || `已清理路径 ${path} 的缓存`,
      })
    } catch (error) {
      toast({
        title: "清理失败",
        description: error instanceof Error ? error.message : "清理缓存失败",
        variant: "destructive",
      })
    }
  }

  const handleResetPathStats = async (path: string) => {
    const token = localStorage.getItem("token")
    if (!token) {
      router.push("/login")
      return
    }

    const response = await fetch("/admin/api/path-stats/reset", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        'Authorization': `Bearer ${token}`
      },
      body: JSON.stringify({ path }),
    })

    if (response.status === 401) {
      localStorage.removeItem("token")
      router.push("/login")
      throw new Error("未授权")
    }

    if (!response.ok) {
      throw new Error("重置统计失败")
    }

    try {
      const statsResponse = await fetch("/admin/api/path-stats", {
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        }
      })
      if (statsResponse.ok) {
        const statsData = await statsResponse.json()
        setPathStats(statsData.path_stats || [])
      }
    } catch (error) {
      console.error("刷新路径统计失败:", error)
    }
  }

  const updateSecurity = (field: keyof SecurityConfig['IPBan'], value: boolean | number) => {
    if (!config) return
    const newConfig = { ...config }

    if (!newConfig.Security) {
      newConfig.Security = {
        IPBan: {
          Enabled: false,
          ErrorThreshold: 10,
          WindowMinutes: 5,
          BanDurationMinutes: 5,
          CleanupIntervalMinutes: 1
        }
      }
    }

    if (field === 'Enabled') {
      newConfig.Security.IPBan.Enabled = value as boolean
    } else {
      newConfig.Security.IPBan[field] = value as number
    }
    updateConfig(newConfig)
  }

  const handleExtensionRuleEdit = (_path: string, index?: number, rule?: ExtRuleConfig) => {
    if (index !== undefined && rule) {
      const { value: thresholdValue, unit: thresholdUnit } = convertBytesToUnit(rule.SizeThreshold || 0)
      const { value: maxValue, unit: maxUnit } = convertBytesToUnit(rule.MaxSize || 0)

      setEditingExtensionRule({
        index,
        extensions: rule.Extensions,
        target: rule.Target,
        sizeThreshold: thresholdValue,
        maxSize: maxValue,
        sizeThresholdUnit: thresholdUnit,
        maxSizeUnit: maxUnit,
        domains: rule.Domains || "",
      })

      setNewExtensionRule({
        extensions: rule.Extensions,
        target: rule.Target,
        redirectMode: rule.RedirectMode || false,
        sizeThreshold: thresholdValue,
        maxSize: maxValue,
        sizeThresholdUnit: thresholdUnit,
        maxSizeUnit: maxUnit,
        domains: rule.Domains || "",
      })
    } else {
      setEditingExtensionRule(null)
      setNewExtensionRule({
        extensions: "",
        target: "",
        redirectMode: false,
        sizeThreshold: 0,
        maxSize: 0,
        sizeThresholdUnit: 'MB',
        maxSizeUnit: 'MB',
        domains: "",
      })
    }

    setExtensionRuleDialogOpen(true)
  }

  const addOrUpdateExtensionRule = () => {
    if (!config || !selectedPath) return

    const { extensions, target, redirectMode, sizeThreshold, maxSize, sizeThresholdUnit, maxSizeUnit, domains } = newExtensionRule

    if (!extensions.trim() || !target.trim()) {
      toast({
        title: "错误",
        description: "扩展名和目标不能为空",
        variant: "destructive",
      })
      return
    }

    const extensionList = extensions.split(',').map(e => e.trim())
    if (extensionList.some(e => !e || (e !== "*" && e.includes('.')))) {
      toast({
        title: "错误",
        description: "扩展名格式不正确,不需要包含点号",
        variant: "destructive",
      })
      return
    }

    try {
      new URL(target)
    } catch {
      toast({
        title: "错误",
        description: "目标URL格式不正确",
        variant: "destructive",
      })
      return
    }

    if (domains.trim()) {
      const domainList = domains.split(',').map(d => d.trim())
      const domainRegex = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/

      for (const domain of domainList) {
        if (domain && !domainRegex.test(domain)) {
          toast({
            title: "错误",
            description: `域名格式不正确: ${domain}`,
            variant: "destructive",
          })
          return
        }
      }
    }

    const sizeThresholdBytes = convertToBytes(sizeThreshold, sizeThresholdUnit)
    const maxSizeBytes = convertToBytes(maxSize, maxSizeUnit)

    if (maxSizeBytes > 0 && sizeThresholdBytes >= maxSizeBytes) {
      toast({
        title: "错误",
        description: "最大阈值必须大于最小阈值",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    const mapping = newConfig.MAP[selectedPath]

    if (typeof mapping === "string") {
      newConfig.MAP[selectedPath] = {
        DefaultTarget: mapping,
        ExtensionMap: [{
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes,
          RedirectMode: redirectMode,
          Domains: domains.trim() || undefined
        }]
      }
    } else {
      if (!Array.isArray(mapping.ExtensionMap)) {
        mapping.ExtensionMap = []
      }

      if (editingExtensionRule) {
        const rules = mapping.ExtensionMap as ExtRuleConfig[]
        rules[editingExtensionRule.index] = {
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes,
          RedirectMode: redirectMode,
          Domains: domains.trim() || undefined
        }
      } else {
        mapping.ExtensionMap.push({
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes,
          RedirectMode: redirectMode,
          Domains: domains.trim() || undefined
        })
      }
    }

    updateConfig(newConfig)
    setExtensionRuleDialogOpen(false)
    setEditingExtensionRule(null)
    setNewExtensionRule({
      extensions: "",
      target: "",
      redirectMode: false,
      sizeThreshold: 0,
      maxSize: 0,
      sizeThresholdUnit: 'MB',
      maxSizeUnit: 'MB',
      domains: "",
    })
  }

  const deleteExtensionRule = (path: string, index: number) => {
    setDeletingExtensionRule({ path, index })
  }

  const confirmDeleteExtensionRule = () => {
    if (!config || !deletingExtensionRule) return

    const newConfig = { ...config }
    const mapping = newConfig.MAP[deletingExtensionRule.path]

    if (typeof mapping !== "string" && Array.isArray(mapping.ExtensionMap)) {
      const rules = mapping.ExtensionMap as ExtRuleConfig[]
      mapping.ExtensionMap = [
        ...rules.slice(0, deletingExtensionRule.index),
        ...rules.slice(deletingExtensionRule.index + 1)
      ]
    }

    updateConfig(newConfig)
    setDeletingExtensionRule(null)
  }

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-gray-500 mt-1">正在获取配置数据</div>
        </div>
      </div>
    )
  }

  // 获取当前选中的映射配置
  const selectedMapping = selectedPath && config ? config.MAP[selectedPath] : null
  const selectedMappingObj: PathMapping | null = selectedMapping
    ? (typeof selectedMapping === 'string'
        ? { DefaultTarget: selectedMapping, Enabled: true }
        : selectedMapping)
    : null
  const selectedStats = selectedPath ? pathStats.find(s => s.path === selectedPath) : undefined
  const isSystemPath = selectedMappingObj?.DefaultTarget === 'mirror'

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Proxy Go配置</CardTitle>
          <div className="flex space-x-2">
            <Button onClick={exportConfig} variant="outline">
              <Download className="w-4 h-4 mr-2" />
              导出配置
            </Button>
            <Button onClick={pullConfigFromD1} variant="outline">
              <RefreshCw className="w-4 h-4 mr-2" />
              从 D1 拉取配置
            </Button>
            {saving && (
              <div className="flex items-center text-sm text-muted-foreground">
                <span className="animate-pulse mr-2">●</span>
                正在自动保存...
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="paths" className="space-y-4">
            <TabsList>
              <TabsTrigger value="paths">路径映射</TabsTrigger>
              <TabsTrigger value="cache">缓存管理</TabsTrigger>
              <TabsTrigger value="security">安全策略</TabsTrigger>
            </TabsList>

            <TabsContent value="paths" className="space-y-0">
              {/* 左右分栏布局 */}
              <div className="flex gap-4 h-[calc(100vh-16rem)]">
                {/* 左侧路径列表 */}
                <div className="w-64 flex-shrink-0 border-r pr-4 overflow-y-auto">
                  <div className="space-y-2">
                    {config && Object.keys(config.MAP).map((path) => {
                      const mapping = config.MAP[path]
                      const mappingObj: PathMapping = typeof mapping === 'string'
                        ? { DefaultTarget: mapping, Enabled: true }
                        : mapping
                      const isEnabled = mappingObj.Enabled !== false
                      const isSelected = selectedPath === path

                      return (
                        <button
                          key={path}
                          onClick={() => handleSelectPath(path)}
                          className={`w-full text-left px-3 py-2 rounded-md transition-colors ${
                            isSelected
                              ? 'bg-[#E8DDD0] text-[#2D2A26]'
                              : 'hover:bg-[#E8E5E0] text-[#3D3A36]'
                          }`}
                        >
                          <div className="flex items-center gap-2">
                            {isEnabled ? (
                              <CheckCircle2 className="w-4 h-4 text-[#518751] flex-shrink-0" />
                            ) : (
                              <Circle className="w-4 h-4 text-gray-400 flex-shrink-0" />
                            )}
                            <span className="font-mono text-sm truncate">{path}</span>
                          </div>
                        </button>
                      )
                    })}

                    {/* 新增路径按钮 */}
                    <Button
                      onClick={handleStartAddPath}
                      variant="outline"
                      className="w-full justify-start"
                      size="sm"
                    >
                      <Plus className="w-4 h-4 mr-2" />
                      新增路径
                    </Button>
                  </div>
                </div>

                {/* 右侧配置内容 */}
                <div className="flex-1 overflow-y-auto">
                  {isAddingPath ? (
                    <Card>
                      <CardHeader>
                        <CardTitle>新增路径</CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-4">
                        <div className="space-y-2">
                          <Label htmlFor="new-path">路径</Label>
                          <Input
                            id="new-path"
                            value={newPath}
                            onChange={(e) => setNewPath(e.target.value)}
                            placeholder="/example"
                          />
                        </div>
                        <div className="space-y-2">
                          <Label htmlFor="new-target">默认目标</Label>
                          <Input
                            id="new-target"
                            value={newTarget}
                            onChange={(e) => setNewTarget(e.target.value)}
                            placeholder="https://example.com"
                          />
                        </div>
                        <div className="flex items-center space-x-2">
                          <Switch
                            id="new-redirect"
                            checked={newRedirectMode}
                            onCheckedChange={setNewRedirectMode}
                          />
                          <Label htmlFor="new-redirect">使用302重定向模式</Label>
                        </div>
                        <div className="flex gap-2">
                          <Button onClick={handleAddPath}>保存</Button>
                          <Button variant="outline" onClick={() => setIsAddingPath(false)}>取消</Button>
                        </div>
                      </CardContent>
                    </Card>
                  ) : selectedPath && selectedMappingObj ? (
                    <div className="space-y-4">
                      {/* 统计信息 */}
                      <Card>
                        <CardHeader>
                          <CardTitle className="text-lg">统计信息</CardTitle>
                        </CardHeader>
                        <CardContent>
                          <PathStatsCard
                            stats={selectedStats}
                            onReset={() => handleResetPathStats(selectedPath)}
                          />
                        </CardContent>
                      </Card>

                      {/* 基本设置 */}
                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between">
                          <CardTitle className="text-lg">基本设置</CardTitle>
                          <div className="flex gap-2">
                            {!isEditingPath && (
                              <Button variant="outline" size="sm" onClick={handleStartEditPath}>
                                <Edit className="w-4 h-4 mr-2" />
                                编辑
                              </Button>
                            )}
                            {!isSystemPath && (
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={handleDeletePath}
                                className="text-destructive hover:text-destructive"
                              >
                                <Trash2 className="w-4 h-4 mr-2" />
                                删除
                              </Button>
                            )}
                          </div>
                        </CardHeader>
                        <CardContent className="space-y-4">
                          <div className="flex items-center justify-between">
                            <Label>当前路径</Label>
                            <span className="font-mono font-semibold">{selectedPath}</span>
                          </div>

                          <div className="flex items-center justify-between">
                            <Label>启用状态</Label>
                            <div className="flex items-center gap-2">
                              <Switch
                                checked={selectedMappingObj.Enabled !== false}
                                onCheckedChange={(checked) => handleToggleEnabled(selectedPath, checked)}
                                className="data-[state=checked]:bg-[#518751]"
                              />
                              <span className="text-sm text-muted-foreground">
                                {selectedMappingObj.Enabled !== false ? "已启用" : "已禁用"}
                              </span>
                            </div>
                          </div>

                          {isEditingPath ? (
                            <>
                              <div className="space-y-2">
                                <Label htmlFor="edit-target">默认目标</Label>
                                <Input
                                  id="edit-target"
                                  value={editedTarget}
                                  onChange={(e) => setEditedTarget(e.target.value)}
                                />
                              </div>
                              <div className="flex items-center space-x-2">
                                <Switch
                                  id="edit-redirect"
                                  checked={editedRedirectMode}
                                  onCheckedChange={setEditedRedirectMode}
                                />
                                <Label htmlFor="edit-redirect">使用302重定向模式</Label>
                              </div>
                              <div className="flex gap-2">
                                <Button onClick={handleSaveEditPath}>保存</Button>
                                <Button variant="outline" onClick={() => setIsEditingPath(false)}>取消</Button>
                              </div>
                            </>
                          ) : (
                            <>
                              <div className="flex items-start justify-between">
                                <Label>默认目标</Label>
                                <span className="text-sm text-muted-foreground break-all max-w-md text-right">
                                  {selectedMappingObj.DefaultTarget}
                                </span>
                              </div>
                              {selectedMappingObj.RedirectMode && (
                                <div className="flex items-center gap-2">
                                  <Badge variant="outline">302重定向模式</Badge>
                                </div>
                              )}
                            </>
                          )}
                        </CardContent>
                      </Card>

                      {/* 缓存配置 */}
                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between">
                          <CardTitle className="text-lg">缓存配置</CardTitle>
                          <div className="flex gap-2">
                            <Button variant="outline" size="sm" onClick={() => setCacheDialogOpen(true)}>
                              <Database className="w-4 h-4 mr-2" />
                              配置缓存
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handleClearPathCache(selectedPath)}
                              className="text-orange-600 hover:text-orange-700"
                            >
                              <Eraser className="w-4 h-4 mr-2" />
                              清理缓存
                            </Button>
                          </div>
                        </CardHeader>
                        <CardContent>
                          {selectedMappingObj.CacheConfig ? (
                            <div className="space-y-2 text-sm">
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">最大缓存时间:</span>
                                <span>{selectedMappingObj.CacheConfig.max_age} 分钟</span>
                              </div>
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">清理间隔:</span>
                                <span>{selectedMappingObj.CacheConfig.cleanup_tick} 分钟</span>
                              </div>
                              <div className="flex justify-between">
                                <span className="text-muted-foreground">最大缓存大小:</span>
                                <span>{selectedMappingObj.CacheConfig.max_cache_size} GB</span>
                              </div>
                            </div>
                          ) : (
                            <p className="text-sm text-muted-foreground">使用全局缓存配置</p>
                          )}
                        </CardContent>
                      </Card>

                      {/* 扩展名规则 */}
                      <Card>
                        <CardHeader className="flex flex-row items-center justify-between">
                          <CardTitle className="text-lg">
                            扩展名规则 ({selectedMappingObj.ExtensionMap?.length || 0})
                          </CardTitle>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleExtensionRuleEdit(selectedPath)}
                          >
                            <FileText className="w-4 h-4 mr-2" />
                            添加规则
                          </Button>
                        </CardHeader>
                        <CardContent>
                          {selectedMappingObj.ExtensionMap && selectedMappingObj.ExtensionMap.length > 0 ? (
                            <div className="space-y-2">
                              {selectedMappingObj.ExtensionMap.map((rule, index) => (
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
                                          大小范围: {rule.SizeThreshold ? convertBytesToUnit(rule.SizeThreshold).value + ' ' + convertBytesToUnit(rule.SizeThreshold).unit : "0"} - {rule.MaxSize ? convertBytesToUnit(rule.MaxSize).value + ' ' + convertBytesToUnit(rule.MaxSize).unit : "无限制"}
                                        </div>
                                      )}
                                    </div>
                                    <div className="flex gap-1 ml-2">
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => handleExtensionRuleEdit(selectedPath, index, rule)}
                                        className="h-8 w-8 p-0"
                                      >
                                        <Edit className="h-3 w-3" />
                                      </Button>
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => deleteExtensionRule(selectedPath, index)}
                                        className="h-8 w-8 p-0 text-destructive hover:text-destructive"
                                      >
                                        <Trash2 className="h-3 w-3" />
                                      </Button>
                                    </div>
                                  </div>
                                </div>
                              ))}
                            </div>
                          ) : (
                            <p className="text-sm text-muted-foreground">暂无扩展名规则</p>
                          )}
                        </CardContent>
                      </Card>
                    </div>
                  ) : (
                    <div className="flex items-center justify-center h-full text-muted-foreground">
                      <p>请选择一个路径或新增路径</p>
                    </div>
                  )}
                </div>
              </div>
            </TabsContent>

            <TabsContent value="cache" className="space-y-6">
              <CacheManagement />
            </TabsContent>

            <TabsContent value="security" className="space-y-6">
              <SecurityConfigPanel
                config={config?.Security || null}
                onUpdate={updateSecurity}
              />
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      {/* 删除路径确认对话框 */}
      <AlertDialog open={!!deletingPath} onOpenChange={(open) => !open && setDeletingPath(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除路径 &ldquo;{deletingPath}&rdquo; 的映射吗？此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDeletePath}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* 缓存配置对话框 */}
      {selectedPath && selectedMappingObj && (
        <PathCacheConfigDialog
          open={cacheDialogOpen}
          onOpenChange={setCacheDialogOpen}
          path={selectedPath}
          cacheConfig={selectedMappingObj.CacheConfig}
          onSave={(config) => handleCacheConfigUpdate(selectedPath, config)}
        />
      )}

      {/* 扩展名规则对话框 */}
      <ExtensionRuleDialog
        open={extensionRuleDialogOpen}
        onOpenChange={setExtensionRuleDialogOpen}
        editingRule={editingExtensionRule}
        newRule={newExtensionRule}
        onNewRuleChange={setNewExtensionRule}
        onSubmit={addOrUpdateExtensionRule}
      />

      {/* 删除扩展名规则确认对话框 */}
      <AlertDialog open={!!deletingExtensionRule} onOpenChange={(open) => !open && setDeletingExtensionRule(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除这个扩展名规则吗？此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDeleteExtensionRule}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
