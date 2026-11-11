import type { ReactNode } from "react"
import { Card } from "@/components/panelui/card"
import { Section } from "@/components/section"
import { CheckboxField } from "@/components/ui-kit"
import { PanelField } from "@/components/applications/panel-wizard"

export function PublishingCard({
  children,
  className = "",
  contentClassName = "",
  size = "default",
}: {
  children: ReactNode
  className?: string
  contentClassName?: string
  size?: "default" | "sm"
}) {
  return (
    <Card size={size} className={`h-full ${className}`}>
      <Card.Content className={contentClassName}>
        {children}
      </Card.Content>
    </Card>
  )
}

/** All publishing sections share one surface and control treatment. */
export function PublishingField({
  label,
  htmlFor,
  hint,
  children,
  className = "",
}: {
  label: string
  htmlFor: string
  hint?: string
  children: ReactNode
  className?: string
}) {
  return (
    <PanelField label={label} htmlFor={htmlFor} hint={hint} className={className}>
      {children}
    </PanelField>
  )
}

export function PublishingSection({
  title,
  description,
  children,
  className = "",
}: {
  title?: string
  description?: string
  children: ReactNode
  className?: string
}) {
  return <Section surface title={title} description={description} className={className}>{children}</Section>
}

/** Boolean publishing options use the same surface as the surrounding fields. */
export function PublishingCheckbox({
  id,
  checked,
  onCheckedChange,
  title,
  description,
  disabled,
}: {
  id: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  title: ReactNode
  description?: ReactNode
  disabled?: boolean
}) {
  return <CheckboxField id={id} checked={checked} onCheckedChange={onCheckedChange} label={title} description={description} disabled={disabled} />
}
