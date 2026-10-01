"use client"

import { useState, type ReactNode } from "react"
import { HugeiconsIcon } from "@hugeicons/react"
import { Copy01Icon, Layers01Icon, RefreshIcon, Tick02Icon } from "@hugeicons/core-free-icons"
import { Badge } from "@/components/reui/badge"
import { IconStack } from "@/components/reui/icon-stack"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ErrorDetailsButton } from "@/components/error-details"
import { Panel } from "@/components/ui-kit"
import type { ActionItem } from "@/components/action-menu"
import { useToast } from "@/components/toast-provider"
import { apiDelete, errorMessage } from "@/lib/api"
import type { Credential } from "@/lib/types"

/**
 * Runs an async mutation, reports the outcome with a toast and resolves to
 * `true` on success. `after` only runs when the work succeeded.
 */
export type ActionRunner = <T>(
  work: () => Promise<T>,
  success: string | ((result: T) => string),
  after?: (value: T) => void
) => Promise<boolean>

export const monoTextareaClass = "min-h-24 font-mono text-sm"

export const credentialKindLabels: Record<Credential["kind"], string> = {
  "kubernetes-token": "Kubernetes bearer token",
  kubeconfig: "Static kubeconfig",
  "git-https": "Git over HTTPS",
  "git-ssh": "Git over SSH",
}

type BadgeVariant = NonNullable<React.ComponentProps<typeof Badge>["variant"]>

const statusLabels: Record<string, string> = {
  passed: "Passed",
  partial: "Some permissions missing",
  missing: "Missing",
  failed: "Failed",
  not_configured: "Not configured",
  unknown: "Unknown",
}

/** Plain-language label for raw API statuses. */
export function statusLabel(status: string) {
  if (statusLabels[status]) return statusLabels[status]
  const text = status.replaceAll("_", " ")
  return text.charAt(0).toUpperCase() + text.slice(1)
}

export function statusVariant(status: string): BadgeVariant {
  if (status === "passed") return "success-light"
  if (status === "partial" || status === "missing") return "warning-light"
  if (status === "failed") return "destructive-light"
  return "outline"
}

/** Small `text-sm text-destructive` inline validation message. */
export function FieldError({ id, children }: { id: string; children?: ReactNode }) {
  if (!children) return null
  return <p id={id} role="alert" className="text-sm text-destructive">{children}</p>
}

/** Inline notice for failed loads, always with a retry. */
export function LoadErrorNotice({ error, title, onRetry }: { error: unknown; title: string; onRetry: () => void }) {
  return (
    <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
      <div className="min-w-0">
        <p className="font-medium text-destructive">{title}</p>
        <p className="mt-0.5 break-words text-muted-foreground">{errorMessage(error)}</p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <ErrorDetailsButton error={error} />
        <Button type="button" size="sm" variant="outline" onClick={onRetry}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} aria-hidden="true" />
          Retry
        </Button>
      </div>
    </div>
  )
}

/** Skeleton shaped like an inventory panel with a few rows. */
export function SectionSkeleton({ rows = 3, label = "Loading" }: { rows?: number; label?: string }) {
  return (
    <div role="status" aria-label={label} className="rounded-xl border bg-card">
      <span className="sr-only">{label}</span>
      <div className="flex items-center justify-between gap-4 border-b px-5 py-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-5 w-8 rounded-full" />
      </div>
      <ul className="divide-y">
        {Array.from({ length: rows }, (_, index) => (
          <li key={index} className="flex items-center gap-4 px-5 py-4">
            <Skeleton className="size-9 shrink-0 rounded-lg" />
            <div className="min-w-0 flex-1 space-y-2">
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-3 w-2/3" />
            </div>
            <Skeleton className="h-8 w-16" />
          </li>
        ))}
      </ul>
    </div>
  )
}

