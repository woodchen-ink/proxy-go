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

  // navLinkClass 激活态走"加深 + 加粗", 不用品牌色着色
  const navLinkClass = (active: boolean) =>
    active
      ? "text-foreground font-medium"
      : "text-muted-foreground hover:text-foreground transition-colors"

  return (
    <nav className="border-b bg-card">
      <div className="container mx-auto flex h-14 items-center px-4">
        <div className="mr-6 font-semibold tracking-tight">Proxy Go 管理后台</div>
        <div className="flex flex-1 items-center space-x-4 md:space-x-6 text-sm">
          <Link href="/dashboard" className={navLinkClass(pathname === "/dashboard")}>
            仪表盘
          </Link>
          <Link href="/dashboard/config" className={navLinkClass(pathname === "/dashboard/config")}>
            配置
          </Link>
          <Link href="/dashboard/security" className={navLinkClass(pathname === "/dashboard/security")}>
            安全
          </Link>
        </div>
        <Button variant="ghost" size="sm" onClick={handleLogout}>
          退出登录
        </Button>
      </div>
    </nav>
  )
} 