"use client"

import { useSyncExternalStore } from "react"
import { useTheme } from "next-themes"
import { HugeiconsIcon } from "@hugeicons/react"
import { Moon01Icon, Sun01Icon, SunMoonIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

export const themeOptions = [
  { value: "light", label: "Light", icon: Sun01Icon },
  { value: "dark", label: "Dark", icon: Moon01Icon },
  { value: "system", label: "System", icon: SunMoonIcon },
]

export function ThemePicker() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const mounted = useSyncExternalStore(() => () => {}, () => true, () => false)
  const icon = !mounted || theme === "system" ? SunMoonIcon : resolvedTheme === "dark" ? Moon01Icon : Sun01Icon
  return <DropdownMenu>
    <DropdownMenuTrigger render={<Button variant="ghost" size="icon" aria-label="Choose appearance" />}>
      <HugeiconsIcon icon={icon} className="size-4" aria-hidden="true" />
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="min-w-40">
      <DropdownMenuGroup>
        <DropdownMenuLabel>Appearance</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={mounted ? theme ?? "system" : "system"} onValueChange={setTheme} aria-label="Appearance">
          {themeOptions.map((option) => <DropdownMenuRadioItem key={option.value} value={option.value} className="gap-2 py-2"><HugeiconsIcon icon={option.icon} className="size-4" aria-hidden="true" />{option.label}</DropdownMenuRadioItem>)}
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>
    </DropdownMenuContent>
  </DropdownMenu>
}
