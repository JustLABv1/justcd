"use client"

import { HugeiconsIcon } from "@hugeicons/react"
import { Copy01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/panelui/button"
import { useToast } from "@/components/toast-provider"
import { cn } from "@/lib/utils"

/** Error text as terminal output: monospace, one sentence or path per line, with a copy button. */
export function ErrorOutput({ message, label = "Error output", className }: { message: string; label?: string; className?: string }) {
  const toast = useToast()
  // Server errors run sentences together. Break after a full stop that starts a new sentence or a field path.
  const lines = message.split(/(?<=\.)\s+(?=[A-Z.])/)
  async function copy() {
    try {
      await navigator.clipboard.writeText(message)
      toast.success("Copied to clipboard.")
    } catch {
      toast.error("Could not copy. Select the text instead.")
    }
  }
  return (
    <figure role="alert" aria-label={label} className={cn("overflow-hidden rounded-xl border border-destructive/30 bg-destructive/5", className)}>
      <figcaption className="flex items-center justify-between gap-3 border-b border-destructive/20 bg-destructive/10 py-1.5 ps-3 pe-1.5">
        <span className="flex items-center gap-2 text-xs font-medium text-destructive">
          <span aria-hidden="true" className="size-1.5 rounded-full bg-destructive" />
          {label}
        </span>
        <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground" onClick={() => void copy()}>
          <HugeiconsIcon icon={Copy01Icon} strokeWidth={1.8} data-icon="inline-start" aria-hidden="true" />
          Copy
        </Button>
      </figcaption>
      <pre className="max-h-72 overflow-auto px-3.5 py-3 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-foreground select-text">
        {lines.map((line, index) => <span key={index} className={cn("block", index === 0 && "font-semibold text-destructive")}>{line}</span>)}
      </pre>
    </figure>
  )
}
