"use client"

import { useEffect, useState, type ReactNode } from "react"
import { createToaster, Toast } from "@/components/panelui/toast"
import { errorMessage } from "@/lib/api"
import { ErrorDetailsDialog } from "@/components/error-details"

type ToastKind = "success" | "error" | "info" | "warning"

// A manager outside React lets non-component code raise toasts too.
const toast = createToaster()

export function ToastProvider({ children }: { children: ReactNode }) {
  const [error, setError] = useState<unknown>(null)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const show = (event: Event) => { setError((event as CustomEvent).detail); setOpen(true) }
    window.addEventListener("justcd-toast-details", show)
    return () => window.removeEventListener("justcd-toast-details", show)
  }, [])
  return <Toast.Provider toastManager={toast} limit={3} position="top-right" timeout={5500}>
    {children}
    <ErrorDetailsDialog error={error} open={open} onOpenChange={setOpen} />
  </Toast.Provider>
}

const titles: Record<ToastKind, string> = { success: "Success", error: "Error", warning: "Attention", info: "Update" }

function toastContent(kind: ToastKind, message: string, error?: unknown) {
  return {
    title: titles[kind],
    description: message,
    type: kind === "error" ? ("destructive" as const) : kind,
    timeout: kind === "error" && error !== undefined ? 0 : kind === "error" ? 8000 : 5500,
    actionProps: error !== undefined ? { children: "Details", onClick: () => window.dispatchEvent(new CustomEvent("justcd-toast-details", { detail: error })) } : undefined,
  }
}

function notify(kind: ToastKind, message: string, error?: unknown) {
  return toast.add(toastContent(kind, message, error))
}

/** Cancelled work (route change, navigation) should not leave an error behind. */
function isAbort(cause: unknown) {
  return cause instanceof Error && cause.name === "AbortError"
}

type PromiseMessages<T> = {
  loading: string
  success: string | ((value: T) => string)
  /** A message, a function of the error, or `null` to close the toast and let the caller report the failure. */
  error?: string | ((cause: unknown) => string) | null
}

const methods = {
  success: (message: string) => { notify("success", message) },
  error: (message: string, error?: unknown) => { notify("error", message, error) },
  info: (message: string) => { notify("info", message) },
  warning: (message: string) => { notify("warning", message) },
  /** A toast that stays until `finish` or `dismiss`; use it for work that takes a moment. */
  loading: (message: string) => toast.add({ title: "Working", description: message, type: "loading", timeout: 0 }),
  /** Turns a `loading` toast into its result. */
  finish: (id: string, kind: ToastKind, message: string, error?: unknown) => { toast.update(id, toastContent(kind, message, error)) },
  dismiss: (id?: string) => { toast.close(id) },
  /**
   * Follows a promise: shows `loading`, then turns into a success or an error toast. The result is
   * returned and a rejection is rethrown, so callers keep their own state handling. Pass `error: null`
   * when the caller reports failures itself; cancelled work closes the toast without an error.
   */
  async promise<T>(work: Promise<T>, messages: PromiseMessages<T>): Promise<T> {
    const id = toast.add({ title: "Working", description: messages.loading, type: "loading", timeout: 0 })
    try {
      const value = await work
      toast.update(id, toastContent("success", typeof messages.success === "function" ? messages.success(value) : messages.success))
      return value
    } catch (cause) {
      if (isAbort(cause) || messages.error === null) toast.close(id)
      else toast.update(id, toastContent("error", typeof messages.error === "function" ? messages.error(cause) : messages.error ?? errorMessage(cause), cause))
      throw cause
    }
  },
}

export function useToast() { return methods }
