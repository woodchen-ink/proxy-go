"use client"

import React, { useEffect, useState, useCallback, useRef } from "react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useToast } from "@/components/ui/use-toast"
import { fetchApi } from "@/lib/api-client"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Download, RefreshCw } from "lucide-react"
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
import PathCacheConfigDialog from "./components/PathCacheConfigDialog"
import SecurityConfigPanel from "./components/SecurityConfigPanel"
import ExtensionRuleDialog from "./components/ExtensionRuleDialog"
import CacheManagement from "./components/CacheManagement"
import CDNCacheManagement from "./components/CDNCacheManagement"
import PathList from "./components/PathList"
import PathDetail from "./components/PathDetail"
import PathForm, { type PathFormValue } from "./components/PathForm"
import { convertToBytes, convertBytesToUnit, getMappingTargets, targetsToText, parseTargetsText, buildTargetFields } from "./utils"

import {
  type CacheConfig,
  type Config,
  type ExtRuleConfig,
  type PathMapping,
  type RefererBanConfig,
  type RefererRedirectConfig,
  type SecurityConfig,
  toMapping,
} from "./types"

export default function ConfigPage() {
  const [config, setConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { toast } = useToast()

  // 选中的路径
  const [selectedPath, setSelectedPath] = useState<string | null>(null)

  // 删除确认
  const [deletingPath, setDeletingPath] = useState<string | null>(null)

  // 缓存配置对话框
  const [cacheDialogOpen, setCacheDialogOpen] = useState(false)

  // 新增 / 编辑路径共用一份表单状态; null 表示展示详情
  const [pathForm, setPathForm] = useState<{ mode: "add" | "edit"; value: PathFormValue } | null>(null)

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
  // versionRef 当前副本对应的配置版本, 保存时以 If-Match 回传; 中控发现版本已变 (别处改过) 会拒绝整份覆盖
  const versionRef = useRef<string | null>(null)

  const fetchConfig = useCallback(async () => {
    try {
      const response = await fetchApi("/admin/api/config/get")

      if (!response.ok) {
        throw new Error("获取配置失败")
      }
      versionRef.current = response.headers.get("X-Config-Version")

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

      // 仅在当前未选中任何路径时，初始化默认选中项
      if (data.MAP && Object.keys(data.MAP).length > 0) {
        const firstPath = Object.keys(data.MAP)[0]
        setSelectedPath((currentSelectedPath) => currentSelectedPath ?? firstPath)
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
  }, [toast])

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
      const headers: Record<string, string> = { "Content-Type": "application/json" }
      if (versionRef.current) headers["If-Match"] = versionRef.current
      const response = await fetchApi("/admin/api/config/save", {
        method: "POST",
        headers,
        body: JSON.stringify(config),
      })

      if (response.status === 409) {
        // 配置在别处被改过 (其他标签页 / 缓存与 CDN 页签): 不覆盖, 载入最新版本后让用户重做这次修改
        toast({
          title: "配置已在别处更新",
          description: "已载入最新配置, 刚才的修改没有保存, 请重新操作",
          variant: "destructive",
        })
        await fetchConfig()
        return
      }
      if (!response.ok) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.message || data.error || "保存配置失败")
      }
      const saved = await response.json().catch(() => ({}))
      if (saved.version !== undefined) versionRef.current = String(saved.version)

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
  }, [config, toast, fetchConfig])

  useEffect(() => {
    if (isInitialLoadRef.current || !config || isConfigFromApiRef.current) {
      isInitialLoadRef.current = false
      isConfigFromApiRef.current = false
      return
    }

    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current)
    }

    // 1.5~3s 随机延迟抖动，避免输入中间状态触发保存
    const jitterDelay = 1500 + Math.random() * 1500
    saveTimeoutRef.current = setTimeout(() => {
      handleSave()
    }, jitterDelay)

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
      const response = await fetchApi('/admin/api/config/pull', {
        method: 'POST',
      })

      if (!response.ok) {
        const errorText = await response.text()
        throw new Error(errorText || '从 D1 拉取配置失败')
      }

      versionRef.current = response.headers.get("X-Config-Version")
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

  // handleToggleCFImageOpt 切换路径的 Cloudflare Images 缓存键策略;
  // 关闭 (默认) 时所有设备共享同一份原文件缓存, 开启时按图片格式 + 浏览器分桶
  const handleToggleCFImageOpt = (path: string, enabled: boolean) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]

    if (typeof mapping === 'string') {
      newConfig.MAP[path] = {
        DefaultTarget: mapping,
        Enabled: true,
        CFImageOpt: enabled,
      }
    } else {
      newConfig.MAP[path] = {
        ...mapping,
        CFImageOpt: enabled,
      }
    }

    updateConfig(newConfig)
  }

  // handlePathRefererBanUpdate 整段替换路径级 RefererBan; 与全局规则叠加 (任一命中即拒)
  const handlePathRefererBanUpdate = (path: string, next: RefererBanConfig) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]
    const base: PathMapping =
      typeof mapping === "string" ? { DefaultTarget: mapping, Enabled: true } : mapping
    newConfig.MAP[path] = { ...base, RefererBan: next }
    updateConfig(newConfig)
  }

  // handlePathRefererRedirectUpdate 整段替换路径级 Referer 重定向; 命中即 302 分流, 优先于黑名单
  const handlePathRefererRedirectUpdate = (path: string, next: RefererRedirectConfig) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]
    const base: PathMapping =
      typeof mapping === "string" ? { DefaultTarget: mapping, Enabled: true } : mapping
    newConfig.MAP[path] = { ...base, RefererRedirect: next }
    updateConfig(newConfig)
  }

  const handleSelectPath = (path: string) => {
    setSelectedPath(path)
    setPathForm(null)
  }

  const handleStartAddPath = () => {
    setPathForm({ mode: "add", value: { path: "", targets: "", redirect: false } })
  }

  const handleStartEditPath = () => {
    if (!selectedPath || !config) return
    const mapping = toMapping(config.MAP[selectedPath])
    // 多行编辑: 一行一个回源, 第一行为主源
    setPathForm({
      mode: "edit",
      value: { path: selectedPath, targets: targetsToText(getMappingTargets(mapping)), redirect: mapping.RedirectMode || false },
    })
  }

  // pathFormError 实时校验路径; 编辑时允许保持原路径
  const pathFormError = (() => {
    if (!pathForm) return null
    const p = pathForm.value.path.trim()
    if (!p) return "路径不能为空"
    if (!p.startsWith("/")) return "路径必须以 / 开头"
    const original = pathForm.mode === "edit" ? selectedPath : null
    if (p !== original && config?.MAP[p] !== undefined) return `路径 ${p} 已存在`
    return null
  })()

  const handleSubmitPathForm = () => {
    if (!config || !pathForm || pathFormError) return
    const targets = parseTargetsText(pathForm.value.targets)
    if (targets.length === 0) {
      toast({ title: "错误", description: "至少需要一个回源目标", variant: "destructive" })
      return
    }
    const nextPath = pathForm.value.path.trim()
    const redirect = pathForm.value.redirect
    const map = { ...config.MAP }

    if (pathForm.mode === "add") {
      map[nextPath] = { ...buildTargetFields(targets), RedirectMode: redirect, Enabled: true, ExtensionMap: [] }
    } else {
      if (!selectedPath) return
      const updated: PathMapping = { ...toMapping(map[selectedPath]), ...buildTargetFields(targets), RedirectMode: redirect }
      // 重命名: 删旧键设新键
      if (nextPath !== selectedPath) delete map[selectedPath]
      map[nextPath] = updated
    }
    updateConfig({ ...config, MAP: map })
    setSelectedPath(nextPath)
    setPathForm(null)
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
      const response = await fetchApi("/admin/api/cache/clear-by-path", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "all",
          path_prefix: path
        }),
      })

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

  // handleClearPathCDNCache 通过 CDN 厂商一键清理当前路径前缀的远端缓存
  // 走 /admin/api/cdn/purge?type=prefixes, 自动用当前 origin 拼成完整 URL
  const handleClearPathCDNCache = async (path: string) => {
    try {
      const origin = typeof window !== "undefined" ? window.location.origin : ""
      const target = origin
        ? origin.replace(/\/+$/, "") + (path.endsWith("/") ? path : path + "/")
        : (path.endsWith("/") ? path : path + "/")

      const response = await fetchApi("/admin/api/cdn/purge", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ type: "prefixes", targets: [target] }),
      })

      const data = await response.json().catch(() => ({}))
      if (!response.ok || !data?.result?.success) {
        const msg = data?.result?.message || data?.error || "CDN 清理失败"
        if (response.status === 424) {
          throw new Error("未启用 CDN provider, 请先到 CDN 缓存 tab 配置")
        }
        throw new Error(msg)
      }

      toast({
        title: "CDN 已清理",
        description: data.result.job_id ? `任务 ID: ${data.result.job_id}` : `目标: ${target}`,
      })
    } catch (error) {
      toast({
        title: "CDN 清理失败",
        description: error instanceof Error ? error.message : "CDN 清理失败",
        variant: "destructive",
      })
    }
  }

  // updateSecurity 整段替换 Security 子树, Panel 内部负责保持其它字段不变
  const updateSecurity = (newSecurity: SecurityConfig) => {
    if (!config) return
    updateConfig({ ...config, Security: newSecurity })
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

  const selectedMappingObj: PathMapping | null =
    selectedPath && config && config.MAP[selectedPath] !== undefined ? toMapping(config.MAP[selectedPath]) : null
  const isSystemPath = selectedMappingObj?.DefaultTarget === "mirror"

  return (
    // 外壳 main 是滚动容器; 配置页锁定在一屏内, 两栏各自内部滚动 (扣除主区上下内边距与移动端顶栏)
    <div className="flex h-[calc(100svh-6rem)] flex-col gap-4 md:h-[calc(100svh-3rem)]">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3">
        <div className="flex items-baseline gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">配置</h1>
          {saving && <span className="text-sm text-foreground-subtle">正在保存…</span>}
        </div>
        <div className="flex gap-2">
          <Button onClick={exportConfig} variant="outline" size="sm">
            <Download className="h-4 w-4" />
            导出
          </Button>
          <Button onClick={pullConfigFromD1} variant="outline" size="sm">
            <RefreshCw className="h-4 w-4" />
            从 D1 拉取
          </Button>
        </div>
      </div>

      <Tabs defaultValue="paths" className="flex min-h-0 flex-1 flex-col gap-4">
        <TabsList className="self-start">
          <TabsTrigger value="paths">路径映射</TabsTrigger>
          <TabsTrigger value="cache">缓存管理</TabsTrigger>
          <TabsTrigger value="cdn">CDN 缓存</TabsTrigger>
          <TabsTrigger value="security">安全策略</TabsTrigger>
        </TabsList>

        <TabsContent value="paths" className="mt-0 min-h-0 flex-1 data-[state=inactive]:hidden">
          <Card className="grid h-full grid-rows-[minmax(0,12rem)_minmax(0,1fr)] overflow-hidden md:grid-cols-[15rem_minmax(0,1fr)] md:grid-rows-1">
            <div className="flex min-h-0 flex-col border-b bg-muted md:border-b-0 md:border-r">
              {loading ? (
                <div className="space-y-2 p-3">
                  {Array.from({ length: 8 }, (_, i) => (
                    <Skeleton key={i} className="h-7 w-full" />
                  ))}
                </div>
              ) : (
                <PathList
                  map={config?.MAP ?? {}}
                  selected={selectedPath}
                  adding={pathForm?.mode === "add"}
                  disabled={saving}
                  onSelect={handleSelectPath}
                  onAdd={handleStartAddPath}
                />
              )}
            </div>

            <div className="flex min-h-0 flex-col">
              {loading ? (
                <div className="space-y-4 p-5">
                  <Skeleton className="h-7 w-40" />
                  <Skeleton className="h-16 w-full" />
                  <Skeleton className="h-16 w-full" />
                </div>
              ) : pathForm ? (
                <div className="min-h-0 flex-1 overflow-y-auto">
                  <PathForm
                    title={pathForm.mode === "add" ? "新增路径" : `编辑 ${selectedPath}`}
                    value={pathForm.value}
                    error={pathFormError}
                    saving={saving}
                    onChange={(value) => setPathForm({ ...pathForm, value })}
                    onSubmit={handleSubmitPathForm}
                    onCancel={() => setPathForm(null)}
                  />
                </div>
              ) : selectedPath && selectedMappingObj ? (
                <PathDetail
                  path={selectedPath}
                  mapping={selectedMappingObj}
                  isSystemPath={isSystemPath}
                  saving={saving}
                  onEdit={handleStartEditPath}
                  onDelete={handleDeletePath}
                  onToggleEnabled={(v) => handleToggleEnabled(selectedPath, v)}
                  onToggleCFImageOpt={(v) => handleToggleCFImageOpt(selectedPath, v)}
                  onConfigureCache={() => setCacheDialogOpen(true)}
                  onClearCache={() => handleClearPathCache(selectedPath)}
                  onClearCDN={() => handleClearPathCDNCache(selectedPath)}
                  onRefererBan={(next) => handlePathRefererBanUpdate(selectedPath, next)}
                  onRefererRedirect={(next) => handlePathRefererRedirectUpdate(selectedPath, next)}
                  onAddExtRule={() => handleExtensionRuleEdit(selectedPath)}
                  onEditExtRule={(i, rule) => handleExtensionRuleEdit(selectedPath, i, rule)}
                  onDeleteExtRule={(i) => deleteExtensionRule(selectedPath, i)}
                />
              ) : (
                <div className="flex flex-1 items-center justify-center text-sm text-foreground-muted">
                  选择左侧路径查看配置, 或新增一个路径
                </div>
              )}
            </div>
          </Card>
        </TabsContent>

        <TabsContent value="cache" className="mt-0 min-h-0 flex-1 overflow-y-auto data-[state=inactive]:hidden">
          <CacheManagement />
        </TabsContent>

        <TabsContent value="cdn" className="mt-0 min-h-0 flex-1 overflow-y-auto data-[state=inactive]:hidden">
          <CDNCacheManagement />
        </TabsContent>

        <TabsContent value="security" className="mt-0 min-h-0 flex-1 overflow-y-auto data-[state=inactive]:hidden">
          <SecurityConfigPanel config={config?.Security || null} onUpdate={updateSecurity} />
        </TabsContent>
      </Tabs>

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
