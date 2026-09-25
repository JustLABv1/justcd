"use client"

import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import type { ApprovalRule, ProjectMember } from "@/lib/types"

const roles = [
  { value: "owner", label: "Owners" },
  { value: "deployer", label: "Deployers" },
  { value: "viewer", label: "Viewers" },
]

export function ApprovalRuleEditor({
  id,
  title,
  rule,
  members,
  deletion = false,
  disabled = false,
  onChange,
}: {
  id: string
  title: string
  rule: ApprovalRule
  members: ProjectMember[]
  deletion?: boolean
  disabled?: boolean
  onChange: (rule: ApprovalRule) => void
}) {
  const minimum = deletion ? 1 : 0
  const configuredUserIds = rule.approverUserIds ?? []
  const configuredRoles = rule.approverRoles ?? []
  const selectedUsers = new Set(configuredUserIds)
  const selectedRoles = new Set(configuredRoles)

  function toggleRole(role: string, checked: boolean) {
    const nextRoles = checked
      ? [...configuredRoles, role]
      : configuredRoles.filter((item) => item !== role)
    onChange({ ...rule, approverRoles: nextRoles })
  }

  function toggleMember(userId: string, checked: boolean) {
    const nextUserIds = checked
      ? [...configuredUserIds, userId]
      : configuredUserIds.filter((item) => item !== userId)
    onChange({ ...rule, approverUserIds: nextUserIds })
  }

  return (
    <section className="rounded-lg border p-4">
      <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_120px] sm:items-end">
        <div>
          <h3 className="text-xs font-semibold">{title}</h3>
          <p className="mt-1 text-[11px] leading-4 text-muted-foreground">
            {deletion ? "Application deletion always requires approval." : "Set to 0 to allow ordinary syncs without approval."}
          </p>
        </div>
        <label htmlFor={`${id}-count`} className="space-y-1.5 text-xs font-medium">
          Required approvals
          <Input
            id={`${id}-count`}
            type="number"
            min={minimum}
            max={5}
            step={1}
            value={rule.requiredApprovals}
            disabled={disabled}
            onChange={(event) => {
              const value = Number(event.target.value)
              onChange({ ...rule, requiredApprovals: Number.isFinite(value) ? Math.max(minimum, Math.min(5, Math.trunc(value))) : minimum })
            }}
          />
        </label>
      </div>

      <fieldset className="mt-4 space-y-2" disabled={disabled}>
        <legend className="mb-2 text-[11px] font-medium">Who can approve?</legend>
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          {roles.map((role) => (
            <label key={role.value} className="flex items-center gap-2 text-xs">
              <Checkbox checked={selectedRoles.has(role.value)} disabled={disabled} onCheckedChange={(checked) => toggleRole(role.value, Boolean(checked))} />
              {role.label}
            </label>
          ))}
        </div>
        <p className="text-[10px] leading-4 text-muted-foreground">A higher project role also qualifies. Selected members qualify regardless of role.</p>
        {members.length > 0 ? (
          <div className="mt-3 grid gap-2 border-t pt-3 sm:grid-cols-2">
            {members.map((member) => (
              <label key={member.id} className="flex min-w-0 items-center gap-2 text-xs">
                <Checkbox checked={selectedUsers.has(member.id)} disabled={disabled || (member.disabled && !selectedUsers.has(member.id))} onCheckedChange={(checked) => toggleMember(member.id, Boolean(checked))} />
                <span className="min-w-0 truncate">{member.displayName || member.email}</span>
                <span className="shrink-0 text-[10px] capitalize text-muted-foreground">{member.disabled ? "locked" : member.role}</span>
              </label>
            ))}
          </div>
        ) : (
          <p className="mt-3 border-t pt-3 text-[11px] text-muted-foreground">Add project members to select individuals.</p>
        )}
      </fieldset>
    </section>
  )
}
