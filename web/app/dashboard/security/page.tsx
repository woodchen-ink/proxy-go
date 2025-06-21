"use client"

import React, { useEffect, useState, useCallback } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
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
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Shield, Ban, Clock, Trash2, RefreshCw } from "lucide-react"

interface BannedIP {
  ip: string
  ban_end_time: string
  remaining_seconds: number
}

interface SecurityStats {
  banned_ips_count: number
  error_records_count: number
  config: {
    ErrorThreshold: number
    WindowMinutes: number
    BanDurationMinutes: number
    CleanupIntervalMinutes: number
  }
}

interface IPStatus {
  ip: string
  banned: boolean
  ban_end_time?: string
  remaining_seconds?: number
}

export default function SecurityPage() {
  const [bannedIPs, setBannedIPs] = useState<BannedIP[]>([])
  const [stats, setStats] = useState<SecurityStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [checkingIP, setCheckingIP] = useState("")
  const [ipStatus, setIPStatus] = useState<IPStatus | null>(null)
  const [unbanning, setUnbanning] = useState<string | null>(null)
  const { toast } = useToast()
  const router = useRouter()

  const fetchData = useCallback(async () => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const [bannedResponse, statsResponse] = await Promise.all([
        fetch("/admin/api/security/banned-ips", {
          headers: { 'Authorization': `Bearer ${token}` }
        }),
        fetch("/admin/api/security/stats", {
          headers: { 'Authorization': `Bearer ${token}` }
        })
      ])

      if (bannedResponse.status === 401 || statsResponse.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (bannedResponse.ok) {
        const bannedData = await bannedResponse.json()
        setBannedIPs(bannedData.banned_ips || [])
      }

      if (statsResponse.ok) {
        const statsData = await statsResponse.json()
        setStats(statsData)
      }
    } catch (error) {
      console.error("获取安全数据失败:", error)
      toast({
        title: "错误",
        description: "获取安全数据失败",
        variant: "destructive",
      })
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [router, toast])

  useEffect(() => {
    fetchData()
    // 每30秒自动刷新一次数据
    const interval = setInterval(fetchData, 30000)
    return () => clearInterval(interval)
  }, [fetchData])

  const handleRefresh = () => {
    setRefreshing(true)
    fetchData()
  }

  const checkIPStatus = async () => {
    if (!checkingIP.trim()) return

    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch(`/admin/api/security/check-ip?ip=${encodeURIComponent(checkingIP)}`, {
        headers: { 'Authorization': `Bearer ${token}` }
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (response.ok) {
        const data = await response.json()
        setIPStatus(data)
             } else {
         throw new Error("检查IP状态失败")
       }
     } catch {
       toast({
         title: "错误",
         description: "检查IP状态失败",
         variant: "destructive",
       })
     }
  }

  const unbanIP = async (ip: string) => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }

      const response = await fetch("/admin/api/security/unban", {
        method: "POST",
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ ip })
      })

      if (response.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }

      if (response.ok) {
        const data = await response.json()
        if (data.success) {
          toast({
            title: "成功",
            description: `IP ${ip} 已解封`,
          })
          fetchData() // 刷新数据
        } else {
          toast({
            title: "提示",
            description: data.message,
          })
        }
             } else {
         throw new Error("解封IP失败")
       }
     } catch {
       toast({
         title: "错误",
         description: "解封IP失败",
         variant: "destructive",
       })
     } finally {
      setUnbanning(null)
    }
  }

  const formatTime = (seconds: number) => {
    if (seconds <= 0) return "已过期"
    const minutes = Math.floor(seconds / 60)
    const remainingSeconds = seconds % 60
    if (minutes > 0) {
      return `${minutes}分${remainingSeconds}秒`
    }
    return `${remainingSeconds}秒`
  }

  if (loading) {
    return (
      <div className="flex h-[calc(100vh-4rem)] items-center justify-center">
        <div className="text-center">
          <div className="text-lg font-medium">加载中...</div>
          <div className="text-sm text-gray-500 mt-1">正在获取安全数据</div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2">
            <Shield className="w-5 h-5" />
            安全管理
          </CardTitle>
          <Button onClick={handleRefresh} disabled={refreshing} variant="outline">
            <RefreshCw className={`w-4 h-4 mr-2 ${refreshing ? 'animate-spin' : ''}`} />
            刷新
          </Button>
        </CardHeader>
        <CardContent>
          {stats && (
            <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
              <div className="bg-red-50 p-4 rounded-lg">
                <div className="flex items-center gap-2">
                  <Ban className="w-5 h-5 text-red-600" />
                  <div>
                    <div className="text-2xl font-bold text-red-600">{stats.banned_ips_count}</div>
                    <div className="text-sm text-red-600">被封禁IP</div>
                  </div>
                </div>
              </div>
              <div className="bg-yellow-50 p-4 rounded-lg">
                <div className="flex items-center gap-2">
                  <Clock className="w-5 h-5 text-yellow-600" />
                  <div>
                    <div className="text-2xl font-bold text-yellow-600">{stats.error_records_count}</div>
                    <div className="text-sm text-yellow-600">错误记录</div>
                  </div>
                </div>
              </div>
              <div className="bg-blue-50 p-4 rounded-lg">
                <div className="text-sm text-blue-600 mb-1">错误阈值</div>
                <div className="text-lg font-bold text-blue-600">
                  {stats.config.ErrorThreshold}次/{stats.config.WindowMinutes}分钟
                </div>
              </div>
              <div className="bg-green-50 p-4 rounded-lg">
                <div className="text-sm text-green-600 mb-1">封禁时长</div>
                <div className="text-lg font-bold text-green-600">
                  {stats.config.BanDurationMinutes}分钟
                </div>
              </div>
            </div>
          )}

          <div className="space-y-4">
            <div className="flex gap-4">
              <div className="flex-1">
                <Label>检查IP状态</Label>
                <div className="flex gap-2 mt-1">
                  <Input
                    placeholder="输入IP地址"
                    value={checkingIP}
                    onChange={(e) => setCheckingIP(e.target.value)}
                  />
                  <Button onClick={checkIPStatus}>检查</Button>
                </div>
              </div>
            </div>

            {ipStatus && (
              <Card>
                <CardContent className="pt-4">
                  <div className="flex items-center gap-4">
                    <div>
                      <strong>IP: {ipStatus.ip}</strong>
                    </div>
                    <div className={`px-2 py-1 rounded text-sm ${
                      ipStatus.banned 
                        ? 'bg-red-100 text-red-800' 
                        : 'bg-green-100 text-green-800'
                    }`}>
                      {ipStatus.banned ? '已封禁' : '正常'}
                    </div>
                                         {ipStatus.banned && ipStatus.remaining_seconds && ipStatus.remaining_seconds > 0 && (
                       <div className="text-sm text-muted-foreground">
                         剩余时间: {formatTime(ipStatus.remaining_seconds)}
                       </div>
                     )}
                  </div>
                </CardContent>
              </Card>
            )}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>被封禁的IP列表</CardTitle>
        </CardHeader>
        <CardContent>
          {bannedIPs.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              当前没有被封禁的IP
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>IP地址</TableHead>
                  <TableHead>封禁结束时间</TableHead>
                  <TableHead>剩余时间</TableHead>
                  <TableHead>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {bannedIPs.map((bannedIP) => (
                  <TableRow key={bannedIP.ip}>
                    <TableCell className="font-mono">{bannedIP.ip}</TableCell>
                    <TableCell>{bannedIP.ban_end_time}</TableCell>
                    <TableCell>
                      <span className={bannedIP.remaining_seconds <= 0 ? 'text-muted-foreground' : 'text-orange-600'}>
                        {formatTime(bannedIP.remaining_seconds)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setUnbanning(bannedIP.ip)}
                        disabled={bannedIP.remaining_seconds <= 0}
                      >
                        <Trash2 className="w-4 h-4 mr-1" />
                        解封
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <AlertDialog open={!!unbanning} onOpenChange={(open) => !open && setUnbanning(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认解封</AlertDialogTitle>
            <AlertDialogDescription>
              确定要解封IP地址 &ldquo;{unbanning}&rdquo; 吗？
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => unbanning && unbanIP(unbanning)}>
              确认解封
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
} 