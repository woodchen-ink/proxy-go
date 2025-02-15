"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"

export default function ConfigPage() {
  const [config, setConfig] = useState("")
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const { toast } = useToast()
  const router = useRouter()

  useEffect(() => {
    fetchConfig()
  }, [])

  const fetchConfig = async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/api/config/get", {
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
      setConfig(JSON.stringify(data, null, 2))
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
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      // 验证 JSON 格式
      const parsedConfig = JSON.parse(config)

      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/api/config/save", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          'Authorization': `Bearer ${token}`
        },
        body: JSON.stringify(parsedConfig),
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
        description: error instanceof SyntaxError ? "JSON 格式错误" : error instanceof Error ? error.message : "保存配置失败",
        variant: "destructive",
      })
    } finally {
      setSaving(false)
    }
  }

  const handleFormat = () => {
    try {
      const parsedConfig = JSON.parse(config)
      setConfig(JSON.stringify(parsedConfig, null, 2))
      toast({
        title: "成功",
        description: "配置已格式化",
      })
    } catch {
      toast({
        title: "错误",
        description: "JSON 格式错误",
        variant: "destructive",
      })
    }
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
        <CardHeader>
          <CardTitle>代理服务配置</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="flex gap-2">
              <Button onClick={handleSave} disabled={saving}>
                {saving ? "保存中..." : "保存配置"}
              </Button>
              <Button variant="outline" onClick={handleFormat}>
                格式化
              </Button>
              <Button variant="outline" onClick={fetchConfig}>
                刷新
              </Button>
            </div>
            <div className="relative">
              <textarea
                className="w-full h-[600px] p-4 font-mono text-sm rounded-md border bg-background"
                value={config}
                onChange={(e) => setConfig(e.target.value)}
                spellCheck={false}
                placeholder="加载配置失败"
              />
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
} 