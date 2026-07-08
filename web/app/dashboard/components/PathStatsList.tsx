"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { RotateCcw } from "lucide-react"
import { useToast } from "@/components/ui/use-toast"
import { useRouter } from "next/navigation"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import PathStatsCard, { type PathStats } from "./PathStatsCard"

// PathStatsList 首页逐路径统计: 从 /admin/api/path-stats 拉取按前缀聚合的累计统计,
// 每个路径一张卡展示缓存命中率 / 错误率 / 状态码分布等 (原本在配置页路径详情里, 已迁到首页)。
// 支持单路径重置与一键重置全部, 复用后端 /admin/api/path-stats/reset[-all]。
export default function PathStatsList() {
  const [stats, setStats] = useState<PathStats[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const failureCount = useRef(0)
  const lastErrorAt = useRef(0)
  const { toast } = useToast()
  const router = useRouter()

  const fetchStats = useCallback(async (): Promise<boolean> => {
    try {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return false
      }
      const resp = await fetch("/admin/api/path-stats", {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (resp.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return false
      }
      if (!resp.ok) throw new Error("加载路径统计失败")
      const json = (await resp.json()) as { path_stats: PathStats[] }
      setStats(json.path_stats || [])
      setError(null)
      return true
    } catch (e) {
      const msg = e instanceof Error ? e.message : "加载路径统计失败"
      setError(msg)
      const now = Date.now()
      if (now - lastErrorAt.current > 30_000) {
        lastErrorAt.current = now
        toast({ title: "错误", description: msg, variant: "destructive" })
      }
      return false
    } finally {
      setLoading(false)
    }
  }, [router, toast])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    const tick = async () => {
      if (cancelled) return
      const ok = await fetchStats()
      if (cancelled) return
      if (ok) failureCount.current = 0
      else failureCount.current = Math.min(failureCount.current + 1, 5)
      const delay = ok ? 30_000 : Math.min(1000 * 2 ** failureCount.current, 60_000)
      timer = setTimeout(tick, delay)
    }
    tick()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [fetchStats])

  // resetPath 重置单个路径统计, 成功后立即刷新列表 (throw 让 PathStatsCard 捕获并 toast 失败)
  const resetPath = useCallback(
    async (path: string) => {
      const token = localStorage.getItem("token")
      if (!token) {
        router.push("/login")
        return
      }
      const resp = await fetch("/admin/api/path-stats/reset", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ path }),
      })
      if (resp.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        throw new Error("未授权")
      }
      if (!resp.ok) throw new Error("重置统计失败")
      await fetchStats()
    },
    [router, fetchStats]
  )

  // resetAll 一键重置全部路径统计
  const resetAll = useCallback(async () => {
    const token = localStorage.getItem("token")
    if (!token) {
      router.push("/login")
      return
    }
    try {
      const resp = await fetch("/admin/api/path-stats/reset-all", {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      })
      if (resp.status === 401) {
        localStorage.removeItem("token")
        router.push("/login")
        return
      }
      if (!resp.ok) throw new Error("重置失败")
      toast({ title: "成功", description: "所有路径统计已重置" })
      await fetchStats()
    } catch (e) {
      toast({
        title: "错误",
        description: e instanceof Error ? e.message : "重置失败",
        variant: "destructive",
      })
    }
  }, [router, toast, fetchStats])

  // 只展示有请求量的路径, 按请求数降序
  const visible = [...stats]
    .filter((s) => s.request_count > 0)
    .sort((a, b) => b.request_count - a.request_count)

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>
          路径统计
          <span className="ml-2 text-sm font-normal text-muted-foreground">
            (累计, {visible.length} 条)
          </span>
        </CardTitle>
        {visible.length > 0 && (
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="ghost" size="sm" className="h-7 px-2 text-muted-foreground">
                <RotateCcw className="h-3.5 w-3.5" />
                <span className="ml-1.5 text-xs">重置全部</span>
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>重置所有路径统计?</AlertDialogTitle>
                <AlertDialogDescription>
                  将清零所有路径的累计请求、错误、状态码与缓存统计, 此操作无法撤销。
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>取消</AlertDialogCancel>
                <AlertDialogAction onClick={resetAll}>确认重置</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </CardHeader>
      <CardContent>
        {loading && stats.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-muted-foreground">
            加载中...
          </div>
        ) : error && stats.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-destructive">
            {error}
          </div>
        ) : visible.length === 0 ? (
          <div className="flex h-32 items-center justify-center text-muted-foreground">
            暂无统计数据
          </div>
        ) : (
          <div className="space-y-4">
            {visible.map((s) => (
              <div key={s.path} className="rounded-md border p-4">
                <div className="mb-3 font-mono text-sm font-semibold">{s.path}</div>
                <PathStatsCard stats={s} onReset={() => resetPath(s.path)} />
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}
