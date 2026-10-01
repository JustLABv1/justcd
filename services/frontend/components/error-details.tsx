"use client"

import { useState } from "react"
import Link from "next/link"
import { AppDialog } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { APIError } from "@/lib/api"

const sensitiveKey =
  /token|secret|password|authorization|cookie|private.?key|api.?key|credential|kubeconfig|known.?hosts|certificate|csrf|session/i

function redactText(value: string) {
  return value
    .replace(/(bearer\s+)[\w.~+/=-]+/gi, "$1[redacted]")
    .replace(
      /((?:["']?(?:access[_-]?token|refresh[_-]?token|api[_-]?key|token|password|secret|authorization|cookie|credential|private[_-]?key|kubeconfig|known[_-]?hosts|client[_-]?certificate[_-]?data|certificate[_-]?authority[_-]?data|client[_-]?key[_-]?data|csrf[_-]?token)["']?)\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)/gi,
      "$1[redacted]"
    )
    .replace(/((?:https?|ssh|git):\/\/)[^/@\s]+:[^/@\s]+@/gi, "$1[redacted]@")
    .replace(
      /-----BEGIN [^-\r\n]*PRIVATE KEY-----[\s\S]*?-----END [^-\r\n]*PRIVATE KEY-----/g,
      "[redacted private key]"
    )
    .slice(0, 12000)
}

function sanitize(
  value: unknown,
  seen = new WeakSet<object>(),
  depth = 0
): unknown {
  if (typeof value === "string") return redactText(value)
  if (value == null || typeof value === "number" || typeof value === "boolean")
    return value
  if (typeof value === "bigint") return value.toString()
  if (typeof value !== "object") return String(value)
  if (depth > 6) return "[maximum detail depth reached]"
  if (seen.has(value)) return "[circular reference]"
  seen.add(value)

  if (value instanceof Error) {
    return {
      name: value.name,
      message: redactText(value.message),
      ...(value instanceof APIError
        ? {
            schemaVersion: value.schemaVersion,
            code: value.code,
            category: value.category,
            retryable: value.retryable,
            remediation: redactText(value.remediation),
            remediationUrl: value.remediationUrl,
          }
        : {}),
      ...(value.stack ? { stack: redactText(value.stack) } : {}),
      ...(value.cause !== undefined
        ? { cause: sanitize(value.cause, seen, depth + 1) }
        : {}),
    }
  }
  if (Array.isArray(value))
    return value.slice(0, 50).map((item) => sanitize(item, seen, depth + 1))
  return Object.fromEntries(
    Object.entries(value)
      .slice(0, 80)
      .map(([key, item]) => [
        key,
        sensitiveKey.test(key) ? "[redacted]" : sanitize(item, seen, depth + 1),
      ])
  )
}

function diagnosticSnapshot(error: unknown) {
  const details: Record<string, unknown> = {
    capturedAt: new Date().toISOString(),
    page: typeof window === "undefined" ? undefined : window.location.pathname,
  }

  if (error instanceof APIError) {
    details.name = error.name
    details.message = redactText(error.message)
    details.httpStatus = error.status || "Network request failed"
    details.errorInfo = {
      schemaVersion: error.schemaVersion,
      code: error.code,
      category: error.category,
      retryable: error.retryable,
      remediation: redactText(error.remediation),
      remediationUrl: error.remediationUrl,
    }
    details.request = {
      method: error.request.method,
      path: redactText(error.request.path),
    }
    if (error.payload !== null && error.payload !== undefined) {
      details.response = sanitize(error.payload)
    }
    if (error.cause !== undefined) details.cause = sanitize(error.cause)
    if (error.stack) details.stack = redactText(error.stack)
  } else if (error instanceof Error) {
    details.name = error.name
    details.message = redactText(error.message)
    if (error.cause !== undefined) details.cause = sanitize(error.cause)
    if (error.stack) details.stack = redactText(error.stack)
  } else {
    details.error = sanitize(error)
  }

  return JSON.stringify(details, null, 2)
}

export function ErrorGuidance({ error }: { error: unknown }) {
  if (!(error instanceof APIError) || !error.remediation) return null

  return (
    <p className="text-sm leading-5 text-muted-foreground">
      <span className="font-medium text-foreground">
        {error.code} · {error.retryable ? "Retryable" : "Resolve before retrying"}:
      </span>{" "}
      {error.remediation}{" "}
      {error.remediationUrl && (
        <Link
          href={error.remediationUrl}
          className="font-medium text-primary underline underline-offset-4"
        >
          Open related settings
        </Link>
      )}
    </p>
  )
}

function ErrorDetailsBody({ error }: { error: unknown }) {
  const [details] = useState(() => diagnosticSnapshot(error))
  const [copied, setCopied] = useState(false)

  async function copyDetails() {
    try {
      await navigator.clipboard.writeText(details)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="space-y-3">
      <ErrorGuidance error={error} />
      <div className="flex justify-end">
        <Button type="button" size="sm" variant="outline" onClick={() => void copyDetails()} aria-live="polite">
          {copied ? "Copied" : "Copy details"}
        </Button>
      </div>
      <pre className="max-h-[55dvh] overflow-auto rounded-lg border bg-muted/40 p-3 text-xs leading-5 break-words whitespace-pre-wrap text-foreground">
        {details}
      </pre>
    </div>
  )
}

/** Read-only diagnostics dialog. Shared by the "View details" button and the toast "Details" action. */
export function ErrorDetailsDialog({
  error,
  open,
  onOpenChange,
}: {
  error: unknown
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <AppDialog
      open={open}
      onOpenChange={onOpenChange}
      variant="info"
      size="xl"
      title="Error details"
      description="Diagnostic information for troubleshooting. Request headers and body are not included."
    >
      <ErrorDetailsBody error={error} />
    </AppDialog>
  )
}

export function ErrorDetailsButton({
  error,
  label = "View details",
}: {
  error: unknown
  label?: string
}) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button type="button" size="xs" variant="outline" onClick={() => setOpen(true)}>
        {label}
      </Button>
      <ErrorDetailsDialog error={error} open={open} onOpenChange={setOpen} />
    </>
  )
}
