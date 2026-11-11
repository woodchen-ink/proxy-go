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

// formatBytes 字节数 → 二进制单位 (1024 进位)
export function formatBytes(bytes: number): string {
  if (!bytes || !Number.isFinite(bytes) || bytes < 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB", "PB"]
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
}

// formatPct 比例 (0~1) → 百分比; 0 显示为 "0%"
export function formatPct(ratio: number, digits = 1): string {
  if (!ratio || !Number.isFinite(ratio)) return "0%"
  return `${(ratio * 100).toFixed(digits)}%`
}

// formatCount 大数缩写: 1.2万 / 3.4亿
export function formatCount(n: number): string {
  if (!n || !Number.isFinite(n)) return "0"
  if (n >= 1e8) return `${(n / 1e8).toFixed(2)}亿`
  if (n >= 1e4) return `${(n / 1e4).toFixed(n >= 1e6 ? 0 : 1)}万`
  return String(Math.round(n))
}

// formatMs 毫秒数 → 自适应单位
export function formatMs(ms: number): string {
  if (!ms || !Number.isFinite(ms)) return "-"
  if (ms < 1) return `${(ms * 1000).toFixed(0)} µs`
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 1 : 0)} ms`
  return `${(ms / 1000).toFixed(2)} s`
}
