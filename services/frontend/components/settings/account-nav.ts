import type { SettingsNavGroup } from "@/components/settings/layout"

export const accountSettingsNav: SettingsNavGroup[] = [{
  label: "Account",
  items: [
    { id: "security", label: "Security", href: "/settings/security" },
    { id: "mcp", label: "MCP access", href: "/settings/mcp" },
  ],
}]
