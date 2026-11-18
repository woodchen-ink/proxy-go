export interface PathHit {
  p: string
  n: number
}

export interface DayPoint {
  date: string
  requests: number
  bytes: number
  errors: number
  blocked: number
}

export interface ActiveHost {
  host: string
  daily: DayPoint[]
  requests: number
  bytes: number
  errors: number
  blocked: number
  redirected: number
  distinct_paths: number
  top_paths: PathHit[]
  all_requests: number
  all_bytes: number
  first_seen: string
  last_seen: string
  is_new: boolean
  is_own: boolean
  is_banned: boolean
}

export interface HistoryHost {
  host: string
  requests: number
  bytes: number
  blocked: number
  first_seen: string
  last_seen: string
  peak_date: string
  top_paths: PathHit[]
  is_own: boolean
  is_banned: boolean
}

export interface RefererSettings {
  own_hosts: string[]
  auto_own_hosts: string[]
  alert_threshold: number
}

export interface RefererView {
  generated_at: number
  days: number
  new_days: number
  hosts: ActiveHost[]
  total_hosts: number
  history: HistoryHost[]
  history_truncated: boolean
  settings: RefererSettings
}

export type HostAction = { kind: "ban"; host: string; banned: boolean } | { kind: "own"; host: string; own: boolean }

// 节点侧每天最多跟踪 200 个不同路径, 达到上限说明在整站引用
export const PATH_CAP = 200
