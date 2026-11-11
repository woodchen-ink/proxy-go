import type { RefererBanConfig } from "./components/RefererBanEditor"
import type { RefererRedirectConfig } from "./components/RefererRedirectEditor"

export type { RefererBanConfig, RefererRedirectConfig }

export interface ExtRuleConfig {
  Extensions: string
  Target: string
  SizeThreshold: number
  MaxSize: number
  RedirectMode?: boolean
  Domains?: string
}

export interface CacheConfig {
  max_age: number
  cleanup_tick: number
  max_cache_size: number
}

export interface PathMapping {
  DefaultTarget: string
  DefaultTargets?: string[] // 有序回源列表 (主源在前); 多源时失败自动按序回落
  ExtensionMap?: ExtRuleConfig[]
  SizeThreshold?: number
  MaxSize?: number
  RedirectMode?: boolean
  Enabled?: boolean
  CacheConfig?: CacheConfig
  CFImageOpt?: boolean
  RefererBan?: RefererBanConfig
  RefererRedirect?: RefererRedirectConfig
}

export interface CompressionConfig {
  Enabled: boolean
  Level: number
}

export interface SecurityConfig {
  IPBan: {
    Enabled: boolean
    ErrorThreshold: number
    WindowMinutes: number
    BanDurationMinutes: number
    CleanupIntervalMinutes: number
  }
  RefererBan?: RefererBanConfig
}

export interface Config {
  MAP: Record<string, PathMapping | string>
  Compression: {
    Gzip: CompressionConfig
    Brotli: CompressionConfig
  }
  Security: SecurityConfig
}

// toMapping 旧格式的字符串映射等价于只有回源地址、默认启用的对象
export function toMapping(m: PathMapping | string): PathMapping {
  return typeof m === "string" ? { DefaultTarget: m, Enabled: true } : m
}
