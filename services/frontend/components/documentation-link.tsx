import { HugeiconsIcon } from "@hugeicons/react"
import { BookOpen01Icon } from "@hugeicons/core-free-icons"
import { buttonVariants } from "@/components/panelui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/panelui/tooltip"
import styles from "./app-shell.module.css"

export function DocumentationLink({ compact = false, collapsed = false }: { compact?: boolean; collapsed?: boolean }) {
  if (collapsed) return (
    <Tooltip>
      <TooltipTrigger render={<a href="/docs" target="_blank" rel="noopener noreferrer" aria-label="Documentation (opens in a new tab)" className={`${styles.navItem} ${styles.navItemCollapsed}`} />}>
        <HugeiconsIcon icon={BookOpen01Icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
      </TooltipTrigger>
      <TooltipContent side="right">Documentation</TooltipContent>
    </Tooltip>
  )
  return (
    <a href="/docs" target="_blank" rel="noopener noreferrer" className={compact ? buttonVariants({ variant: "ghost", size: "sm", className: "shrink-0" }) : styles.navItem}>
      <HugeiconsIcon icon={BookOpen01Icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
      {compact ? "Docs" : "Documentation"}
      <span className="sr-only"> (opens in a new tab)</span>
    </a>
  )
}
