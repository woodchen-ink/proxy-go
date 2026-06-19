// 辅助函数：格式化字节大小
export const formatBytes = (bytes: number) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`
}

// 辅助函数：截断 URL
export const truncateUrl = (url: string) => {
  if (url.length > 30) {
    return url.substring(0, 27) + '...'
  }
  return url
}

// getMappingTargets 取路径的有序回源列表 (主源在前)。
// 优先用 DefaultTargets, 为空时退化为单源 DefaultTarget; 与后端 PathConfig.GetTargets() 对齐。
export const getMappingTargets = (
  mapping: { DefaultTarget?: string; DefaultTargets?: string[] }
): string[] => {
  const list = (mapping.DefaultTargets ?? [])
    .map((t) => t.trim())
    .filter((t) => t.length > 0)
  if (list.length > 0) return list
  const single = (mapping.DefaultTarget ?? '').trim()
  return single ? [single] : []
}

// targetsToText 把回源列表转成"一行一个"的文本, 供多行输入框编辑
export const targetsToText = (targets: string[]): string => targets.join('\n')

// parseTargetsText 把多行输入解析为去空行的回源数组 (保持顺序, 第一行即主源)
export const parseTargetsText = (text: string): string[] =>
  text
    .split('\n')
    .map((t) => t.trim())
    .filter((t) => t.length > 0)

// buildTargetFields 由回源数组生成写入配置的字段:
//   - 单源: 只写 DefaultTarget, DefaultTargets 留空 (保持配置精简, 与后端归一逻辑一致)
//   - 多源: DefaultTarget = 主源 (兼容旧逻辑 / mirror 判断), DefaultTargets = 完整有序列表
export const buildTargetFields = (
  targets: string[]
): { DefaultTarget: string; DefaultTargets?: string[] } => {
  if (targets.length <= 1) {
    return { DefaultTarget: targets[0] ?? '', DefaultTargets: undefined }
  }
  return { DefaultTarget: targets[0], DefaultTargets: targets }
}

// 辅助函数：单位转换
export const convertToBytes = (value: number, unit: 'B' | 'KB' | 'MB' | 'GB'): number => {
  if (value < 0) return 0
  const multipliers = {
    'B': 1,
    'KB': 1024,
    'MB': 1024 * 1024,
    'GB': 1024 * 1024 * 1024
  }
  return Math.floor(value * multipliers[unit])
}

export const convertBytesToUnit = (bytes: number): { value: number, unit: 'B' | 'KB' | 'MB' | 'GB' } => {
  if (bytes <= 0) return { value: 0, unit: 'MB' }
  const k = 1024
  const sizes: Array<'B' | 'KB' | 'MB' | 'GB'> = ['B', 'KB', 'MB', 'GB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return {
    value: Number((bytes / Math.pow(k, i)).toFixed(2)),
    unit: sizes[i]
  }
}
