"use client"

import { useEffect, useState, useCallback, useRef } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { Slider } from "@/components/ui/slider"
import { Plus, Trash2, Edit, Save, Download, Upload } from "lucide-react"
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

interface PathMapping {
  DefaultTarget: string
  ExtensionMap?: Record<string, string>
  SizeThreshold?: number  // 最小文件大小阈值
  MaxSize?: number       // 最大文件大小阈值
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
  MetricsSaveInterval?: number  // 指标保存间隔（分钟）
  MetricsMaxFiles?: number      // 保留的最大统计文件数量
}

export default function ConfigPage() {
  const [config, setConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { toast } = useToast()
  const router = useRouter()

  // 使用 ref 来保存滚动位置
  const scrollPositionRef = useRef(0)

  // 对话框状态
  const [pathDialogOpen, setPathDialogOpen] = useState(false)
  const [newPathData, setNewPathData] = useState({
    path: "",
    defaultTarget: "",
    extensionMap: {} as Record<string, string>,
    sizeThreshold: 0,
    maxSize: 0,
    sizeThresholdUnit: 'MB' as 'B' | 'KB' | 'MB' | 'GB',
    maxSizeUnit: 'MB' as 'B' | 'KB' | 'MB' | 'GB',
  })

  const [extensionMapDialogOpen, setExtensionMapDialogOpen] = useState(false)
  const [editingPath, setEditingPath] = useState<string | null>(null)
  const [editingExtension, setEditingExtension] = useState<{ext: string, target: string} | null>(null)
  const [newExtension, setNewExtension] = useState({ ext: "", target: "" })

  const [editingPathData, setEditingPathData] = useState<{
    path: string;
    defaultTarget: string;
    sizeThreshold: number;
    maxSize: number;
    sizeThresholdUnit: 'B' | 'KB' | 'MB' | 'GB';
    maxSizeUnit: 'B' | 'KB' | 'MB' | 'GB';
  } | null>(null);

  const [deletingPath, setDeletingPath] = useState<string | null>(null)
  const [deletingExtension, setDeletingExtension] = useState<{path: string, ext: string} | null>(null)

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
      
      // 设置默认值
      if (data.MetricsSaveInterval === undefined || data.MetricsSaveInterval === 0) {
        data.MetricsSaveInterval = 15; // 默认15分钟
      }
      
      if (data.MetricsMaxFiles === undefined || data.MetricsMaxFiles === 0) {
        data.MetricsMaxFiles = 10; // 默认10个文件
      }
      
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

  useEffect(() => {
    fetchConfig()
  }, [fetchConfig])

  const handleSave = async () => {
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
        description: "配置已保存",
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
  }

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
          extensionMap: {},
          sizeThreshold: 0,
          maxSize: 0,
          sizeThresholdUnit: 'MB',
          maxSizeUnit: 'MB',
        })
      }
    })
  }, [handleDialogOpenChange])

  const handleExtensionMapDialogOpenChange = useCallback((open: boolean) => {
    handleDialogOpenChange(open, (isOpen) => {
      setExtensionMapDialogOpen(isOpen)
      if (!isOpen) {
        setEditingPath(null)
        setEditingExtension(null)
        setNewExtension({ ext: "", target: "" })
      }
    })
  }, [handleDialogOpenChange])

  const addOrUpdatePath = () => {
    if (!config) return
    
    const data = editingPathData || newPathData
    const { path, defaultTarget, sizeThreshold, maxSize, sizeThresholdUnit, maxSizeUnit } = data
    
    if (!path || !defaultTarget) {
      toast({
        title: "错误",
        description: "路径和默认目标不能为空",
        variant: "destructive",
      })
      return
    }

    // 转换大小为字节
    const sizeThresholdBytes = convertToBytes(sizeThreshold, sizeThresholdUnit)
    const maxSizeBytes = convertToBytes(maxSize, maxSizeUnit)

    // 验证阈值
    if (maxSizeBytes > 0 && sizeThresholdBytes >= maxSizeBytes) {
      toast({
        title: "错误",
        description: "最大文件大小阈值必须大于最小文件大小阈值",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    const pathConfig: PathMapping = {
      DefaultTarget: defaultTarget,
      ExtensionMap: {},
      SizeThreshold: sizeThresholdBytes,
      MaxSize: maxSizeBytes
    }

    // 如果是编辑现有路径，保留原有的扩展名映射
    if (editingPathData && typeof config.MAP[path] === 'object') {
      const existingConfig = config.MAP[path] as PathMapping
      pathConfig.ExtensionMap = existingConfig.ExtensionMap
    }

    newConfig.MAP[path] = pathConfig
    setConfig(newConfig)
    
    if (editingPathData) {
      setEditingPathData(null)
    } else {
      setNewPathData({
        path: "",
        defaultTarget: "",
        extensionMap: {},
        sizeThreshold: 0,
        maxSize: 0,
        sizeThresholdUnit: 'MB',
        maxSizeUnit: 'MB',
      })
    }
    
    setPathDialogOpen(false)
    
    toast({
      title: "成功",
      description: `${editingPathData ? '更新' : '添加'}路径配置成功`,
    })
  }

  const deletePath = (path: string) => {
    setDeletingPath(path)
  }

  const confirmDeletePath = () => {
    if (!config || !deletingPath) return
    const newConfig = { ...config }
    delete newConfig.MAP[deletingPath]
    setConfig(newConfig)
    setDeletingPath(null)
    toast({
      title: "成功",
      description: "路径映射已删除",
    })
  }

  const updateCompression = (type: 'Gzip' | 'Brotli', field: 'Enabled' | 'Level', value: boolean | number) => {
    if (!config) return
    const newConfig = { ...config }
    if (field === 'Enabled') {
      newConfig.Compression[type].Enabled = value as boolean
    } else {
      newConfig.Compression[type].Level = value as number
    }
    setConfig(newConfig)
  }

  const updateMetricsSettings = (field: 'MetricsSaveInterval' | 'MetricsMaxFiles', value: number) => {
    if (!config) return
    const newConfig = { ...config }
    newConfig[field] = value
    setConfig(newConfig)
  }

  const handleExtensionMapEdit = (path: string, ext?: string, target?: string) => {
    setEditingPath(path)
    if (ext && target) {
      setEditingExtension({ ext, target })
      setNewExtension({ ext, target })
    } else {
      setEditingExtension(null)
      setNewExtension({ ext: "", target: "" })
    }
    setExtensionMapDialogOpen(true)
  }

  const addOrUpdateExtensionMap = () => {
    if (!config || !editingPath) return
    const { ext, target } = newExtension
    
    // 验证输入
    if (!ext.trim() || !target.trim()) {
      toast({
        title: "错误",
        description: "扩展名和目标不能为空",
        variant: "destructive",
      })
      return
    }

    // 验证扩展名格式
    const extensions = ext.split(',').map(e => e.trim())
    if (extensions.some(e => !e || e.includes('.'))) {
      toast({
        title: "错误",
        description: "扩展名格式不正确，不需要包含点号",
        variant: "destructive",
      })
      return
    }

    // 验证URL格式
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

    const newConfig = { ...config }
    const mapping = newConfig.MAP[editingPath]
    if (typeof mapping === "string") {
      newConfig.MAP[editingPath] = {
        DefaultTarget: mapping,
        ExtensionMap: { [ext]: target }
      }
    } else {
      // 如果是编辑现有的扩展名映射，先删除旧的
      if (editingExtension) {
        const newExtMap = { ...mapping.ExtensionMap }
        delete newExtMap[editingExtension.ext]
        mapping.ExtensionMap = newExtMap
      }
      // 添加新的映射
      mapping.ExtensionMap = {
        ...mapping.ExtensionMap,
        [ext]: target
      }
    }

    setConfig(newConfig)
    setExtensionMapDialogOpen(false)
    setEditingExtension(null)
    setNewExtension({ ext: "", target: "" })
    
    toast({
      title: "成功",
      description: "扩展名映射已更新",
    })
  }

  const deleteExtensionMap = (path: string, ext: string) => {
    setDeletingExtension({ path, ext })
  }

  const confirmDeleteExtensionMap = () => {
    if (!config || !deletingExtension) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[deletingExtension.path]
    if (typeof mapping !== "string" && mapping.ExtensionMap) {
      const newExtensionMap = { ...mapping.ExtensionMap }
      delete newExtensionMap[deletingExtension.ext]
      mapping.ExtensionMap = newExtensionMap
    }
    setConfig(newConfig)
    setDeletingExtension(null)
    toast({
      title: "成功",
      description: "扩展名映射已删除",
    })
  }

  const openAddPathDialog = () => {
    setEditingPathData(null)
    setNewPathData({
      path: "",
      defaultTarget: "",
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
            <Button onClick={handleSave} disabled={saving}>
              <Save className="w-4 h-4 mr-2" />
              {saving ? "保存中..." : "保存配置"}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <Tabs defaultValue="paths" className="space-y-4">
            <TabsList>
              <TabsTrigger value="paths">路径映射</TabsTrigger>
              <TabsTrigger value="compression">压缩设置</TabsTrigger>
              <TabsTrigger value="metrics">指标设置</TabsTrigger>
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
                      <div className="grid gap-4">
                        <div className="grid gap-2">
                          <Label htmlFor="sizeThreshold">最小文件大小阈值</Label>
                          <div className="flex gap-2">
                            <Input
                              id="sizeThreshold"
                              type="number"
                              value={editingPathData?.sizeThreshold ?? newPathData.sizeThreshold}
                              onChange={(e) => {
                                if (editingPathData) {
                                  setEditingPathData({
                                    ...editingPathData,
                                    sizeThreshold: Number(e.target.value),
                                  })
                                } else {
                                  setNewPathData({
                                    ...newPathData,
                                    sizeThreshold: Number(e.target.value),
                                  })
                                }
                              }}
                            />
                            <select
                              className="w-24 rounded-md border border-input bg-background px-3"
                              value={editingPathData?.sizeThresholdUnit ?? newPathData.sizeThresholdUnit}
                              onChange={(e) => {
                                const unit = e.target.value as 'B' | 'KB' | 'MB' | 'GB'
                                if (editingPathData) {
                                  setEditingPathData({
                                    ...editingPathData,
                                    sizeThresholdUnit: unit,
                                  })
                                } else {
                                  setNewPathData({
                                    ...newPathData,
                                    sizeThresholdUnit: unit,
                                  })
                                }
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
                          <Label htmlFor="maxSize">最大文件大小阈值</Label>
                          <div className="flex gap-2">
                            <Input
                              id="maxSize"
                              type="number"
                              value={editingPathData?.maxSize ?? newPathData.maxSize}
                              onChange={(e) => {
                                if (editingPathData) {
                                  setEditingPathData({
                                    ...editingPathData,
                                    maxSize: Number(e.target.value),
                                  })
                                } else {
                                  setNewPathData({
                                    ...newPathData,
                                    maxSize: Number(e.target.value),
                                  })
                                }
                              }}
                            />
                            <select
                              className="w-24 rounded-md border border-input bg-background px-3"
                              value={editingPathData?.maxSizeUnit ?? newPathData.maxSizeUnit}
                              onChange={(e) => {
                                const unit = e.target.value as 'B' | 'KB' | 'MB' | 'GB'
                                if (editingPathData) {
                                  setEditingPathData({
                                    ...editingPathData,
                                    maxSizeUnit: unit,
                                  })
                                } else {
                                  setNewPathData({
                                    ...newPathData,
                                    maxSizeUnit: unit,
                                  })
                                }
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
                      <Button onClick={addOrUpdatePath}>
                        {editingPathData ? "保存" : "添加"}
                      </Button>
                    </div>
                  </DialogContent>
                </Dialog>
              </div>

              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[10%]">路径</TableHead>
                    <TableHead className="w-[40%]">默认目标</TableHead>
                    <TableHead className="w-[10%]">最小阈值</TableHead>
                    <TableHead className="w-[10%]">最大阈值</TableHead>
                    <TableHead className="w-[15%]">扩展名映射</TableHead>
                    <TableHead className="w-[15%]">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {config && Object.entries(config.MAP).map(([path, target]) => (
                    <>
                      <TableRow key={`${path}-main`}>
                        <TableCell>{path}</TableCell>
                        <TableCell>
                          {typeof target === 'string' ? target : target.DefaultTarget}
                        </TableCell>
                        <TableCell>
                          {typeof target === 'object' && target.SizeThreshold ? (
                            <span title={`${target.SizeThreshold} 字节`}>
                              {formatBytes(target.SizeThreshold)}
                            </span>
                          ) : '-'}
                        </TableCell>
                        <TableCell>
                          {typeof target === 'object' && target.MaxSize ? (
                            <span title={`${target.MaxSize} 字节`}>
                              {formatBytes(target.MaxSize)}
                            </span>
                          ) : '-'}
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleExtensionMapEdit(path)}
                          >
                            <Plus className="w-3 h-3 mr-2" />
                            添加扩展名映射
                          </Button>
                        </TableCell>
                        <TableCell>
                          <div className="flex space-x-2">
                            <Button
                              variant="ghost"
                              size="icon"
                              onClick={() => handleEditPath(path, target)}
                            >
                              <Edit className="w-4 h-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              onClick={() => deletePath(path)}
                            >
                              <Trash2 className="w-4 h-4" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                      {typeof target === 'object' && target.ExtensionMap && Object.keys(target.ExtensionMap).length > 0 && (
                        <TableRow key={`${path}-extensions`}>
                          <TableCell colSpan={6} className="p-0 border-t-0">
                            <div className="bg-muted/30 px-2 py-1 mx-4">
                              <Table>
                                <TableHeader>
                                  <TableRow className="border-0">
                                    <TableHead className="w-[30%] h-8 text-xs">扩展名</TableHead>
                                    <TableHead className="w-[50%] h-8 text-xs">目标地址</TableHead>
                                    <TableHead className="w-[20%] h-8 text-xs">操作</TableHead>
                                  </TableRow>
                                </TableHeader>
                                <TableBody>
                                  {Object.entries(target.ExtensionMap).map(([ext, url]) => (
                                    <TableRow key={ext} className="border-0">
                                      <TableCell className="py-1 text-sm">{ext}</TableCell>
                                      <TableCell className="py-1 text-sm">
                                        <span title={url}>{truncateUrl(url)}</span>
                                      </TableCell>
                                      <TableCell className="py-1">
                                        <div className="flex space-x-1">
                                          <Button
                                            variant="ghost"
                                            size="icon"
                                            className="h-6 w-6"
                                            onClick={() => handleExtensionMapEdit(path, ext, url)}
                                          >
                                            <Edit className="h-3 w-3" />
                                          </Button>
                                          <Button
                                            variant="ghost"
                                            size="icon"
                                            className="h-6 w-6"
                                            onClick={() => deleteExtensionMap(path, ext)}
                                          >
                                            <Trash2 className="h-3 w-3" />
                                          </Button>
                                        </div>
                                      </TableCell>
                                    </TableRow>
                                  ))}
                                </TableBody>
                              </Table>
                            </div>
                          </TableCell>
                        </TableRow>
                      )}
                    </>
                  ))}
                </TableBody>
              </Table>
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

            <TabsContent value="metrics" className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle>指标设置</CardTitle>
                  <CardDescription>
                    配置指标收集和保存的相关设置
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-6">
                  <div className="space-y-4">
                    <div className="grid gap-4">
                      <div className="grid gap-2">
                        <Label htmlFor="metricsSaveInterval">指标保存间隔（分钟）</Label>
                        <div className="flex items-center gap-2">
                          <Input
                            id="metricsSaveInterval"
                            type="number"
                            min="1"
                            max="1440"
                            value={config?.MetricsSaveInterval || 15}
                            onChange={(e) => updateMetricsSettings('MetricsSaveInterval', parseInt(e.target.value) || 15)}
                            className="w-24"
                          />
                          <span className="text-sm text-muted-foreground">
                            分钟（默认：15分钟）
                          </span>
                        </div>
                        <p className="text-sm text-muted-foreground">
                          系统将按此间隔定期保存统计数据到文件
                        </p>
                      </div>

                      <div className="grid gap-2">
                        <Label htmlFor="metricsMaxFiles">保留的最大统计文件数量</Label>
                        <div className="flex items-center gap-2">
                          <Input
                            id="metricsMaxFiles"
                            type="number"
                            min="1"
                            max="100"
                            value={config?.MetricsMaxFiles || 10}
                            onChange={(e) => updateMetricsSettings('MetricsMaxFiles', parseInt(e.target.value) || 10)}
                            className="w-24"
                          />
                          <span className="text-sm text-muted-foreground">
                            个文件（默认：10个）
                          </span>
                        </div>
                        <p className="text-sm text-muted-foreground">
                          超过此数量的旧统计文件将被自动清理
                        </p>
                      </div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>

      <Dialog open={extensionMapDialogOpen} onOpenChange={handleExtensionMapDialogOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editingExtension ? "编辑扩展名映射" : "添加扩展名映射"}
            </DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>扩展名</Label>
              <Input
                value={newExtension.ext}
                onChange={(e) => setNewExtension({ ...newExtension, ext: e.target.value })}
                placeholder="jpg,png,webp"
              />
              <p className="text-sm text-muted-foreground">
                多个扩展名用逗号分隔，不需要包含点号
              </p>
            </div>
            <div className="space-y-2">
              <Label>目标 URL</Label>
              <Input
                value={newExtension.target}
                onChange={(e) => setNewExtension({ ...newExtension, target: e.target.value })}
                placeholder="https://example.com"
              />
              <p className="text-sm text-muted-foreground">
                当文件大小超过阈值且扩展名匹配时，将使用此地址
              </p>
            </div>
            <Button onClick={addOrUpdateExtensionMap}>
              {editingExtension ? "保存" : "添加"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

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
      
      <AlertDialog 
        open={!!deletingExtension} 
        onOpenChange={(open) => handleDeleteDialogOpenChange(open, setDeletingExtension)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除扩展名 &ldquo;{deletingExtension?.ext}&rdquo; 的映射吗？此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDeleteExtensionMap}>删除</AlertDialogAction>
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