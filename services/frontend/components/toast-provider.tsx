"use client"

import { useEffect, useState, type ReactNode } from "react"
import { toast, Toaster, useToastManager } from "@/components/ui/toast"
import { ErrorDetailsDialog } from "@/components/error-details"

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
    <ErrorDetailsDialog error={error} open={open} onOpenChange={setOpen} />
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
