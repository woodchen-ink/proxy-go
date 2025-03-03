"use client"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

export default function LoginPage() {
  const handleLogin = () => {
    window.location.href = "/admin/api/auth"
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100">
      <Card className="w-[400px]">
        <CardHeader>
          <CardTitle className="text-2xl text-center">管理员登录</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <Button onClick={handleLogin} className="w-full">
              使用 CZL Connect 登录
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
} 