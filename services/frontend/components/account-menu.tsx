"use client"

import { useRouter } from "next/navigation"
import { useTheme } from "next-themes"
import { HugeiconsIcon } from "@hugeicons/react"
import { ArrowDown01Icon, Logout01Icon } from "@hugeicons/core-free-icons"
import { Avatar } from "@/components/panelui/avatar"
import { Menu } from "@/components/panelui/menu"
import { themeOptions } from "@/components/theme-picker"
import type { User } from "@/lib/types"

export function AccountMenu({ user, onSignOut, compact = false, collapsed = false }: { user: User; onSignOut: () => Promise<void>; compact?: boolean; collapsed?: boolean }) {
  const { theme, setTheme } = useTheme()
  const router = useRouter()
  const name = user.displayName || user.email
  return <Menu>
    <Menu.Trigger aria-label={collapsed ? `Account menu · ${name}` : "Account menu"} title={collapsed ? name : undefined} className={compact || collapsed
      ? `inline-flex shrink-0 rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring ${collapsed ? "mx-auto" : ""}`
      : "flex w-full shrink-0 items-center gap-2 rounded-2xl bg-muted/50 p-2.5 text-left outline-none transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"}>
      {compact || collapsed ? <Avatar name={name} size="md" color="muted" aria-hidden="true" /> : <>
        <Avatar name={name} size="md" color="muted" aria-hidden="true" />
        <span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{name}</span><span className="mt-0.5 block truncate text-xs text-muted-foreground">{user.isAdmin ? "Administrator" : user.email}</span></span>
        <HugeiconsIcon icon={ArrowDown01Icon} className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      </>}
    </Menu.Trigger>
    <Menu.Content side={compact ? "bottom" : collapsed ? "right" : "top"} align={compact ? "end" : collapsed ? "end" : "start"} sideOffset={8} className="min-w-56 rounded-xl p-1.5">
      <div className="max-w-64 px-2 py-2"><p className="truncate text-sm font-medium">{name}</p><p className="mt-0.5 truncate text-sm text-muted-foreground" title={user.email}>{user.email}</p></div>
      <Menu.Separator />
      <Menu.Group>
        <Menu.GroupLabel>Appearance</Menu.GroupLabel>
        <Menu.RadioGroup value={theme ?? "system"} onValueChange={setTheme} aria-label="Appearance">
          {themeOptions.map(option => <Menu.RadioItem key={option.value} value={option.value} className="gap-2 py-2"><HugeiconsIcon icon={option.icon} className="size-4" aria-hidden="true" />{option.label}</Menu.RadioItem>)}
        </Menu.RadioGroup>
      </Menu.Group>
      <Menu.Separator />
      <Menu.Item onClick={() => router.push("/settings/security")} className="gap-2 py-2">Account security</Menu.Item>
      <Menu.Item onClick={() => router.push("/settings/mcp")} className="gap-2 py-2">MCP access</Menu.Item>
      <Menu.Item onClick={() => void onSignOut()} className="gap-2 py-2"><HugeiconsIcon icon={Logout01Icon} className="size-4" aria-hidden="true" />Sign out</Menu.Item>
    </Menu.Content>
  </Menu>
}
