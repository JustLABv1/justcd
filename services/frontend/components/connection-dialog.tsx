"use client"

import { AppDialog } from "@/components/ui/dialog"

export function ConnectionDialog({ open, onOpenChange, title, description, busy = false, size = "lg", children }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  busy?: boolean
  size?: "sm" | "md" | "lg" | "xl"
  children: React.ReactNode
}) {
  return <AppDialog open={open} onOpenChange={onOpenChange} title={title} description={description} busy={busy} size={size}>{children}</AppDialog>
}
