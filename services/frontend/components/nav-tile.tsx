import { HugeiconsIcon } from "@hugeicons/react"
import type { Home01Icon } from "@hugeicons/core-free-icons"
import { IconTile } from "@/components/reui/icon-tile"
import { cn } from "cn"

/** Category colours shared by the sidebar and the command palette so a colour means the same thing everywhere. */
export const tones = {
  blue: { solid: "bg-blue-500 text-white", soft: "text-blue-600 dark:text-blue-400" },
  violet: { solid: "bg-violet-500 text-white", soft: "text-violet-600 dark:text-violet-400" },
  green: { solid: "bg-emerald-500 text-white", soft: "text-emerald-600 dark:text-emerald-400" },
  amber: { solid: "bg-amber-500 text-white", soft: "text-amber-600 dark:text-amber-400" },
  rose: { solid: "bg-rose-500 text-white", soft: "text-rose-600 dark:text-rose-400" },
  slate: { solid: "bg-slate-600 text-white dark:bg-slate-500", soft: "text-slate-600 dark:text-slate-300" },
  teal: { solid: "bg-teal-500 text-white", soft: "text-teal-600 dark:text-teal-400" },
} as const

export type Tone = keyof typeof tones

/**
 * Coloured icon tile. `solid` is the full-colour tile used in the palette and for the current sidebar page;
 * `soft` is the quiet tinted version for inactive rows. Fills its parent unless `className` sizes it.
 */
export function NavTile({ icon, tone = "blue", variant = "solid", className }: { icon: typeof Home01Icon; tone?: Tone; variant?: "solid" | "soft"; className?: string }) {
  return <IconTile variant={variant} radius="default" className={cn("size-full rounded-lg [--icon-tile-icon-size:--spacing(4)]", variant === "solid" ? tones[tone].solid : tones[tone].soft, className)}>
    <HugeiconsIcon icon={icon} strokeWidth={1.8} aria-hidden="true" />
  </IconTile>
}
