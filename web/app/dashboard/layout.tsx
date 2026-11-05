"use client"

import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { AppSidebar } from "@/components/app-sidebar"
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { checkAuth } from "@/lib/api-client"

// SIDEBAR_OPEN_KEY 侧栏展开偏好, 纯 UI 状态走 localStorage (登录态仍在 HttpOnly cookie)
const SIDEBAR_OPEN_KEY = "proxy-go:admin:sidebar-open"

// DashboardLayout 仪表盘外壳: 挂载时校验会话, 无效跳登录;
// 侧栏 + 主区分区独立滚动, 外壳锁视口高度不出现 body 滚动
export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const [sidebarOpen, setSidebarOpen] = useState(true)

  useEffect(() => {
    checkAuth().then((authenticated) => {
      if (!authenticated) {
        router.push("/login")
      }
    })
  }, [router])

  // 挂载后再读偏好, 首屏用稳定默认值避免水合不一致; 只认 "true" / "false", 其它值回退默认
  useEffect(() => {
    try {
      const saved = localStorage.getItem(SIDEBAR_OPEN_KEY)
      if (saved === "true" || saved === "false") setSidebarOpen(saved === "true")
    } catch {
      // 隐私模式等场景读不到, 保持默认展开
    }
  }, [])

  const handleSidebarOpenChange = useCallback((open: boolean) => {
    setSidebarOpen(open)
    try {
      localStorage.setItem(SIDEBAR_OPEN_KEY, String(open))
    } catch {
      // 写不进去只影响下次刷新的默认值
    }
  }, [])

  return (
    <SidebarProvider
      open={sidebarOpen}
      onOpenChange={handleSidebarOpenChange}
      className="h-svh overflow-hidden"
    >
      <AppSidebar />
      <SidebarInset className="min-h-0 min-w-0 overflow-y-auto overscroll-contain">
        <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b bg-background px-3 md:hidden">
          <SidebarTrigger aria-label="打开导航" />
          <span className="text-sm font-semibold tracking-tight">Proxy Go 中控</span>
        </header>
        <div className="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:px-8">
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}
