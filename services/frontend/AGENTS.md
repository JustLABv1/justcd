<!-- BEGIN:nextjs-agent-rules -->
# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->

# UI components

Follow the repository's ReUI/ShadCN component policy in `../../AGENTS.md`.
Reuse installed components from `@/components/ui` and the existing form/dialog
wrappers before adding a primitive or implementing a custom control. In
particular, checkboxes must use `@/components/ui/checkbox`; selects must use
`FormSelect` or the shared Select primitives, rather than native browser inputs.

# Settings pages

Every settings surface (instance settings, workspace settings, workspace connections, account
security, the application Settings tab) uses one layout. Do not invent a new one.

- **Shell:** `SettingsLayout` from `@/components/settings/layout`. It renders the page heading, a
  scope badge (`ScopeBadge`: whole instance, workspace, account) instead of "these settings apply
  to…" banners, a sticky left navigation (`nav` groups with `href` items) and a content column.
  The nav disappears when an area has a single section. Put the page's primary action in `actions`.
- **Sections:** `SettingsSection` (heading above one card) with `SettingsGroup` + `SettingsRow` for
  label/control rows; `SettingsPanel` when the card holds a list, form or table. Headings always sit
  above the card, never inside it. Use `SettingsPanel` in settings code, not the generic `Panel`.
- **Destructive actions:** one `SettingsDangerZone` at the end of the page, with the destructive
  button variant. Do not hand-style red cards.
- **Notices:** `SettingsNotice` for the rare inline info or warning. Prefer the scope badge.
- **Navigation:** sub-sections are routes or `?tab=` links in the left nav, not horizontal tabs.
  A landing page that only lists sections is redundant; redirect to the first section.
