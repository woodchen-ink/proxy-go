"use client"

import { useEffect, useRef } from "react"

// usePolling 按间隔重复执行 fn: 上一次结束后才排下一次 (不叠请求), 页面隐藏时暂停, 回到前台立即补一次;
// fn 返回 false 视为失败, 按指数退避 (最长 60s)
export function usePolling(fn: () => Promise<boolean | void>, intervalMs: number, deps: unknown[] = []) {
  const fnRef = useRef(fn)
  fnRef.current = fn

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null
    let failures = 0

    const schedule = (ms: number) => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(tick, ms)
    }
    const tick = async () => {
      if (cancelled) return
      if (document.visibilityState === "hidden") return // 回到前台时由 visibilitychange 续上
      const ok = (await fnRef.current()) !== false
      if (cancelled) return
      failures = ok ? 0 : Math.min(failures + 1, 5)
      schedule(ok ? intervalMs : Math.min(intervalMs * 2 ** failures, 60_000))
    }
    const onVisible = () => {
      if (document.visibilityState === "visible") schedule(0)
    }

    tick()
    document.addEventListener("visibilitychange", onVisible)
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
      document.removeEventListener("visibilitychange", onVisible)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intervalMs, ...deps])
}
