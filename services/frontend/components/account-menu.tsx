"use client"

import { useTheme } from "next-themes"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowDown01Icon, Logout01Icon } from "@hugeicons/core-free-icons"
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { themeOptions } from "@/components/theme-picker"
import type { User } from "@/lib/types"

export function AccountMenu({ user, onSignOut, compact = false }: { user: User; onSignOut: () => Promise<void>; compact?: boolean }) {
  const { theme, setTheme } = useTheme()
  const name = user.displayName || user.email
  return <DropdownMenu>
    <DropdownMenuTrigger aria-label="Account menu" className={compact
      ? "grid size-9 shrink-0 place-items-center rounded-full border bg-card text-xs font-semibold uppercase outline-none focus-visible:ring-2 focus-visible:ring-ring"
      : "mx-3 mb-3 flex shrink-0 items-center gap-2 rounded-2xl bg-muted/50 p-2.5 text-left outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"}>
      {compact ? name.slice(0, 2) : <>
        <span className="grid size-9 shrink-0 place-items-center rounded-full border border-border/60 bg-background text-xs font-semibold uppercase">{name.slice(0, 2)}</span>
        <span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{name}</span><span className="mt-0.5 block truncate text-xs text-muted-foreground">{user.isAdmin ? "Administrator" : user.email}</span></span>
        <HugeiconsIcon icon={ArrowDown01Icon} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      </>}
    </DropdownMenuTrigger>
    <DropdownMenuContent side={compact ? "bottom" : "top"} align={compact ? "end" : "start"} sideOffset={8} className="min-w-56 rounded-xl p-1.5">
      <div className="max-w-64 px-2 py-2"><p className="truncate text-sm font-medium">{name}</p><p className="mt-0.5 truncate text-sm text-muted-foreground" title={user.email}>{user.email}</p></div>
      <DropdownMenuSeparator />
      <DropdownMenuGroup>
        <DropdownMenuLabel>Appearance</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme ?? "system"} onValueChange={setTheme} aria-label="Appearance">
          {themeOptions.map(option => <DropdownMenuRadioItem key={option.value} value={option.value} className="gap-2 py-2"><HugeiconsIcon icon={option.icon} className="size-4" aria-hidden="true" />{option.label}</DropdownMenuRadioItem>)}
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuItem onClick={() => void onSignOut()} className="gap-2 py-2"><HugeiconsIcon icon={Logout01Icon} className="size-4" aria-hidden="true" />Sign out</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
}
