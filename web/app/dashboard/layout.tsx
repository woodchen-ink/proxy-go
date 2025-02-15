"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { Nav } from "@/components/nav"

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()

  useEffect(() => {
    const token = localStorage.getItem("token")
    if (!token) {
      router.push("/login")
      return
    }

    // 设置全局请求拦截器
    const originalFetch = window.fetch
    window.fetch = async (...args) => {
      const [resource, config = {}] = args
      
      const newConfig = {
        ...config,
        headers: {
          ...(config.headers || {}),
          'Authorization': `Bearer ${token}`,
        },
      }
      
      try {
        const response = await originalFetch(resource, newConfig)
        if (response.status === 401) {
          localStorage.removeItem("token")
          router.push("/login")
          return response
        }
        return response
      } catch (error) {
        console.error('请求失败:', error)
        return Promise.reject(error)
      }
    }

    // 验证 token 有效性
    fetch("/admin/api/check-auth").catch(() => {
      localStorage.removeItem("token")
      router.push("/login")
    })

    return () => {
      window.fetch = originalFetch
    }
  }, [router])

  return (
    <div className="min-h-screen bg-gray-100">
      <Nav />
      <main className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
        {children}
      </main>
    </div>
  )
} 