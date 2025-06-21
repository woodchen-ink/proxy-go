"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { useToast } from "@/components/ui/use-toast"

export function Nav() {
  const pathname = usePathname()
  const router = useRouter()
  const { toast } = useToast()

  const handleLogout = async () => {
    try {
      const response = await fetch("/admin/api/logout", {
        method: "POST",
      })

      if (response.ok) {
        localStorage.removeItem("token")
        toast({
          title: "已退出登录",
        })
        router.push("/")
      }
    } catch {
      toast({
        title: "退出失败",
        description: "请稍后重试",
        variant: "destructive",
      })
    }
  }

  return (
    <nav className="border-b bg-white">
      <div className="container mx-auto flex h-14 items-center px-4">
        <div className="mr-4 font-bold">Proxy Go管理后台</div>
        <div className="flex flex-1 items-center space-x-4 md:space-x-6">
          <Link
            href="/dashboard"
            className={pathname === "/dashboard" ? "text-primary" : "text-muted-foreground"}
          >
            仪表盘
          </Link>
          <Link
            href="/dashboard/config"
            className={pathname === "/dashboard/config" ? "text-primary" : "text-muted-foreground"}
          >
            配置
          </Link>
          <Link
            href="/dashboard/cache"
            className={pathname === "/dashboard/cache" ? "text-primary" : "text-muted-foreground"}
          >
            缓存
          </Link>
          <Link
            href="/dashboard/security"
            className={pathname === "/dashboard/security" ? "text-primary" : "text-muted-foreground"}
          >
            安全
          </Link>
        </div>
        <Button variant="ghost" onClick={handleLogout}>
          退出登录
        </Button>
      </div>
    </nav>
  )
} 