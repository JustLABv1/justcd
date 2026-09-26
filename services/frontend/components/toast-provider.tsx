"use client"

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { Toast } from "@base-ui/react/toast"
import { ErrorDetailsButton, ErrorGuidance } from "@/components/error-details"

type ToastKind = "success" | "error" | "info" | "warning"
type ToastData = { kind: ToastKind; error?: unknown }
type ToastMethods = {
  success: (message: string) => void
  error: (message: string, error?: unknown) => void
  info: (message: string) => void
  warning: (message: string) => void
}

const ToastContext = createContext<ToastMethods | null>(null)

const toastStyles: Record<ToastKind, { icon: string; className: string; iconClass: string }> = {
  success: {
    icon: "✓",
    className:
      "border-emerald-200 bg-background text-foreground dark:border-emerald-900",
    iconClass: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  },
  error: {
    icon: "!",
    className: "border-destructive/30 bg-background text-foreground",
    iconClass: "bg-destructive/10 text-destructive",
  },
  info: {
    icon: "i",
    className: "border-border bg-background text-foreground",
    iconClass: "bg-primary/10 text-primary",
  },
  warning: {
    icon: "!",
    className:
      "border-amber-300 bg-background text-foreground dark:border-amber-900",
    iconClass: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  },
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toastManager] = useState(() => Toast.createToastManager<ToastData>())

  const pushToast = useCallback((kind: ToastKind, message: string, error?: unknown) => {
    toastManager.add({
      title: kind === "success" ? "Success" : kind === "error" ? "Error" : kind === "warning" ? "Attention" : "Update",
      description: message,
      type: kind,
      priority: kind === "error" || kind === "warning" ? "high" : "low",
      timeout: kind === "error" && error !== undefined ? 0 : kind === "error" ? 8000 : 5500,
      data: { kind, error },
    })
  }, [toastManager])

  const methods = useMemo<ToastMethods>(
    () => ({
      success: (message) => pushToast("success", message),
      error: (message, error) => pushToast("error", message, error),
      info: (message) => pushToast("info", message),
      warning: (message) => pushToast("warning", message),
    }),
    [pushToast],
  )

  return (
    <Toast.Provider toastManager={toastManager} limit={4}>
      <ToastContext.Provider value={methods}>
        {children}
        <Toast.Portal>
          <Toast.Viewport className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-[calc(100vw-2rem)] flex-col gap-2 outline-none sm:w-96">
            <ToastList />
          </Toast.Viewport>
        </Toast.Portal>
      </ToastContext.Provider>
    </Toast.Provider>
  )
}

export function useToast() {
  const toast = useContext(ToastContext)
  if (!toast) {
    throw new Error("useToast must be used within a ToastProvider")
  }
  return toast
}

function ToastList() {
  const { toasts } = Toast.useToastManager<ToastData>()

  return toasts.map((toast) => {
    const styles = toastStyles[toast.data?.kind ?? "info"]
    return (
      <Toast.Root
        key={toast.id}
        toast={toast}
        className={`pointer-events-auto flex items-start gap-3 rounded-xl border p-3.5 text-sm shadow-lg shadow-black/8 transition-[transform,opacity] duration-200 data-starting-style:translate-x-3 data-starting-style:opacity-0 data-ending-style:translate-x-3 data-ending-style:opacity-0 data-limited:opacity-0 motion-reduce:transition-none ${styles.className}`}
      >
        <Toast.Content className="flex w-full items-start gap-3">
          <span
            aria-hidden="true"
            className={`mt-px grid size-5 shrink-0 place-items-center rounded-full text-[11px] font-semibold ${styles.iconClass}`}
          >
            {styles.icon}
          </span>
          <div className="min-w-0 flex-1">
            <Toast.Title className="font-medium" />
            <Toast.Description className="mt-0.5 break-words leading-5 text-muted-foreground" />
            {toast.data?.error !== undefined && (
              <div className="mt-2 space-y-2">
                <ErrorGuidance error={toast.data.error} />
                <ErrorDetailsButton error={toast.data.error} />
              </div>
            )}
          </div>
          <Toast.Close
            aria-label="Dismiss notification"
            className="grid size-6 shrink-0 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            <span aria-hidden="true">×</span>
          </Toast.Close>
        </Toast.Content>
      </Toast.Root>
    )
  })
}
