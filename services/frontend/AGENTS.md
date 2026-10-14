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
