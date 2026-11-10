"use client"

import { Select } from "@/components/panelui/select"

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

/** The app's select, drawn with PanelUI. Keeps the old API so every form shares one look. */
function FormSelect({ id, value, onValueChange, items, placeholder, ariaLabel, emptyOption, disabled, required, className, size = "default" }: FormSelectProps) {
  const selectValue = value || (emptyOption && !required ? EMPTY_VALUE : null)
  const valueItems = [
    ...(emptyOption ? [{ value: EMPTY_VALUE, label: emptyOption }] : []),
    ...items,
  ]
  return (
    <Select items={valueItems} value={selectValue} onValueChange={(next) => onValueChange(next === EMPTY_VALUE || next == null ? "" : String(next))} disabled={disabled} required={required}>
      <Select.Trigger id={id} aria-label={ariaLabel} aria-required={required} size={size === "sm" ? "sm" : "md"} className={`w-full ${className ?? ""}`}>
        <Select.Value placeholder={placeholder ?? emptyOption} />
      </Select.Trigger>
      <Select.Content>
        {valueItems.map((item) => <Select.Item key={item.value} value={item.value} disabled={required && item.value === EMPTY_VALUE}>{item.label}</Select.Item>)}
      </Select.Content>
    </Select>
  )
}

export { FormSelect }
