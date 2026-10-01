# Frontend component policy

Use ReUI or ShadCN components wherever an appropriate component exists.
This policy applies to new UI and to UI controls touched by a change.

- First inspect and reuse the installed primitives in `services/frontend/components/ui` and existing application wrappers such as `FormSelect`, `FormField`, and `ConnectionDialog`.
- Use the shared `Checkbox`, `Switch`, `Button`, `Input`, `Textarea`, `Select`/`FormSelect`, dialog, menu, tooltip, and other primitives instead of native browser controls or hand-built equivalents. Do not introduce raw `<input type="checkbox">`, native `<select>`, or custom-styled interactive controls when a shared component is available.
- For richer patterns, check ReUI for a suitable component or example before building one from scratch. Reuse its structure and documented API. Check `components.json` for this project's Base UI/ShadCN style before installing a missing component; do not mix incompatible Radix and Base UI APIs.
- Native HTML remains appropriate for semantic structure and for capabilities the component libraries do not provide. If a custom interactive control is necessary, explain the specific gap in the change description.
- Preserve semantic theme tokens and established variants: destructive actions use the destructive variant, while primary and secondary actions have clear hierarchy.
- Keep labels, keyboard interaction, focus states, disabled states, and responsive alignment accessible. Verify touched controls in the collaborative browser, and run frontend typecheck and relevant lint checks.

Read the more specific instructions in `services/frontend/AGENTS.md` before editing frontend code.
