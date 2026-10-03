import {
  Alert02Icon,
  Cancel01Icon,
  CheckmarkCircle02Icon,
  HelpCircleIcon,
} from "@hugeicons/core-free-icons"
import type { HealthLevel } from "@/lib/connection-health"

export const tone: Record<HealthLevel, { text: string; bg: string; border: string; dot: string; icon: typeof Alert02Icon }> = {
  healthy: { text: "text-success-foreground dark:text-success", bg: "bg-success/10", border: "border-success/30", dot: "bg-success", icon: CheckmarkCircle02Icon },
  warning: { text: "text-warning-foreground dark:text-warning", bg: "bg-warning/10", border: "border-warning/30", dot: "bg-warning", icon: Alert02Icon },
  critical: { text: "text-destructive", bg: "bg-destructive/10", border: "border-destructive/30", dot: "bg-destructive", icon: Cancel01Icon },
  unknown: { text: "text-muted-foreground", bg: "bg-muted", border: "border-border", dot: "bg-muted-foreground/50", icon: HelpCircleIcon },
}
