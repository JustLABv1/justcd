"use client"

import { useState, type ComponentProps, type ReactNode } from "react"
import { AlertDialog } from "@/components/panelui/alert-dialog"
import { Button } from "@/components/panelui/button"
import { useToast } from "@/components/toast-provider"
import { errorMessage } from "@/lib/api"

export function ConfirmDisclosure({ trigger, title, description, confirmLabel, onConfirm, children, disabled = false, confirmDisabled = false, triggerVariant = "destructive", confirmVariant = "destructive", open: controlledOpen, onOpenChange, busyLabel = "Working…" }: {
  trigger?: ReactNode
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => Promise<void>
  children?: ReactNode
  disabled?: boolean
  confirmDisabled?: boolean
  triggerVariant?: ComponentProps<typeof Button>["variant"]
  confirmVariant?: ComponentProps<typeof Button>["variant"]
  open?: boolean
  onOpenChange?: (open: boolean) => void
  /** Text shown on the confirm button while `onConfirm` is running. */
  busyLabel?: string
}) {
  const toast = useToast()
  const [localOpen, setLocalOpen] = useState(false)
  const open = controlledOpen ?? localOpen
  const setOpen = onOpenChange ?? setLocalOpen
  const [busy, setBusy] = useState(false)
  async function confirm() {
    setBusy(true)
    try { await onConfirm() }
    catch (cause) { toast.error(errorMessage(cause), cause); throw cause }
    finally { setBusy(false) }
  }
  const destructive = confirmVariant === "destructive"
  return <AlertDialog open={open} onOpenChange={setOpen}>
    {trigger && <AlertDialog.Trigger render={<Button type="button" variant={triggerVariant} size="sm" disabled={disabled} />}>{trigger}</AlertDialog.Trigger>}
    <AlertDialog.Content tone={destructive ? "destructive" : "neutral"}>
      <AlertDialog.Title>{title}</AlertDialog.Title>
      <AlertDialog.Description>{description}</AlertDialog.Description>
      {children && <div className="mt-3 text-left">{children}</div>}
      <AlertDialog.Footer>
        <AlertDialog.Cancel />
        <AlertDialog.Action disabled={confirmDisabled} onClick={confirm}>{busy ? busyLabel : confirmLabel}</AlertDialog.Action>
      </AlertDialog.Footer>
    </AlertDialog.Content>
  </AlertDialog>
}
