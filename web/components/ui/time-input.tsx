"use client"

import * as React from "react"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

export type TimeUnit = "minute" | "hour" | "day"

interface TimeInputProps {
  /** 时间值（以分钟为单位） */
  value: number
  /** 值变化回调（返回分钟数） */
  onChange: (minutes: number) => void
  /** 输入框 placeholder */
  placeholder?: string
  /** 输入框 id */
  id?: string
  /** 最小值（分钟） */
  min?: number
  /** 最大值（分钟） */
  max?: number
  /** 是否禁用 */
  disabled?: boolean
  /** 默认时间单位 */
  defaultUnit?: TimeUnit
}

const TIME_UNITS = {
  minute: { label: "分钟", multiplier: 1 },
  hour: { label: "小时", multiplier: 60 },
  day: { label: "天", multiplier: 1440 },
} as const

/**
 * 时间输入组件
 *
 * 用户可以选择时间单位（天/小时/分钟）并输入数字
 * 组件内部自动转换为分钟数
 *
 * @example
 * ```tsx
 * <TimeInput
 *   value={maxAge}
 *   onChange={setMaxAge}
 *   placeholder="30"
 * />
 * ```
 */
export function TimeInput({
  value,
  onChange,
  placeholder = "0",
  id,
  min = 0,
  max,
  disabled = false,
  defaultUnit = "minute",
}: TimeInputProps) {
  // 自动选择最合适的时间单位
  const getOptimalUnit = (minutes: number): TimeUnit => {
    if (minutes === 0) return defaultUnit
    if (minutes % 1440 === 0) return "day"
    if (minutes % 60 === 0) return "hour"
    return "minute"
  }

  const [unit, setUnit] = React.useState<TimeUnit>(() => getOptimalUnit(value))

  // 计算当前单位下的显示值
  const displayValue = React.useMemo(() => {
    if (value === 0) return 0
    const multiplier = TIME_UNITS[unit].multiplier
    return Math.round(value / multiplier)
  }, [value, unit])

  // 处理输入值变化
  const handleValueChange = (inputValue: string) => {
    const num = parseInt(inputValue) || 0
    const minutes = num * TIME_UNITS[unit].multiplier

    // 应用最小/最大值限制
    let finalMinutes = Math.max(min, minutes)
    if (max !== undefined) {
      finalMinutes = Math.min(max, finalMinutes)
    }

    onChange(finalMinutes)
  }

  // 处理单位变化
  const handleUnitChange = (newUnit: TimeUnit) => {
    setUnit(newUnit)
    // 保持实际分钟数不变，只改变显示单位
    // 但需要重新计算显示值以避免精度问题
    const currentMinutes = value
    const newMultiplier = TIME_UNITS[newUnit].multiplier
    const newDisplayValue = Math.round(currentMinutes / newMultiplier)
    onChange(newDisplayValue * newMultiplier)
  }

  return (
    <div className="flex gap-2">
      <Input
        id={id}
        type="number"
        min={Math.ceil(min / TIME_UNITS[unit].multiplier)}
        max={max !== undefined ? Math.floor(max / TIME_UNITS[unit].multiplier) : undefined}
        value={displayValue}
        onChange={(e) => handleValueChange(e.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        className="flex-1"
      />
      <Select value={unit} onValueChange={handleUnitChange} disabled={disabled}>
        <SelectTrigger className="w-[100px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="minute">分钟</SelectItem>
          <SelectItem value="hour">小时</SelectItem>
          <SelectItem value="day">天</SelectItem>
        </SelectContent>
      </Select>
    </div>
  )
}