/** Compact skeleton for fields inside a dialog. */
export function FieldsSkeleton({ label = "Loading" }: { label?: string }) {
  return (
    <div role="status" aria-label={label} className="space-y-3">
      <span className="sr-only">{label}</span>
      <Skeleton className="h-4 w-32" />
      <Skeleton className="h-9 w-full" />
    </div>
  )
}

/** Panel with a count badge in the header, an optional toolbar row and primary action. */
export function InventoryPanel({ title, description, count, action, toolbar, children }: {
  title: string
  description?: string
  count: number
  action?: ReactNode
  toolbar?: ReactNode
  children: ReactNode
}) {
  return (
    <Panel
      title={title}
      description={description}
      action={
        <div className="flex items-center gap-3">
          <Badge variant="secondary" radius="full" aria-label={`${count} total`} className="tabular-nums">{count}</Badge>
          {action}
        </div>
      }
      className="overflow-hidden"
    >
      {toolbar && <div className="border-b px-5 py-3">{toolbar}</div>}
      {children}
    </Panel>
  )
}

/** Empty state with a primary CTA for permitted users and an "ask an owner" hint for others. */
export function SectionEmpty({ title, description, action, hint }: {
  title: string
  description: string
  /** Rendered when the user may act; omit to show the hint instead. */
  action?: ReactNode
  hint?: string
}) {
  return (
    <div className="flex flex-col items-center justify-center px-5 py-12 text-center">
      <IconStack className="empty-state-icon-stack text-primary"><HugeiconsIcon icon={Layers01Icon} className="size-6" /></IconStack>
      <h3 className="mt-4 text-sm font-semibold">{title}</h3>
      <p className="mt-1 max-w-sm text-sm leading-5 text-muted-foreground">{description}</p>
      {action ? <div className="mt-4">{action}</div> : hint ? <p className="mt-4 max-w-sm text-sm text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

/** Monospace code with a copy button. `block` renders a scrollable `<pre>`. */
export function CopyCode({ value, label, block = false }: { value: string; label: string; block?: boolean }) {
  const toast = useToast()
  const [copied, setCopied] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      toast.success(`${label} copied.`)
      window.setTimeout(() => setCopied(false), 2000)
    } catch (cause) {
      toast.error("Could not copy to the clipboard.", cause)
    }
  }
  const button = (
    <Button type="button" size="icon-xs" variant="ghost" aria-label={`Copy ${label}`} onClick={() => void copy()}>
      <HugeiconsIcon icon={copied ? Tick02Icon : Copy01Icon} strokeWidth={2} aria-hidden="true" />
    </Button>
  )
  if (block) {
    return (
      <div className="relative">
        <pre className="overflow-x-auto rounded-md bg-muted p-3 pr-12 text-xs">{value}</pre>
        <div className="absolute top-2 right-2">{button}</div>
      </div>
    )
  }
  return (
    <div className="flex items-start gap-2 rounded-md bg-muted px-2 py-1.5">
      <code className="min-w-0 flex-1 break-all font-mono text-xs">{value}</code>
      {button}
    </div>
  )
}

/** Tracks a local "saving" flag around an async submit so dialogs only block on their own work. */
export function useSaving() {
  const [saving, setSaving] = useState(false)
  async function track<T>(work: Promise<T>) {
    setSaving(true)
    try {
      return await work
    } finally {
      setSaving(false)
    }
  }
  return { saving, track }
}

/** Destructive row action with a confirmation that names the object. */
export function deleteItem({ label = "Delete", name, description, confirmLabel, endpoint, successMessage, onDeleted, toast, disabled }: {
  label?: string
  name: string
  description: string
  confirmLabel: string
  endpoint: string
  successMessage: string
  onDeleted: () => void
  toast: ReturnType<typeof useToast>
  disabled?: boolean
}): ActionItem {
  return {
    label,
    destructive: true,
    disabled,
    confirm: {
      title: `${label} ${name}?`,
      description,
      confirmLabel,
      onConfirm: async () => {
        await apiDelete(endpoint)
        onDeleted()
        toast.success(successMessage)
      },
    },
  }
}
