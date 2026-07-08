// 管理端统一 API 客户端: 鉴权走 HttpOnly cookie (同源请求浏览器自动携带),
// 401 集中处理为跳登录页, 组件不再各自读 token / 拼 Authorization 头。

// UnauthorizedError 标记 401 场景, 调用方 catch 后可选择静默 (页面即将跳转)
export class UnauthorizedError extends Error {
  constructor() {
    super("未登录或会话已过期")
    this.name = "UnauthorizedError"
  }
}

// clearLegacyAuthStorage 清理旧版 localStorage 登录态残留 (已迁移到 HttpOnly cookie)
function clearLegacyAuthStorage() {
  localStorage.removeItem("token")
  localStorage.removeItem("user")
}

// redirectToLogin 会话失效时跳转登录页 (basePath /admin 由 Next 自动补)
function redirectToLogin() {
  clearLegacyAuthStorage()
  window.location.href = "/admin/login"
}

// fetchApi 发起同源管理 API 请求; 401 时清残留、跳登录并抛 UnauthorizedError,
// 其余状态码原样返回 Response 由调用方按业务处理
export async function fetchApi(path: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(path, init)
  if (response.status === 401) {
    redirectToLogin()
    throw new UnauthorizedError()
  }
  return response
}

// checkAuth 查询当前会话是否有效 (登录守卫用), 网络错误视为未登录
export async function checkAuth(): Promise<boolean> {
  try {
    const response = await fetch("/admin/api/check-auth")
    return response.ok
  } catch {
    return false
  }
}

// logout 吊销会话并清理旧存储; 由调用方负责后续跳转与提示
export async function logout(): Promise<boolean> {
  try {
    const response = await fetch("/admin/api/logout", { method: "POST" })
    clearLegacyAuthStorage()
    return response.ok
  } catch {
    return false
  }
}
