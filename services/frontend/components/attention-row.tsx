"use client"

import type { ReactNode } from "react"
import { HugeiconsIcon, type IconSvgElement } from "@hugeicons/react"
import { Frame } from "@/components/panelui/frame"

export type AttentionTone = "destructive" | "warning" | "info"

const toneClass: Record<AttentionTone, string> = {
  destructive: "bg-destructive/15 text-destructive",
  warning: "bg-warning/15 text-warning",
  info: "bg-info/15 text-info",
}

/**
 * One row of a "Needs attention" digest (design system §4): tinted icon, title, muted
 * detail, actions on the right. Used by the dashboard and the application page alike.
 */
export function AttentionRow({ tone, icon, title, detail, children, actions, alert = false }: {
  tone: AttentionTone
  icon: IconSvgElement
  title: ReactNode
  /** Preformatted tool output (renderer errors); shown in a scrollable block. */
  detail?: string
  children?: ReactNode
  actions?: ReactNode
  alert?: boolean
}) {
  return <Frame.Row role={alert ? "alert" : "status"} className="flex-wrap items-start gap-x-4 gap-y-3">
    <span aria-hidden="true" className={`grid size-8 shrink-0 place-items-center rounded-full ${toneClass[tone]}`}><HugeiconsIcon icon={icon} strokeWidth={2} className="size-4" /></span>
    <div className="min-w-0 flex-1 basis-72 space-y-0.5">
      <div className="text-sm font-medium">{title}</div>
      {detail && <pre className="mt-2 max-h-48 overflow-auto rounded-md border bg-muted/40 p-2 text-xs leading-5 whitespace-pre-wrap break-words" aria-label="Renderer output">{detail}</pre>}
      {children}
    </div>
    {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
  </Frame.Row>
}
