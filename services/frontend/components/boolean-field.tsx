"use client"

import { useId, type ReactNode } from "react"
import { Checkbox } from "@/components/panelui/checkbox"
import { Label } from "@/components/panelui/label"
import { Switch } from "@/components/panelui/switch"

type BooleanFieldProps = {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  label: ReactNode
  description?: ReactNode
  disabled?: boolean
  id?: string
  tone?: "default" | "warning"
}

/** Plain label/control rows sit inside their parent's surface without drawing another box. */
function BooleanField({ checked, onCheckedChange, label, description, disabled, id: suppliedId, tone = "default", immediate }: BooleanFieldProps & { immediate: boolean }) {
  const generatedId = useId()
  const id = suppliedId ?? generatedId
  const content = <span className="min-w-0"><span id={`${id}-label`} className={`block text-sm font-medium ${tone === "warning" ? "text-warning-foreground dark:text-warning" : ""}`}>{label}</span>{description && <span id={`${id}-description`} className="mt-1 block text-sm leading-5 text-muted-foreground">{description}</span>}</span>
  return <div className={`flex items-start gap-3 ${immediate ? "justify-between" : ""} ${disabled ? "opacity-60" : ""}`}>
    {!immediate && <Checkbox id={id} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} aria-labelledby={`${id}-label`} aria-describedby={description ? `${id}-description` : undefined} className="mt-0.5 shrink-0" />}
    <Label htmlFor={id} className="min-w-0 flex-1 cursor-pointer">{content}</Label>
    {immediate && <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange} aria-labelledby={`${id}-label`} aria-describedby={description ? `${id}-description` : undefined} className="mt-0.5 shrink-0" />}
  </div>
}

/** A setting that takes effect as soon as it changes. */
export function SwitchField(props: BooleanFieldProps) { return <BooleanField {...props} immediate /> }

/** A draft choice that takes effect when the enclosing form is saved. */
export function CheckboxField(props: BooleanFieldProps) { return <BooleanField {...props} immediate={false} /> }

/** Compatibility name for saved options; the shared field keeps title and help separately associated. */
export function CheckboxCard({ title, ...props }: { checked: boolean; onCheckedChange: (checked: boolean) => void; title: ReactNode; description?: ReactNode; disabled?: boolean; id?: string }) {
  return <CheckboxField {...props} label={title} />
}
