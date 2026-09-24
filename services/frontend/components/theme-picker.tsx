"use client"

import { useSyncExternalStore } from "react"
import { useTheme } from "next-themes"
import { HugeiconsIcon } from "@hugeicons/react"
import { Moon01Icon, Sun01Icon, SunMoonIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

export function ThemePicker() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const mounted = useSyncExternalStore(() => () => {}, () => true, () => false)
  const icon = !mounted || theme === "system" ? SunMoonIcon : resolvedTheme === "dark" ? Moon01Icon : Sun01Icon
  return <DropdownMenu>
    <DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label="Choose appearance" title="Appearance" />}>
      <HugeiconsIcon icon={icon} className="size-4" aria-hidden="true" />
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="min-w-36">
      <DropdownMenuItem onClick={() => setTheme("light")}>Light {mounted && theme === "light" ? "✓" : ""}</DropdownMenuItem>
      <DropdownMenuItem onClick={() => setTheme("dark")}>Dark {mounted && theme === "dark" ? "✓" : ""}</DropdownMenuItem>
      <DropdownMenuItem onClick={() => setTheme("system")}>System {mounted && theme === "system" ? "✓" : ""}</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
}
