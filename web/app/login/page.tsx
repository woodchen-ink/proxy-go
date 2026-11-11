"use client"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { BrandIcon } from "@/components/brand-icon"

export default function LoginPage() {
  const handleLogin = () => {
    window.location.href = "/admin/api/auth"
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <BrandIcon className="mb-2 size-12" />
          <CardTitle className="text-2xl tracking-tight">Proxy Go 中控</CardTitle>
          <p className="mt-1 text-sm text-foreground-muted">使用 CZL Connect 单点登录</p>
        </CardHeader>
        <CardContent>
          <Button onClick={handleLogin} className="w-full">
            使用 CZL Connect 登录
          </Button>
        </CardContent>
      </Card>
    </div>
  )
} 