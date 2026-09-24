"use client"

import { useState } from "react"
import { AlertDialog } from "@base-ui/react/alert-dialog"
import { Button } from "@/components/ui/button"
import { APIError } from "@/lib/api"

const sensitiveKey = /token|secret|password|authorization|cookie|private.?key|credential|csrf|session/i

function redactText(value: string) {
  return value
    .replace(/(bearer\s+)[\w.~+/=-]+/gi, "$1[redacted]")
    .replace(/((?:access[_-]?token|refresh[_-]?token|api[_-]?key|token|password|secret|authorization|cookie|csrf[_-]?token)\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)/gi, "$1[redacted]")
    .slice(0, 12000)
}

function sanitize(value: unknown, seen = new WeakSet<object>(), depth = 0): unknown {
  if (typeof value === "string") return redactText(value)
  if (value == null || typeof value === "number" || typeof value === "boolean") return value
  if (typeof value === "bigint") return value.toString()
  if (typeof value !== "object") return String(value)
  if (depth > 6) return "[maximum detail depth reached]"
  if (seen.has(value)) return "[circular reference]"
  seen.add(value)

  if (value instanceof Error) {
    return {
      name: value.name,
      message: redactText(value.message),
      ...(value.stack ? { stack: redactText(value.stack) } : {}),
      ...(value.cause !== undefined ? { cause: sanitize(value.cause, seen, depth + 1) } : {}),
    }
  }
  if (Array.isArray(value)) return value.slice(0, 50).map((item) => sanitize(item, seen, depth + 1))
  return Object.fromEntries(
    Object.entries(value).slice(0, 80).map(([key, item]) => [
      key,
      sensitiveKey.test(key) ? "[redacted]" : sanitize(item, seen, depth + 1),
    ]),
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

export function ErrorDetailsButton({
  error,
  label = "View details",
}: {
  error: unknown
  label?: string
}) {
  const [open, setOpen] = useState(false)
  const [details, setDetails] = useState("")
  const [copied, setCopied] = useState(false)

  function changeOpen(next: boolean) {
    if (next) {
      setDetails(diagnosticSnapshot(error))
      setCopied(false)
    }
    setOpen(next)
  }

  async function copyDetails() {
    try {
      await navigator.clipboard.writeText(details)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <AlertDialog.Root open={open} onOpenChange={changeOpen}>
      <AlertDialog.Trigger render={<Button type="button" size="xs" variant="outline" />}>
        {label}
      </AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className="fixed inset-0 z-[110] bg-black/55 backdrop-blur-[2px] data-open:animate-in data-open:fade-in-0" />
        <AlertDialog.Viewport className="fixed inset-0 z-[110] grid place-items-center overflow-y-auto p-4">
          <AlertDialog.Popup className="flex max-h-[calc(100vh-2rem)] w-full max-w-3xl flex-col rounded-xl border bg-card p-5 text-card-foreground shadow-2xl outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95">
            <AlertDialog.Title className="text-base font-semibold">
              Error details
            </AlertDialog.Title>
            <AlertDialog.Description className="mt-1 text-sm leading-6 text-muted-foreground">
              Diagnostic information for troubleshooting. Request headers and body are not included.
            </AlertDialog.Description>
            <pre className="mt-4 min-h-0 flex-1 overflow-auto rounded-lg border bg-muted/40 p-3 text-xs leading-5 whitespace-pre-wrap break-words text-foreground">
              {details}
            </pre>
            <div className="mt-4 flex justify-end gap-2 border-t pt-4">
              <Button type="button" size="sm" variant="outline" onClick={() => void copyDetails()}>
                {copied ? "Copied" : "Copy details"}
              </Button>
              <AlertDialog.Close render={<Button type="button" size="sm" />}>Close</AlertDialog.Close>
            </div>
          </AlertDialog.Popup>
        </AlertDialog.Viewport>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  )
}
