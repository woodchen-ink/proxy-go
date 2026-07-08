"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { Nav } from "@/components/nav"
import { checkAuth } from "@/lib/api-client"

// DashboardLayout 仪表盘外壳: 挂载时校验会话, 无效跳登录;
// 鉴权凭据在 HttpOnly cookie 中由浏览器自动携带, 不再拦截 window.fetch 注头
export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()

  useEffect(() => {
    checkAuth().then((authenticated) => {
      if (!authenticated) {
        router.push("/login")
      }
    })
  }, [router])

  return (
    <div className="min-h-screen bg-background">
      <Nav />
      <main className="max-w-7xl mx-auto px-4 py-6 sm:px-6 lg:px-8">
        {children}
      </main>
    </div>
  )
}
