"use client"

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

const EMPTY_VALUE = "__justcd_empty_value__"

type FormSelectProps = {
  id?: string
  value: string
  onValueChange: (value: string) => void
  items: { value: string; label: string }[]
  placeholder?: string
  ariaLabel?: string
  emptyOption?: string
  disabled?: boolean
  required?: boolean
  className?: string
  size?: "sm" | "default"
}

function FormSelect({ id, value, onValueChange, items, placeholder, ariaLabel, emptyOption, disabled, required, className, size = "default" }: FormSelectProps) {
  const selectValue = value || (emptyOption ? EMPTY_VALUE : null)
  const valueItems = [
    ...(emptyOption ? [{ value: EMPTY_VALUE, label: emptyOption }] : []),
    ...items,
  ]
  return (
    <Select items={valueItems} value={selectValue} onValueChange={(next) => onValueChange(next === EMPTY_VALUE || next == null ? "" : next)} disabled={disabled}>
      <SelectTrigger id={id} aria-label={ariaLabel} aria-required={required} size={size} className={`w-full ${className ?? ""}`}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent align="start">
        {emptyOption && <SelectItem value={EMPTY_VALUE}>{emptyOption}</SelectItem>}
        {items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
      </SelectContent>
    </Select>
  )
}

export { FormSelect }
