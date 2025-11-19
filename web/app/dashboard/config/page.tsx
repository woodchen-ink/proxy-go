"use client"

import React, { useEffect, useState, useCallback, useRef } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { Plus, Trash2, Edit, Download, Upload, Shield } from "lucide-react"
import Link from "next/link"
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
import PathMappingItem from "./PathMappingItem"

interface ExtRuleConfig {
  Extensions: string;    // 逗号分隔的扩展名
  Target: string;        // 目标服务器
  SizeThreshold: number; // 最小阈值（字节）
  MaxSize: number;       // 最大阈值（字节）
  RedirectMode?: boolean; // 是否使用302跳转模式
  Domains?: string;      // 逗号分隔的域名列表，为空表示匹配所有域名
}

interface CacheConfig {
  max_age: number;        // 最大缓存时间（分钟）
  cleanup_tick: number;   // 清理间隔（分钟）
  max_cache_size: number; // 最大缓存大小（GB）
}

interface PathMapping {
  DefaultTarget: string
  ExtensionMap?: ExtRuleConfig[]  // 只支持新格式
  SizeThreshold?: number  // 保留全局阈值字段（向后兼容）
  MaxSize?: number       // 保留全局阈值字段（向后兼容）
  RedirectMode?: boolean  // 是否使用302跳转模式
  Enabled?: boolean       // 是否启用此路径
  CacheConfig?: CacheConfig // 独立缓存配置
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

  // 使用 ref 来保存滚动位置
  const scrollPositionRef = useRef(0)
  // 添加一个ref来跟踪是否是初始加载
  const isInitialLoadRef = useRef(true)
  // 添加一个标志来跟踪配置是否是从API加载的
  const isConfigFromApiRef = useRef(true)
  // 添加一个防抖定时器ref
  const saveTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // 对话框状态
  const [pathDialogOpen, setPathDialogOpen] = useState(false)
  const [newPathData, setNewPathData] = useState({
    path: "",
    defaultTarget: "",
    redirectMode: false,
    extensionMap: {} as Record<string, string>,
    sizeThreshold: 0,
    maxSize: 0,
    sizeThresholdUnit: 'MB' as 'B' | 'KB' | 'MB' | 'GB',
    maxSizeUnit: 'MB' as 'B' | 'KB' | 'MB' | 'GB',
  })

  const [editingPath, setEditingPath] = useState<string | null>(null)

  const [editingPathData, setEditingPathData] = useState<{
    path: string;
    defaultTarget: string;
    redirectMode: boolean;
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
  } | null>(null);

  const [deletingPath, setDeletingPath] = useState<string | null>(null)

