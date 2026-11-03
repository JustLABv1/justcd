import type { ReactNode } from "react"
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
