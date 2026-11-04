import type { ReactNode } from "react"
import { Checkbox } from "@/components/ui/checkbox"
import { Card, CardContent } from "@/components/ui/card"
import { FormField } from "@/components/ui-kit"

const publishingControlStyles =
  "[&_[data-slot=input]]:h-10 [&_[data-slot=input]]:rounded-xl [&_[data-slot=input]]:border [&_[data-slot=input]]:border-input dark:[&_[data-slot=input]]:border-0 [&_[data-slot=input]]:bg-background [&_[data-slot=input]]:px-4 [&_[data-slot=input]]:shadow-none [&_[data-slot=select-trigger]]:h-10 [&_[data-slot=select-trigger]]:rounded-xl [&_[data-slot=select-trigger]]:border [&_[data-slot=select-trigger]]:border-input dark:[&_[data-slot=select-trigger]]:border-0 [&_[data-slot=select-trigger]]:bg-background [&_[data-slot=select-trigger]]:px-4"

export function PublishingCard({
  children,
  className = "",
  contentClassName = "",
}: {
  children: ReactNode
  className?: string
  contentClassName?: string
}) {
  return (
    <Card
      className={`h-full rounded-2xl bg-card py-4 ring-1 ring-border dark:bg-muted/45 dark:ring-0 ${className}`}
    >
      <CardContent
        className={`px-4 ${publishingControlStyles} ${contentClassName}`}
      >
        {children}
      </CardContent>
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
    <PublishingCard
      className={className}
      contentClassName="[&_label]:mb-2 [&_label]:text-sm [&_label]:font-medium [&_p]:mt-2 [&_p]:text-xs [&_p]:leading-5"
    >
      <FormField label={label} htmlFor={htmlFor} hint={hint}>
        {children}
      </FormField>
    </PublishingCard>
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
  return (
    <section className={`space-y-3 ${className}`}>
      {title && (
        <div>
          <h2 className="text-base font-medium tracking-tight">{title}</h2>
          {description && (
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              {description}
            </p>
          )}
        </div>
      )}
      {children}
    </section>
  )
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
  return (
    <PublishingCard>
      <label
        htmlFor={id}
        className={`flex items-start gap-3 text-sm ${disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer"}`}
      >
        <Checkbox
          id={id}
          checked={checked}
          disabled={disabled}
          onCheckedChange={(next) => onCheckedChange(Boolean(next))}
          aria-labelledby={`${id}-label`}
          aria-describedby={description ? `${id}-description` : undefined}
          className="mt-0.5 shrink-0"
        />
        <span className="min-w-0">
          <span id={`${id}-label`} className="block font-medium">
            {title}
          </span>
          {description && (
            <span
              id={`${id}-description`}
              className="mt-2 block text-xs leading-5 text-muted-foreground"
            >
              {description}
            </span>
          )}
        </span>
      </label>
    </PublishingCard>
  )
}
