// 节点 / DNS 页共用的展示格式化

export function formatTime(ms: number): string {
  if (!ms) return "-"
  return new Date(ms).toLocaleString("zh-CN", { hour12: false })
}

export function formatAge(ms: number): string {
  if (!ms) return ""
  const sec = Math.max(0, Math.floor((Date.now() - ms) / 1000))
  if (sec < 60) return `${sec} 秒`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时`
  return `${Math.floor(sec / 86400)} 天`
}

export function formatBps(bps: number): string {
  const units = ["B/s", "KB/s", "MB/s", "GB/s"]
  let v = bps
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}
