import { HugeiconsIcon } from "@hugeicons/react"
import { BookOpen01Icon } from "@hugeicons/core-free-icons"
import { buttonVariants } from "@/components/ui/button"
import styles from "./app-shell.module.css"

export function DocumentationLink({ compact = false }: { compact?: boolean }) {
  return (
    <a href="/docs" target="_blank" rel="noopener noreferrer" className={compact ? buttonVariants({ variant: "ghost", size: "sm", className: "shrink-0" }) : styles.navItem}>
      <HugeiconsIcon icon={BookOpen01Icon} strokeWidth={1.8} className="size-4 shrink-0" aria-hidden="true" />
      {compact ? "Docs" : "Documentation"}
      <span className="sr-only"> (opens in a new tab)</span>
    </a>
  )
}