  // 添加扩展名规则状态
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
  });

  const [editingExtensionRule, setEditingExtensionRule] = useState<{
    index: number,
    extensions: string;
    target: string;
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
    domains: string;
  } | null>(null);

  // 添加扩展名规则对话框状态
  const [extensionRuleDialogOpen, setExtensionRuleDialogOpen] = useState(false);

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
      
      // 确保安全配置存在
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
      
      isConfigFromApiRef.current = true // 标记配置来自API
      setConfig(data)

      // 获取路径统计信息
      try {
        const statsResponse = await fetch("/admin/api/path-stats", {
          headers: {
            'Authorization': `Bearer ${token}`,
            'Content-Type': 'application/json'
          }
        })
        if (statsResponse.ok) {
          const statsData = await statsResponse.json()
          console.log("路径统计数据:", statsData)
          setPathStats(statsData.path_stats || [])
        } else {
          console.error("获取路径统计失败，状态码:", statsResponse.status)
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
  }, [router, toast])

  // 创建一个包装的setConfig函数，用于用户修改配置时
  const updateConfig = useCallback((newConfig: Config) => {
    isConfigFromApiRef.current = false // 标记配置来自用户修改
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
  
  // 添加自动保存的useEffect
  useEffect(() => {
    // 如果是初始加载或者配置为空，不触发保存
    if (isInitialLoadRef.current || !config || isConfigFromApiRef.current) {
      isInitialLoadRef.current = false
      isConfigFromApiRef.current = false // 重置标志
      return
    }

    // 清除之前的定时器
    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current)
    }

    // 保存当前滚动位置
    const currentScrollPosition = window.scrollY

    // 设置新的定时器，延迟1秒后保存
    saveTimeoutRef.current = setTimeout(() => {
      handleSave().then(() => {
        // 保存完成后恢复滚动位置
        window.scrollTo(0, currentScrollPosition)
      })
    }, 1000)

    // 组件卸载时清除定时器
    return () => {
      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current)
      }
    }
  }, [config, handleSave]) // 监听config变化

 

  // 处理对话框打开和关闭时的滚动位置
  const handleDialogOpenChange = useCallback((open: boolean, handler: (open: boolean) => void) => {
    if (open) {
      // 对话框打开时，保存当前滚动位置
      scrollPositionRef.current = window.scrollY
    } else {
      // 对话框关闭时，恢复滚动位置
      handler(open)
      requestAnimationFrame(() => {
        window.scrollTo(0, scrollPositionRef.current)
      })
    }
  }, [])

  const handlePathDialogOpenChange = useCallback((open: boolean) => {
    handleDialogOpenChange(open, (isOpen) => {
      setPathDialogOpen(isOpen)
      if (!isOpen) {
        setEditingPathData(null)
        setNewPathData({
          path: "",
          defaultTarget: "",
          redirectMode: false,
          extensionMap: {},
          sizeThreshold: 0,
          maxSize: 0,
          sizeThresholdUnit: 'MB',
          maxSizeUnit: 'MB',
        })
      }
    })
  }, [handleDialogOpenChange])

  const addOrUpdatePath = () => {
    if (!config) return
    
    const data = editingPathData || newPathData
    const { path, defaultTarget } = data
    
    if (!path || !defaultTarget) {
      toast({
        title: "错误",
        description: "路径和默认目标不能为空",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    const pathConfig: PathMapping = {
      DefaultTarget: defaultTarget,
      RedirectMode: data.redirectMode,
      ExtensionMap: []
    }

    // 如果是编辑现有路径，保留原有的扩展名映射
    if (editingPathData && typeof config.MAP[path] === 'object') {
      const existingConfig = config.MAP[path] as PathMapping
      pathConfig.ExtensionMap = existingConfig.ExtensionMap
    }

    newConfig.MAP[path] = pathConfig
    updateConfig(newConfig)
    
    if (editingPathData) {
      setEditingPathData(null)
    } else {
      setNewPathData({
        path: "",
        defaultTarget: "",
        redirectMode: false,
        extensionMap: {},
        sizeThreshold: 0,
        maxSize: 0,
        sizeThresholdUnit: 'MB',
        maxSizeUnit: 'MB',
      })
    }
    
    setPathDialogOpen(false)
  }

  const deletePath = (path: string) => {
    setDeletingPath(path)
  }

  const confirmDeletePath = () => {
    if (!config || !deletingPath) return
    const newConfig = { ...config }
    delete newConfig.MAP[deletingPath]
    updateConfig(newConfig)
    setDeletingPath(null)
  }

  const handleCacheConfigUpdate = (path: string, cacheConfig: CacheConfig | null) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]

    if (typeof mapping === 'string') {
      // 将字符串格式转换为对象格式
      newConfig.MAP[path] = {
        DefaultTarget: mapping,
        Enabled: true,
        CacheConfig: cacheConfig || undefined,
      }
    } else {
      // 更新对象格式的缓存配置
      newConfig.MAP[path] = {
        ...mapping,
        CacheConfig: cacheConfig || undefined,
      }
    }

    updateConfig(newConfig)
  }

  const updateSecurity = (field: keyof SecurityConfig['IPBan'], value: boolean | number) => {
    if (!config) return
    const newConfig = { ...config }
    
    // 确保安全配置存在
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

  const handleExtensionMapEdit = (path: string) => {
    // 将添加规则的操作重定向到handleExtensionRuleEdit
    handleExtensionRuleEdit(path);
  };

  const deleteExtensionRule = (path: string, index: number) => {
    setDeletingExtensionRule({ path, index });
  };

  const openAddPathDialog = () => {
    setEditingPathData(null)
    setNewPathData({
      path: "",
      defaultTarget: "",
      redirectMode: false,
      extensionMap: {},
      sizeThreshold: 0,
      maxSize: 0,
      sizeThresholdUnit: 'MB',
      maxSizeUnit: 'MB',
    })
    setPathDialogOpen(true)
  }

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

  const importConfig = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    const reader = new FileReader()
    reader.onload = (e) => {
      try {
        const content = e.target?.result as string
        const newConfig = JSON.parse(content)

        // 验证配置结构
        if (!newConfig.MAP || typeof newConfig.MAP !== 'object') {
          throw new Error('配置文件缺少 MAP 字段或格式不正确')
        }

        if (!newConfig.Compression || 
            typeof newConfig.Compression !== 'object' ||
            !newConfig.Compression.Gzip ||
            !newConfig.Compression.Brotli) {
          throw new Error('配置文件压缩设置格式不正确')
        }

        // 如果没有安全配置，添加默认配置
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

        // 验证路径映射
        for (const [path, target] of Object.entries(newConfig.MAP)) {
          if (!path.startsWith('/')) {
            throw new Error(`路径 ${path} 必须以/开头`)
          }

          if (typeof target === 'string') {
            try {
              new URL(target)
            } catch {
              throw new Error(`路径 ${path} 的目标URL格式不正确`)
            }
          } else if (target && typeof target === 'object') {
            const mapping = target as PathMapping
            if (!mapping.DefaultTarget || typeof mapping.DefaultTarget !== 'string') {
              throw new Error(`路径 ${path} 的默认目标格式不正确`)
            }
            try {
              new URL(mapping.DefaultTarget)
            } catch {
              throw new Error(`路径 ${path} 的默认目标URL格式不正确`)
            }
          } else {
            throw new Error(`路径 ${path} 的目标格式不正确`)
          }
        }

        // 使用setConfig而不是updateConfig，因为导入的配置不应触发自动保存
        isConfigFromApiRef.current = true
        setConfig(newConfig)
        toast({
          title: "成功",
          description: "配置已导入",
        })
      } catch (error) {
        toast({
          title: "错误",
          description: error instanceof Error ? error.message : "配置文件格式错误",
          variant: "destructive",
        })
      }
    }
    reader.readAsText(file)
  }

  const handleEditPath = (path: string, target: PathMapping | string) => {
    if (typeof target === 'string') {
      setEditingPathData({
        path,
        defaultTarget: target,
        redirectMode: false,
        sizeThreshold: 0,
        maxSize: 0,
        sizeThresholdUnit: 'MB',
        maxSizeUnit: 'MB'
      })
    } else {
      const { value: thresholdValue, unit: thresholdUnit } = convertBytesToUnit(target.SizeThreshold || 0)
      const { value: maxValue, unit: maxUnit } = convertBytesToUnit(target.MaxSize || 0)
      setEditingPathData({
        path,
        defaultTarget: target.DefaultTarget,
        redirectMode: target.RedirectMode || false,
        sizeThreshold: thresholdValue,
        maxSize: maxValue,
        sizeThresholdUnit: thresholdUnit,
        maxSizeUnit: maxUnit
      })
    }
    setPathDialogOpen(true)
  }

  // 处理删除对话框的滚动位置
  const handleDeleteDialogOpenChange = useCallback((open: boolean, setter: (value: null) => void) => {
    if (open) {
      scrollPositionRef.current = window.scrollY
    } else {
      setter(null)
      requestAnimationFrame(() => {
        window.scrollTo(0, scrollPositionRef.current)
      })
    }
  }, [])

  // 为扩展名规则对话框添加处理函数
  const handleExtensionRuleDialogOpenChange = useCallback((open: boolean) => {
    handleDialogOpenChange(open, (isOpen) => {
      setExtensionRuleDialogOpen(isOpen);
      if (!isOpen) {
        setEditingExtensionRule(null);
        setNewExtensionRule({
          extensions: "",
          target: "",
          redirectMode: false,
          sizeThreshold: 0,
          maxSize: 0,
          sizeThresholdUnit: 'MB',
          maxSizeUnit: 'MB',
          domains: "",
        });
      }
    });
  }, [handleDialogOpenChange]);

  // 处理扩展名规则的编辑
  const handleExtensionRuleEdit = (path: string, index?: number, rule?: { Extensions: string; Target: string; SizeThreshold?: number; MaxSize?: number; RedirectMode?: boolean; Domains?: string }) => {
    setEditingPath(path);
    
    if (index !== undefined && rule) {
      // 转换规则的阈值到合适的单位显示
      const { value: thresholdValue, unit: thresholdUnit } = convertBytesToUnit(rule.SizeThreshold || 0);
      const { value: maxValue, unit: maxUnit } = convertBytesToUnit(rule.MaxSize || 0);
      
      setEditingExtensionRule({
        index,
        extensions: rule.Extensions, 
        target: rule.Target,
        sizeThreshold: thresholdValue,
        maxSize: maxValue,
        sizeThresholdUnit: thresholdUnit,
        maxSizeUnit: maxUnit,
        domains: rule.Domains || "",
      });
      
      // 同时更新表单显示数据
      setNewExtensionRule({
        extensions: rule.Extensions,
        target: rule.Target,
        redirectMode: rule.RedirectMode || false, // 正确读取RedirectMode字段
        sizeThreshold: thresholdValue,
        maxSize: maxValue,
        sizeThresholdUnit: thresholdUnit,
        maxSizeUnit: maxUnit,
        domains: rule.Domains || "",
      });
    } else {
      setEditingExtensionRule(null);
      // 重置表单
      setNewExtensionRule({
        extensions: "",
        target: "",
        redirectMode: false,
        sizeThreshold: 0,
        maxSize: 0,
        sizeThresholdUnit: 'MB',
        maxSizeUnit: 'MB',
        domains: "",
      });
    }
    
    setExtensionRuleDialogOpen(true);
  };

  // 添加或更新扩展名规则
  const addOrUpdateExtensionRule = () => {
    if (!config || !editingPath) return;
    
    const { extensions, target, redirectMode, sizeThreshold, maxSize, sizeThresholdUnit, maxSizeUnit, domains } = newExtensionRule;
    
    // 验证输入
    if (!extensions.trim() || !target.trim()) {
      toast({
        title: "错误",
        description: "扩展名和目标不能为空",
        variant: "destructive",
      });
      return;
    }

    // 验证扩展名格式
    const extensionList = extensions.split(',').map(e => e.trim());
    if (extensionList.some(e => !e || (e !== "*" && e.includes('.')))) {
      toast({
        title: "错误",
        description: "扩展名格式不正确，不需要包含点号",
        variant: "destructive",
      });
      return;
    }

    // 验证URL格式
    try {
      new URL(target);
    } catch {
      toast({
        title: "错误",
        description: "目标URL格式不正确",
        variant: "destructive",
      });
      return;
    }

    // 验证域名格式（如果提供）
    if (domains.trim()) {
      const domainList = domains.split(',').map(d => d.trim());
      const domainRegex = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
      
      for (const domain of domainList) {
        if (domain && !domainRegex.test(domain)) {
          toast({
            title: "错误",
            description: `域名格式不正确: ${domain}`,
            variant: "destructive",
          });
          return;
        }
      }
    }

    // 转换大小为字节
    const sizeThresholdBytes = convertToBytes(sizeThreshold, sizeThresholdUnit);
    const maxSizeBytes = convertToBytes(maxSize, maxSizeUnit);

    // 验证阈值
    if (maxSizeBytes > 0 && sizeThresholdBytes >= maxSizeBytes) {
      toast({
        title: "错误",
        description: "最大阈值必须大于最小阈值",
        variant: "destructive",
      });
      return;
    }

    const newConfig = { ...config };
    const mapping = newConfig.MAP[editingPath];
    
    if (typeof mapping === "string") {
      // 如果映射是字符串，创建新的PathConfig对象
      newConfig.MAP[editingPath] = {
        DefaultTarget: mapping,
        ExtensionMap: [{
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes,
          RedirectMode: redirectMode,
          Domains: domains.trim() || undefined
        }]
      };
    } else {
      // 确保ExtensionMap是数组
      if (!Array.isArray(mapping.ExtensionMap)) {
        mapping.ExtensionMap = [];
      }
      
      if (editingExtensionRule) {
        // 更新现有规则
        const rules = mapping.ExtensionMap as ExtRuleConfig[];
        rules[editingExtensionRule.index] = {
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes,
          RedirectMode: redirectMode,
          Domains: domains.trim() || undefined
        };
      } else {
        // 添加新规则
        mapping.ExtensionMap.push({
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes,
          RedirectMode: redirectMode,
          Domains: domains.trim() || undefined
        });
      }
    }

    updateConfig(newConfig);
    setExtensionRuleDialogOpen(false);
    setEditingExtensionRule(null);
    setNewExtensionRule({
      extensions: "",
      target: "",
      redirectMode: false,
      sizeThreshold: 0,
      maxSize: 0,
      sizeThresholdUnit: 'MB',
      maxSizeUnit: 'MB',
      domains: "",
    });
  };

  // 删除扩展名规则
  const [deletingExtensionRule, setDeletingExtensionRule] = useState<{path: string, index: number} | null>(null);

  const confirmDeleteExtensionRule = () => {
    if (!config || !deletingExtensionRule) return;
    
    const newConfig = { ...config };
    const mapping = newConfig.MAP[deletingExtensionRule.path];
    
    if (typeof mapping !== "string" && Array.isArray(mapping.ExtensionMap)) {
      // 移除指定索引的规则
      const rules = mapping.ExtensionMap as ExtRuleConfig[];
      mapping.ExtensionMap = [
        ...rules.slice(0, deletingExtensionRule.index),
        ...rules.slice(deletingExtensionRule.index + 1)
      ];
    }
    
    updateConfig(newConfig);
    setDeletingExtensionRule(null);
  };

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
            <label>
              <Button variant="outline" className="cursor-pointer">
                <Upload className="w-4 h-4 mr-2" />
                导入配置
              </Button>
              <input
                type="file"
                className="hidden"
                accept=".json"
                onChange={importConfig}
              />
            </label>
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
              <TabsTrigger value="security">安全策略</TabsTrigger>
            </TabsList>

            <TabsContent value="paths" className="space-y-4">
              <div className="flex justify-end">
                <Dialog open={pathDialogOpen} onOpenChange={handlePathDialogOpenChange}>
                  <DialogTrigger asChild>
                    <Button onClick={openAddPathDialog}>
                      <Plus className="w-4 h-4 mr-2" />
                      添加路径
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>{editingPathData ? "编辑路径映射" : "添加路径映射"}</DialogTitle>
                    </DialogHeader>
                    <div className="space-y-4">
                      <div className="space-y-2">
                        <Label>路径 (如: /images)</Label>
                        <Input
                          value={editingPathData ? editingPathData.path : newPathData.path}
                          onChange={(e) => editingPathData 
                            ? setEditingPathData({ ...editingPathData, path: e.target.value })
                            : setNewPathData({ ...newPathData, path: e.target.value })
                          }
                          placeholder="/example"
                        />
                        <p className="text-sm text-muted-foreground">
                          请输入需要代理的路径
                        </p>
                      </div>
                      <div className="space-y-2">
                        <Label>默认目标</Label>
                        <Input
                          value={editingPathData ? editingPathData.defaultTarget : newPathData.defaultTarget}
                          onChange={(e) => editingPathData
                            ? setEditingPathData({ ...editingPathData, defaultTarget: e.target.value })
                            : setNewPathData({ ...newPathData, defaultTarget: e.target.value })
                          }
                          placeholder="https://example.com"
                        />
                        <p className="text-sm text-muted-foreground">
                          默认的回源地址，所有请求都会转发到这个地址
                        </p>
                      </div>
                      <div className="flex items-center justify-between">
                        <Label>使用302跳转</Label>
                        <Switch
                          checked={editingPathData ? editingPathData.redirectMode : newPathData.redirectMode}
                          onCheckedChange={(checked) => editingPathData
                            ? setEditingPathData({ ...editingPathData, redirectMode: checked })
                            : setNewPathData({ ...newPathData, redirectMode: checked })
                          }
                        />
                      </div>
                      <p className="text-sm text-muted-foreground">
                        启用后，访问此路径时将302跳转到目标URL，而不是代理转发
                      </p>
                      <Button onClick={addOrUpdatePath}>
                        {editingPathData ? "保存" : "添加"}
                      </Button>
                    </div>
                  </DialogContent>
                </Dialog>
              </div>

              <div className="space-y-4">
                {config && Object.entries(config.MAP).map(([path, mapping]) => {
                  const stats = pathStats.find(s => s.path === path)
                  const isSystemPath = typeof mapping === 'object' && mapping.DefaultTarget === 'mirror'

                  return (
                    <PathMappingItem
                      key={path}
                      path={path}
                      mapping={mapping}
                      stats={stats}
                      isSystemPath={isSystemPath}
                      onEdit={(p) => handleEditPath(p, mapping)}
                      onDelete={deletePath}
                      onCacheConfigUpdate={handleCacheConfigUpdate}
                      onExtensionMapEdit={handleExtensionMapEdit}
                      onExtensionRuleEdit={handleExtensionRuleEdit}
                      onExtensionRuleDelete={deleteExtensionRule}
                    />
                  )
                })}
              </div>
            </TabsContent>

            <TabsContent value="security" className="space-y-6">
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
                      checked={config?.Security?.IPBan?.Enabled || false}
                      onCheckedChange={(checked) => updateSecurity('Enabled', checked)}
                    />
                  </div>
                  
                  {config?.Security?.IPBan?.Enabled && (
                    <>
                      <div className="space-y-2">
                        <Label>404 错误阈值</Label>
                        <Input
                          type="number"
                          min={1}
                          max={100}
                          value={config?.Security?.IPBan?.ErrorThreshold || 10}
                          onChange={(e) => updateSecurity('ErrorThreshold', parseInt(e.target.value) || 10)}
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
                          value={config?.Security?.IPBan?.WindowMinutes || 5}
                          onChange={(e) => updateSecurity('WindowMinutes', parseInt(e.target.value) || 5)}
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
                          value={config?.Security?.IPBan?.BanDurationMinutes || 5}
                          onChange={(e) => updateSecurity('BanDurationMinutes', parseInt(e.target.value) || 5)}
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
                          value={config?.Security?.IPBan?.CleanupIntervalMinutes || 1}
                          onChange={(e) => updateSecurity('CleanupIntervalMinutes', parseInt(e.target.value) || 1)}
                        />
                        <p className="text-sm text-muted-foreground">
                          清理过期记录的间隔时间
                        </p>
                      </div>
                    </>
                  )}
                </CardContent>
              </Card>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      <AlertDialog 
        open={!!deletingPath} 
        onOpenChange={(open) => handleDeleteDialogOpenChange(open, setDeletingPath)}
      >
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
      
      <Dialog open={extensionRuleDialogOpen} onOpenChange={handleExtensionRuleDialogOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editingExtensionRule ? "编辑扩展名规则" : "添加扩展名规则"}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>扩展名</Label>
              <Input
                value={newExtensionRule.extensions}
                onChange={(e) => setNewExtensionRule({ ...newExtensionRule, extensions: e.target.value })}
                placeholder="jpg,png,webp"
              />
              <p className="text-sm text-muted-foreground">
                多个扩展名用逗号分隔，不需要包含点号。使用星号 * 表示匹配所有未指定的扩展名。
              </p>
            </div>
            <div className="space-y-2">
              <Label>目标 URL</Label>
              <Input
                value={newExtensionRule.target}
                onChange={(e) => setNewExtensionRule({ ...newExtensionRule, target: e.target.value })}
                placeholder="https://example.com"
              />
            </div>
            <div className="space-y-2">
              <Label>限制域名（可选）</Label>
              <Input
                value={newExtensionRule.domains}
                onChange={(e) => setNewExtensionRule({ ...newExtensionRule, domains: e.target.value })}
                placeholder="a.com,b.com"
              />
              <p className="text-sm text-muted-foreground">
                指定该规则适用的域名，多个域名用逗号分隔。留空表示适用于所有域名。
              </p>
            </div>
            <div className="flex items-center justify-between">
              <Label>使用302跳转</Label>
              <Switch
                checked={newExtensionRule.redirectMode}
                onCheckedChange={(checked) => setNewExtensionRule({ ...newExtensionRule, redirectMode: checked })}
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
                    value={newExtensionRule.sizeThreshold}
                    onChange={(e) => {
                      setNewExtensionRule({
                        ...newExtensionRule,
                        sizeThreshold: Number(e.target.value),
                      });
                    }}
                  />
                  <select
                    className="w-24 rounded-md border border-input bg-background px-3"
                    value={newExtensionRule.sizeThresholdUnit}
                    onChange={(e) => {
                      setNewExtensionRule({
                        ...newExtensionRule,
                        sizeThresholdUnit: e.target.value as 'B' | 'KB' | 'MB' | 'GB',
                      });
                    }}
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
                    value={newExtensionRule.maxSize}
                    onChange={(e) => {
                      setNewExtensionRule({
                        ...newExtensionRule,
                        maxSize: Number(e.target.value),
                      });
                    }}
                  />
                  <select
                    className="w-24 rounded-md border border-input bg-background px-3"
                    value={newExtensionRule.maxSizeUnit}
                    onChange={(e) => {
                      setNewExtensionRule({
                        ...newExtensionRule,
                        maxSizeUnit: e.target.value as 'B' | 'KB' | 'MB' | 'GB',
                      });
                    }}
                  >
                    <option value="B">B</option>
                    <option value="KB">KB</option>
                    <option value="MB">MB</option>
                    <option value="GB">GB</option>
                  </select>
                </div>
              </div>
            </div>
            <Button onClick={addOrUpdateExtensionRule}>
              {editingExtensionRule ? "保存" : "添加"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog 
        open={!!deletingExtensionRule} 
        onOpenChange={(open) => handleDeleteDialogOpenChange(open, () => setDeletingExtensionRule(null))}
      >
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

// 辅助函数：格式化字节大小
const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// 辅助函数：截断 URL
const truncateUrl = (url: string) => {
  if (url.length > 30) {
    return url.substring(0, 27) + '...'
  }
  return url
}

// 辅助函数：单位转换
const convertToBytes = (value: number, unit: 'B' | 'KB' | 'MB' | 'GB'): number => {
  if (value < 0) return 0
  const multipliers = {
    'B': 1,
    'KB': 1024,
    'MB': 1024 * 1024,
    'GB': 1024 * 1024 * 1024
  }
  return Math.floor(value * multipliers[unit])
}

const convertBytesToUnit = (bytes: number): { value: number, unit: 'B' | 'KB' | 'MB' | 'GB' } => {
  if (bytes <= 0) return { value: 0, unit: 'MB' }
  const k = 1024
  const sizes: Array<'B' | 'KB' | 'MB' | 'GB'> = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return {
    value: Number((bytes / Math.pow(k, i)).toFixed(2)),
    unit: sizes[i]
  }
}
