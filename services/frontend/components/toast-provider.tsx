"use client"

import { useEffect, useState, type ReactNode } from "react"
import { Dialog } from "@base-ui/react/dialog"
import { toast, Toaster, useToastManager } from "@/components/ui/toast"
import { Button } from "@/components/ui/button"
import { ErrorGuidance, ErrorDetailsButton } from "@/components/error-details"

type ToastKind = "success" | "error" | "info" | "warning"

function ToastClearance() {
  const { toasts } = useToastManager()
  useEffect(() => {
    let frame = 0
    let until = 0
    const measure = () => {
      const elements = document.querySelectorAll<HTMLElement>('[data-slot="toast"]:not([data-limited])')
      const top = Math.min(window.innerHeight, ...Array.from(elements, element => element.getBoundingClientRect().top))
      const clearance = elements.length ? Math.max(0, window.innerHeight - top + 12) : 0
      document.documentElement.style.setProperty("--toast-clearance", `${clearance}px`)
      if (performance.now() < until) frame = requestAnimationFrame(measure)
    }
    const update = () => {
      cancelAnimationFrame(frame)
      until = performance.now() + 600
      frame = requestAnimationFrame(measure)
    }
    const viewport = document.querySelector('[data-slot="toast-viewport"]')
    const observer = new ResizeObserver(update)
    document.querySelectorAll('[data-slot="toast"]').forEach(element => observer.observe(element))
    viewport?.addEventListener("pointerenter", update)
    viewport?.addEventListener("pointerleave", update)
    window.addEventListener("resize", update)
    update()
    return () => {
      cancelAnimationFrame(frame)
      observer.disconnect()
      viewport?.removeEventListener("pointerenter", update)
      viewport?.removeEventListener("pointerleave", update)
      window.removeEventListener("resize", update)
    }
  }, [toasts])
  useEffect(() => () => { document.documentElement.style.removeProperty("--toast-clearance") }, [])
  return null
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [error, setError] = useState<unknown>(null)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const show = (event: Event) => { setError((event as CustomEvent).detail); setOpen(true) }
    window.addEventListener("justcd-toast-details", show)
    return () => window.removeEventListener("justcd-toast-details", show)
  }, [])
  return <Toaster limit={3}>
    {children}
    <ToastClearance />
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-[110] bg-black/50" />
        <Dialog.Viewport className="fixed inset-0 z-[110] grid place-items-center overflow-auto p-4">
          <Dialog.Popup className="w-full max-w-lg space-y-4 rounded-xl border bg-card p-5 shadow-xl">
            <Dialog.Title className="font-semibold">Error details</Dialog.Title>
            <Dialog.Description className="text-sm text-muted-foreground">Review the cause and available troubleshooting details.</Dialog.Description>
            <ErrorGuidance error={error} />
            <ErrorDetailsButton error={error} />
            <Dialog.Close render={<Button variant="outline" />}>Close</Dialog.Close>
          </Dialog.Popup>
        </Dialog.Viewport>
      </Dialog.Portal>
    </Dialog.Root>
  </Toaster>
}

function notify(kind: ToastKind, message: string, error?: unknown) {
  return toast.add({
    title: kind === "success" ? "Success" : kind === "error" ? "Error" : kind === "warning" ? "Attention" : "Update",
    description: message,
    type: kind,
    timeout: kind === "error" && error !== undefined ? 0 : kind === "error" ? 8000 : 5500,
    ...(error !== undefined ? { actionProps: { children: "Details", onClick: () => window.dispatchEvent(new CustomEvent("justcd-toast-details", { detail: error })) } } : {}),
  })
}

const methods = {
  success: (message: string) => { notify("success", message) },
  error: (message: string, error?: unknown) => { notify("error", message, error) },
  info: (message: string) => { notify("info", message) },
  warning: (message: string) => { notify("warning", message) },
}

export function useToast() { return methods }
