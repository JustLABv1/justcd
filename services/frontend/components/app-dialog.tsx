"use client"

import { Children, cloneElement, isValidElement, type ReactElement, type ReactNode } from "react"
import { Dialog } from "@/components/panelui/dialog"
import { Button } from "@/components/panelui/button"

const sizes = { sm: "sm", md: "md", lg: "lg", xl: "lg" } as const

/**
 * Shared modal, drawn with the PanelUI Dialog. `form` dialogs close through the footer's Cancel
 * button (see DialogFooter); `info` dialogs have no footer and close through the header X.
 * A dialog never shows both, so there is exactly one obvious way out.
 */
function AppDialog({ open, onOpenChange, title, description, busy = false, variant = "form", size = "lg", children }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  busy?: boolean
  variant?: "form" | "info"
  size?: keyof typeof sizes
  children: ReactNode
}) {
  // Keep form ownership intact while the body scrolls independently of its actions.
  function layout(content: ReactNode): ReactNode {
    const parts = Children.toArray(content)
    const footers = parts.filter((part) => isValidElement(part) && part.type === DialogFooter)
    const body = parts.filter((part) => !isValidElement(part) || part.type !== DialogFooter)
    if (footers.length) return <><Dialog.Header title={title} description={description} /><Dialog.Body>{body}</Dialog.Body>{footers}</>
    if (parts.length === 1 && isValidElement<{ children?: ReactNode; className?: string }>(parts[0]) &&
        (parts[0].type === "form" || parts[0].type === "div")) {
      const wrapper = parts[0]
      const nested = Children.toArray(wrapper.props.children)
      if (nested.some((part) => isValidElement(part) && part.type === DialogFooter)) {
        return cloneElement(wrapper as ReactElement<{ className?: string; children?: ReactNode }>, {
          className: "flex min-h-0 flex-1 flex-col",
          children: layout(wrapper.props.children),
        })
      }
    }
    return <><Dialog.Header title={title} description={description} /><Dialog.Body>{content}</Dialog.Body></>
  }
  return <Dialog open={open} onOpenChange={(next) => { if (!busy) onOpenChange(next) }} dismissible={!busy}>
    <Dialog.Content variant="plain" size={sizes[size]} showClose={variant === "info" && !busy}>
      {layout(children)}
    </Dialog.Content>
  </Dialog>
}

/** Right-aligned `[Cancel] [Primary]` footer. Put it as the last child of the dialog form. */
function DialogFooter({ children, className }: { children: ReactNode; className?: string }) {
  return <Dialog.Footer className={`flex-wrap ${className ?? ""}`}>{children}</Dialog.Footer>
}

/** Cancel button for DialogFooter; must render inside an AppDialog. */
function DialogCancel({ disabled, children = "Cancel" }: { disabled?: boolean; children?: ReactNode }) {
  return <Dialog.Close render={<Button type="button" variant="outline" disabled={disabled} />}>{children}</Dialog.Close>
}

export { AppDialog, DialogFooter, DialogCancel }
