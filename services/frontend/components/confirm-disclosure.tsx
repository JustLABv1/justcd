"use client"

import { useState, type ComponentProps, type ReactNode } from "react"
import { AlertDialog } from "@base-ui/react/alert-dialog"
import { Button } from "@/components/ui/button"
import { errorMessage } from "@/lib/api"

export function ConfirmDisclosure({ trigger, title, description, confirmLabel, onConfirm, children, disabled = false, triggerVariant = "destructive", open: controlledOpen, onOpenChange }: {
  trigger?: ReactNode
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => Promise<void>
  children?: ReactNode
  disabled?: boolean
  triggerVariant?: ComponentProps<typeof Button>["variant"]
  open?: boolean
  onOpenChange?: (open: boolean) => void
}) {
  const [localOpen, setLocalOpen] = useState(false)
  const open = controlledOpen ?? localOpen
  const setOpen = onOpenChange ?? setLocalOpen
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  async function confirm() {
    setBusy(true); setError("")
    try { await onConfirm(); setOpen(false) }
    catch (cause) { setError(errorMessage(cause)) }
    finally { setBusy(false) }
  }
  return <AlertDialog.Root open={open} onOpenChange={(next) => { if (!busy) { setOpen(next); if (!next) setError("") } }}>
    {trigger && <AlertDialog.Trigger render={<Button type="button" variant={triggerVariant} size="sm" disabled={disabled} />}>{trigger}</AlertDialog.Trigger>}
    <AlertDialog.Portal>
      <AlertDialog.Backdrop className="fixed inset-0 z-50 bg-black/55 backdrop-blur-[2px] data-open:animate-in data-open:fade-in-0" />
      <AlertDialog.Viewport className="fixed inset-0 z-50 grid place-items-center overflow-y-auto p-4">
        <AlertDialog.Popup className="w-full max-w-md rounded-xl border bg-card p-5 text-card-foreground shadow-2xl outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95">
          <AlertDialog.Title className="text-base font-semibold">{title}</AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-sm leading-6 text-muted-foreground">{description}</AlertDialog.Description>
          {children && <div className="mt-4">{children}</div>}
          {error && <p role="alert" className="mt-3 text-xs text-destructive">{error}</p>}
          <div className="mt-6 flex justify-end gap-2 border-t pt-4">
            <AlertDialog.Close render={<Button type="button" variant="outline" disabled={busy} />}>Cancel</AlertDialog.Close>
            <Button type="button" variant="destructive" disabled={busy} onClick={() => void confirm()}>{busy ? "Working…" : confirmLabel}</Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Viewport>
    </AlertDialog.Portal>
  </AlertDialog.Root>
}
