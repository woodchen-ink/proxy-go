"use client"

import { useEffect } from "react"
import { useRouter } from "next/navigation"
import { checkAuth } from "@/lib/api-client"

// HomePage 登录态分流: 会话有效进仪表盘, 否则去登录页
export default function HomePage() {
  const router = useRouter()

  useEffect(() => {
    checkAuth().then((authenticated) => {
      router.push(authenticated ? "/dashboard" : "/login")
    })
  }, [router])

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="text-center">
        正在加载...
      </div>
    </div>
  )
}
