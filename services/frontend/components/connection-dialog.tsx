"use client"

import { Dialog } from "@base-ui/react/dialog"
import { Button } from "@/components/ui/button"

export function ConnectionDialog({ open, onOpenChange, title, description, busy = false, children }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  busy?: boolean
  children: React.ReactNode
}) {
  return <Dialog.Root open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }}>
    <Dialog.Portal>
      <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/55 backdrop-blur-[2px] data-open:animate-in data-open:fade-in-0 motion-reduce:animate-none" />
      <Dialog.Popup className="fixed left-1/2 top-1/2 z-50 max-h-[min(88dvh,900px)] w-[calc(100%-2rem)] max-w-2xl -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border bg-card p-5 text-card-foreground shadow-2xl outline-none data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 motion-reduce:animate-none sm:p-6">
        <div className="flex items-start justify-between gap-4 border-b pb-4">
          <div>
            <Dialog.Title className="text-base font-semibold">{title}</Dialog.Title>
            <Dialog.Description className="mt-1 text-sm leading-6 text-muted-foreground">{description}</Dialog.Description>
          </div>
          <Dialog.Close render={<Button type="button" size="sm" variant="ghost" disabled={busy} />}>Close</Dialog.Close>
        </div>
        <div className="pt-5">{children}</div>
      </Dialog.Popup>
    </Dialog.Portal>
  </Dialog.Root>
}
