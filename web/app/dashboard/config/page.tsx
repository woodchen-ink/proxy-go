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
import { Slider } from "@/components/ui/slider"
import { Plus, Trash2, Edit, Download, Upload } from "lucide-react"
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

interface ExtRuleConfig {
  Extensions: string;    // 逗号分隔的扩展名
  Target: string;        // 目标服务器
  SizeThreshold: number; // 最小阈值（字节）
  MaxSize: number;       // 最大阈值（字节）
  RedirectMode?: boolean; // 是否使用302跳转模式
}

interface PathMapping {
  DefaultTarget: string
  ExtensionMap?: ExtRuleConfig[]  // 只支持新格式
  SizeThreshold?: number  // 保留全局阈值字段（向后兼容）
  MaxSize?: number       // 保留全局阈值字段（向后兼容）
  RedirectMode?: boolean  // 是否使用302跳转模式
}

interface CompressionConfig {
  Enabled: boolean
  Level: number
}

interface Config {
  MAP: Record<string, PathMapping | string>
  Compression: {
    Gzip: CompressionConfig
    Brotli: CompressionConfig
  }
}

export default function ConfigPage() {
  const [config, setConfig] = useState<Config | null>(null)
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
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
  }>({
    extensions: "",
    target: "",
    sizeThreshold: 0,
    maxSize: 0,
    sizeThresholdUnit: 'MB',
    maxSizeUnit: 'MB',
  });

  const [editingExtensionRule, setEditingExtensionRule] = useState<{
    index: number,
    extensions: string;
    target: string;
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
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
      isConfigFromApiRef.current = true // 标记配置来自API
      setConfig(data)
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

  const updateCompression = (type: 'Gzip' | 'Brotli', field: 'Enabled' | 'Level', value: boolean | number) => {
    if (!config) return
    const newConfig = { ...config }
    if (field === 'Enabled') {
      newConfig.Compression[type].Enabled = value as boolean
    } else {
      newConfig.Compression[type].Level = value as number
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
          sizeThreshold: 0,
          maxSize: 0,
          sizeThresholdUnit: 'MB',
          maxSizeUnit: 'MB',
        });
      }
    });
  }, [handleDialogOpenChange]);

  // 处理扩展名规则的编辑
  const handleExtensionRuleEdit = (path: string, index?: number, rule?: { Extensions: string; Target: string; SizeThreshold?: number; MaxSize?: number }) => {
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
      });
      
      // 同时更新表单显示数据
      setNewExtensionRule({
        extensions: rule.Extensions,
        target: rule.Target,
        sizeThreshold: thresholdValue,
        maxSize: maxValue,
        sizeThresholdUnit: thresholdUnit,
        maxSizeUnit: maxUnit,
      });
    } else {
      setEditingExtensionRule(null);
      // 重置表单
      setNewExtensionRule({
        extensions: "",
        target: "",
        sizeThreshold: 0,
        maxSize: 0,
        sizeThresholdUnit: 'MB',
        maxSizeUnit: 'MB',
      });
    }
    
    setExtensionRuleDialogOpen(true);
  };

  // 添加或更新扩展名规则
  const addOrUpdateExtensionRule = () => {
    if (!config || !editingPath) return;
    
    const { extensions, target, sizeThreshold, maxSize, sizeThresholdUnit, maxSizeUnit } = newExtensionRule;
    
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
          MaxSize: maxSizeBytes
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
          MaxSize: maxSizeBytes
        };
      } else {
        // 添加新规则
        mapping.ExtensionMap.push({
          Extensions: extensions,
          Target: target,
          SizeThreshold: sizeThresholdBytes,
          MaxSize: maxSizeBytes
        });
      }
    }

    updateConfig(newConfig);
    setExtensionRuleDialogOpen(false);
    setEditingExtensionRule(null);
    setNewExtensionRule({
      extensions: "",
      target: "",
      sizeThreshold: 0,
      maxSize: 0,
      sizeThresholdUnit: 'MB',
      maxSizeUnit: 'MB',
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
              <TabsTrigger value="compression">压缩设置</TabsTrigger>
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
                      <Button onClick={addOrUpdatePath}>
                        {editingPathData ? "保存" : "添加"}
                      </Button>
                    </div>
                  </DialogContent>
                </Dialog>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {config && Object.entries(config.MAP).map(([path, target]) => (
                  <Card key={`${path}-card`} className="overflow-hidden">
                    <CardHeader className="pb-2">
                      <CardTitle className="text-lg flex justify-between items-center">
                        <span className="font-medium truncate" title={path}>{path}</span>
                        <div className="flex space-x-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8"
                            onClick={() => handleEditPath(path, target)}
                          >
                            <Edit className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8"
                            onClick={() => deletePath(path)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </CardTitle>
                    </CardHeader>
                    <CardContent className="pb-3">
                      <div className="text-sm text-muted-foreground mb-3">
                        <span className="font-medium text-primary">默认目标: </span>
                        <span className="break-all">{typeof target === 'string' ? target : target.DefaultTarget}</span>
                      </div>
                      
                      <Button
                        variant="outline"
                        size="sm"
                        className="w-full"
                        onClick={() => handleExtensionMapEdit(path)}
                      >
                        <Plus className="w-4 h-4 mr-2" />
                        添加规则
                      </Button>
                      
                      {typeof target === 'object' && target.ExtensionMap && Array.isArray(target.ExtensionMap) && target.ExtensionMap.length > 0 && (
                        <div className="mt-4">
                          <div className="text-sm font-semibold mb-2">扩展名映射规则</div>
                          <div className="space-y-2 max-h-[250px] overflow-y-auto pr-1">
                            {target.ExtensionMap.map((rule, index) => (
                              <div 
                                key={`${path}-rule-${index}`} 
                                className="bg-muted/30 rounded-md p-2 text-xs"
                              >
                                <div className="flex justify-between mb-1">
                                  <span className="font-semibold">{rule.Extensions}</span>
                                  <div className="flex space-x-1">
                                    <Button
                                      variant="ghost"
                                      size="icon"
                                      className="h-5 w-5"
                                      onClick={() => handleExtensionRuleEdit(path, index, rule)}
                                    >
                                      <Edit className="h-3 w-3" />
                                    </Button>
                                    <Button
                                      variant="ghost"
                                      size="icon"
                                      className="h-5 w-5"
                                      onClick={() => deleteExtensionRule(path, index)}
                                    >
                                      <Trash2 className="h-3 w-3" />
                                    </Button>
                                  </div>
                                </div>
                                <div className="text-muted-foreground truncate" title={rule.Target}>
                                  目标: {truncateUrl(rule.Target)}
                                </div>
                                <div className="flex justify-between mt-1 text-muted-foreground">
                                  <div>阈值: {formatBytes(rule.SizeThreshold || 0)}</div>
                                  <div>最大: {formatBytes(rule.MaxSize || 0)}</div>
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            </TabsContent>

            <TabsContent value="compression" className="space-y-6">
              <Card>
                <CardHeader>
                  <CardTitle>Gzip 压缩</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="flex items-center justify-between">
                    <Label>启用 Gzip</Label>
                    <Switch
                      checked={config?.Compression.Gzip.Enabled}
                      onCheckedChange={(checked) => updateCompression('Gzip', 'Enabled', checked)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>压缩级别 (1-9)</Label>
                    <Slider
                      min={1}
                      max={9}
                      step={1}
                      value={[config?.Compression.Gzip.Level || 6]}
                      onValueChange={(value: number[]) => updateCompression('Gzip', 'Level', value[0])}
                    />
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <CardTitle>Brotli 压缩</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="flex items-center justify-between">
                    <Label>启用 Brotli</Label>
                    <Switch
                      checked={config?.Compression.Brotli.Enabled}
                      onCheckedChange={(checked) => updateCompression('Brotli', 'Enabled', checked)}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>压缩级别 (1-11)</Label>
                    <Slider
                      min={1}
                      max={11}
                      step={1}
                      value={[config?.Compression.Brotli.Level || 4]}
                      onValueChange={(value: number[]) => updateCompression('Brotli', 'Level', value[0])}
                    />
                  </div>
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
