"use client"

import { useEffect, useState, useCallback } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
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

interface PathMapping {
  DefaultTarget: string
  ExtensionMap?: Record<string, string>
  SizeThreshold?: number
}

interface FixedPath {
  Path: string
  TargetHost: string
  TargetURL: string
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
  FixedPaths: FixedPath[]
}

export default function ConfigPage() {
  const [config, setConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { toast } = useToast()
  const router = useRouter()

  // 对话框状态
  const [pathDialogOpen, setPathDialogOpen] = useState(false)
  const [newPathData, setNewPathData] = useState({
    path: "",
    defaultTarget: "",
    extensionMap: {} as Record<string, string>,
    sizeThreshold: 0,
  })
  const [fixedPathDialogOpen, setFixedPathDialogOpen] = useState(false)
  const [editingFixedPath, setEditingFixedPath] = useState<FixedPath | null>(null)
  const [newFixedPath, setNewFixedPath] = useState<FixedPath>({
    Path: "",
    TargetHost: "",
    TargetURL: "",
  })
  const [extensionMapDialogOpen, setExtensionMapDialogOpen] = useState(false)
  const [editingPath, setEditingPath] = useState<string | null>(null)
  const [newExtension, setNewExtension] = useState({ ext: "", target: "" })

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

  const addPath = () => {
    if (!config) return
    const { path, defaultTarget, sizeThreshold, extensionMap } = newPathData
    
    if (!path || !defaultTarget) {
      toast({
        title: "错误",
        description: "路径和默认目标不能为空",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    newConfig.MAP[path] = {
      DefaultTarget: defaultTarget,
      ...(sizeThreshold ? { SizeThreshold: sizeThreshold } : {}),
      ...(Object.keys(extensionMap).length > 0 ? { ExtensionMap: extensionMap } : {})
    }

    setConfig(newConfig)
    setPathDialogOpen(false)
    setNewPathData({
      path: "",
      defaultTarget: "",
      extensionMap: {},
      sizeThreshold: 0,
    })
  }

  const deletePath = (path: string) => {
    if (!config) return
    const newConfig = { ...config }
    delete newConfig.MAP[path]
    setConfig(newConfig)
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

  const handleExtensionMapEdit = (path: string) => {
    setEditingPath(path)
    setExtensionMapDialogOpen(true)
  }

  const addExtensionMap = () => {
    if (!config || !editingPath) return
    const { ext, target } = newExtension
    if (!ext || !target) {
      toast({
        title: "错误",
        description: "扩展名和目标不能为空",
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
      mapping.ExtensionMap = {
        ...mapping.ExtensionMap,
        [ext]: target
      }
    }

    setConfig(newConfig)
    setNewExtension({ ext: "", target: "" })
  }

  const deleteExtensionMap = (path: string, ext: string) => {
    if (!config) return
    const newConfig = { ...config }
    const mapping = newConfig.MAP[path]
    if (typeof mapping !== "string" && mapping.ExtensionMap) {
      const newExtensionMap = { ...mapping.ExtensionMap }
      delete newExtensionMap[ext]
      mapping.ExtensionMap = newExtensionMap
    }
    setConfig(newConfig)
  }

  const addFixedPath = () => {
    if (!config) return
    const { Path, TargetHost, TargetURL } = newFixedPath
    
    if (!Path || !TargetHost || !TargetURL) {
      toast({
        title: "错误",
        description: "所有字段都不能为空",
        variant: "destructive",
      })
      return
    }

    const newConfig = { ...config }
    if (editingFixedPath) {
      const index = newConfig.FixedPaths.findIndex(p => p.Path === editingFixedPath.Path)
      if (index !== -1) {
        newConfig.FixedPaths[index] = newFixedPath
      }
    } else {
      newConfig.FixedPaths.push(newFixedPath)
    }

    setConfig(newConfig)
    setFixedPathDialogOpen(false)
    setEditingFixedPath(null)
    setNewFixedPath({
      Path: "",
      TargetHost: "",
      TargetURL: "",
    })
  }

  const editFixedPath = (path: FixedPath) => {
    setEditingFixedPath(path)
    setNewFixedPath(path)
    setFixedPathDialogOpen(true)
  }

  const deleteFixedPath = (path: FixedPath) => {
    if (!config) return
    const newConfig = { ...config }
    newConfig.FixedPaths = newConfig.FixedPaths.filter(p => p.Path !== path.Path)
    setConfig(newConfig)
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
        setConfig(newConfig)
        toast({
          title: "成功",
          description: "配置已导入",
        })
      } catch {
        toast({
          title: "错误",
          description: "配置文件格式错误",
          variant: "destructive",
        })
      }
    }
    reader.readAsText(file)
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
              <TabsTrigger value="fixed-paths">固定路径</TabsTrigger>
            </TabsList>

            <TabsContent value="paths" className="space-y-4">
              <div className="flex justify-end">
                <Dialog open={pathDialogOpen} onOpenChange={setPathDialogOpen}>
                  <DialogTrigger asChild>
                    <Button>
                      <Plus className="w-4 h-4 mr-2" />
                      添加路径
                    </Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>添加路径映射</DialogTitle>
                    </DialogHeader>
                    <div className="space-y-4">
                      <div className="space-y-2">
                        <Label>路径</Label>
                        <Input
                          value={newPathData.path}
                          onChange={(e) => setNewPathData({ ...newPathData, path: e.target.value })}
                          placeholder="/example"
                        />
                      </div>
                      <div className="space-y-2">
                        <Label>默认目标</Label>
                        <Input
                          value={newPathData.defaultTarget}
                          onChange={(e) => setNewPathData({ ...newPathData, defaultTarget: e.target.value })}
                          placeholder="https://example.com"
                        />
                      </div>
                      <div className="space-y-2">
                        <Label>大小阈值 (字节)</Label>
                        <Input
                          type="number"
                          value={newPathData.sizeThreshold}
                          onChange={(e) => setNewPathData({ ...newPathData, sizeThreshold: parseInt(e.target.value) })}
                        />
                      </div>
                      <Button onClick={addPath}>添加</Button>
                    </div>
                  </DialogContent>
                </Dialog>
              </div>

              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>路径</TableHead>
                    <TableHead>目标</TableHead>
                    <TableHead>大小阈值</TableHead>
                    <TableHead>扩展名映射</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {config && Object.entries(config.MAP).map(([path, target]) => (
                    <TableRow key={path}>
                      <TableCell>{path}</TableCell>
                      <TableCell>
                        {typeof target === 'string' ? target : target.DefaultTarget}
                      </TableCell>
                      <TableCell>
                        {typeof target === 'object' && target.SizeThreshold ? target.SizeThreshold : '-'}
                      </TableCell>
                      <TableCell>
                        {typeof target === 'object' && target.ExtensionMap ? (
                          <div className="space-y-1">
                            {Object.entries(target.ExtensionMap).map(([ext, url]) => (
                              <div key={ext} className="flex items-center space-x-2">
                                <span className="text-sm">{ext}: {url}</span>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-6 w-6"
                                  onClick={() => deleteExtensionMap(path, ext)}
                                >
                                  <Trash2 className="h-3 w-3" />
                                </Button>
                              </div>
                            ))}
                          </div>
                        ) : '-'}
                      </TableCell>
                      <TableCell>
                        <div className="flex space-x-2">
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => handleExtensionMapEdit(path)}
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
                  ))}
                </TableBody>
              </Table>

              <Dialog open={extensionMapDialogOpen} onOpenChange={setExtensionMapDialogOpen}>
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>编辑扩展名映射</DialogTitle>
                  </DialogHeader>
                  <div className="space-y-4">
                    <div className="space-y-2">
                      <Label>扩展名 (如: jpg,png)</Label>
                      <Input
                        value={newExtension.ext}
                        onChange={(e) => setNewExtension({ ...newExtension, ext: e.target.value })}
                        placeholder="jpg,png"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>目标 URL</Label>
                      <Input
                        value={newExtension.target}
                        onChange={(e) => setNewExtension({ ...newExtension, target: e.target.value })}
                        placeholder="https://example.com"
                      />
                    </div>
                    <Button onClick={addExtensionMap}>添加</Button>
                  </div>
                </DialogContent>
              </Dialog>
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

            <TabsContent value="fixed-paths">
              <div className="flex justify-end mb-4">
                <Button onClick={() => setFixedPathDialogOpen(true)}>
                  <Plus className="w-4 h-4 mr-2" />
                  添加固定路径
                </Button>
              </div>

              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>路径</TableHead>
                    <TableHead>目标主机</TableHead>
                    <TableHead>目标 URL</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {config?.FixedPaths.map((path, index) => (
                    <TableRow key={index}>
                      <TableCell>{path.Path}</TableCell>
                      <TableCell>{path.TargetHost}</TableCell>
                      <TableCell>{path.TargetURL}</TableCell>
                      <TableCell>
                        <div className="flex space-x-2">
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => editFixedPath(path)}
                          >
                            <Edit className="w-4 h-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => deleteFixedPath(path)}
                          >
                            <Trash2 className="w-4 h-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              <Dialog open={fixedPathDialogOpen} onOpenChange={setFixedPathDialogOpen}>
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>
                      {editingFixedPath ? "编辑固定路径" : "添加固定路径"}
                    </DialogTitle>
                  </DialogHeader>
                  <div className="space-y-4">
                    <div className="space-y-2">
                      <Label>路径</Label>
                      <Input
                        value={newFixedPath.Path}
                        onChange={(e) => setNewFixedPath({ ...newFixedPath, Path: e.target.value })}
                        placeholder="/example"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>目标主机</Label>
                      <Input
                        value={newFixedPath.TargetHost}
                        onChange={(e) => setNewFixedPath({ ...newFixedPath, TargetHost: e.target.value })}
                        placeholder="example.com"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>目标 URL</Label>
                      <Input
                        value={newFixedPath.TargetURL}
                        onChange={(e) => setNewFixedPath({ ...newFixedPath, TargetURL: e.target.value })}
                        placeholder="https://example.com"
                      />
                    </div>
                    <Button onClick={addFixedPath}>
                      {editingFixedPath ? "保存" : "添加"}
                    </Button>
                  </div>
                </DialogContent>
              </Dialog>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </div>
  )
} 