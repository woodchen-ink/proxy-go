"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import {
  Gauge,
  Globe,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  Server,
  Settings,
  ShieldCheck,
  Waypoints,
  type LucideIcon,
} from "lucide-react"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar"
import { useToast } from "@/components/ui/use-toast"
import { logout } from "@/lib/api-client"

interface NavItem {
  title: string
  href: string
  icon: LucideIcon
}

const NAV_GROUPS: { label: string; items: NavItem[] }[] = [
  {
    label: "监控",
    items: [
      { title: "仪表盘", href: "/dashboard", icon: Gauge },
      { title: "节点", href: "/dashboard/nodes", icon: Server },
    ],
  },
  {
    label: "管理",
    items: [
      { title: "配置", href: "/dashboard/config", icon: Settings },
      { title: "DNS", href: "/dashboard/dns", icon: Globe },
      { title: "安全", href: "/dashboard/security", icon: ShieldCheck },
    ],
  },
]

// normalizePath 去掉 trailingSlash 导出带来的尾斜杠, 便于与路由表精确比对
function normalizePath(path: string) {
  return path.length > 1 ? path.replace(/\/+$/, "") : path
}

// AppSidebar 后台侧边栏: 分组导航 + 底部折叠 / 退出; 激活态走 shadcn 默认的浅灰底 + 加深加粗
export function AppSidebar() {
  const pathname = normalizePath(usePathname() ?? "")
  const router = useRouter()
  const { toast } = useToast()
  const { state, isMobile, toggleSidebar, setOpenMobile } = useSidebar()
  const collapsed = state === "collapsed"

  // handleLogout 吊销服务端会话 (清 HttpOnly cookie) 后回到登录分流页
  const handleLogout = async () => {
    const ok = await logout()
    if (ok) {
      toast({ title: "已退出登录" })
      router.push("/")
    } else {
      toast({ title: "退出失败", description: "请稍后重试", variant: "destructive" })
    }
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg" tooltip="Proxy Go 中控">
              <Link href="/dashboard" onClick={() => setOpenMobile(false)}>
                <div className="flex aspect-square size-8 items-center justify-center rounded-md bg-sidebar-primary text-sidebar-primary-foreground">
                  <Waypoints className="size-4" />
                </div>
                <span className="truncate font-semibold tracking-tight">Proxy Go 中控</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent className="overscroll-contain">
        {NAV_GROUPS.map((group) => (
          <SidebarGroup key={group.label}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      asChild
                      isActive={pathname === item.href}
                      tooltip={item.title}
                      // 当前项走品牌浅底, 与 hover 的半透明灰区分
                      className="data-[active=true]:bg-brand-soft data-[active=true]:text-brand-strong data-[active=true]:hover:bg-brand-soft data-[active=true]:hover:text-brand-strong"
                    >
                      <Link href={item.href} onClick={() => setOpenMobile(false)}>
                        <item.icon />
                        <span>{item.title}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          {!isMobile && (
            <SidebarMenuItem>
              <SidebarMenuButton onClick={toggleSidebar} tooltip={collapsed ? "展开侧栏" : "收起侧栏"}>
                {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
                <span>收起侧栏</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
          <SidebarMenuItem>
            <SidebarMenuButton onClick={handleLogout} tooltip="退出登录">
              <LogOut />
              <span>退出登录</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
