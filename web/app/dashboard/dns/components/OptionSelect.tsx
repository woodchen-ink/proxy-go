"use client";

import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const NONE = "__none__";

interface Option {
  value: string;
  label: string;
  disabled?: boolean;
}

// OptionSelect 带标签的下拉; clearable 时首项为「不使用」(值为空串)
export function OptionSelect({
  label,
  value,
  options,
  placeholder,
  clearable,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  options: Option[];
  placeholder?: string;
  clearable?: boolean;
  disabled?: boolean;
  onChange: (v: string) => void;
}) {
  return (
    <div className="space-y-1">
      <Label>{label}</Label>
      <Select
        value={value || (clearable ? NONE : "")}
        onValueChange={(v) => onChange(v === NONE ? "" : v)}
        disabled={disabled}
      >
        <SelectTrigger>
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {clearable && <SelectItem value={NONE}>不使用</SelectItem>}
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value} disabled={o.disabled}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
