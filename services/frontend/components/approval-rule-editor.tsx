"use client"

import { useState } from "react"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { FormField } from "@/components/ui-kit"
import type { ApprovalRule, WorkspaceMember } from "@/lib/types"

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
  members: WorkspaceMember[]
  deletion?: boolean
  disabled?: boolean
  onChange: (rule: ApprovalRule) => void
}) {
  const minimum = deletion ? 1 : 0
  const configuredUserIds = rule.approverUserIds ?? []
  const configuredRoles = rule.approverRoles ?? []
  const selectedUsers = new Set(configuredUserIds)
  const selectedRoles = new Set(configuredRoles)
  const [filter, setFilter] = useState("")
  const query = filter.trim().toLowerCase()
  const visibleMembers = members.filter((member) => !query || `${member.displayName ?? ""} ${member.email}`.toLowerCase().includes(query))

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
      <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_120px] sm:items-center">
        <div>
          <h3 className="text-sm font-semibold">{title}</h3>
          <p className="mt-1 text-sm leading-5 text-muted-foreground">
            {deletion ? "Application deletion always requires approval." : "Set to 0 to allow ordinary syncs without approval."}
          </p>
        </div>
        <FormField label="Required approvals" htmlFor={`${id}-count`}>
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
        </FormField>
      </div>

      <fieldset className="mt-4 space-y-2" disabled={disabled}>
        <legend className="mb-2 text-sm font-medium">Who can approve?</legend>
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          {roles.map((role) => (
            <label key={role.value} className="flex items-center gap-2 text-sm">
              <Checkbox checked={selectedRoles.has(role.value)} disabled={disabled} onCheckedChange={(checked) => toggleRole(role.value, Boolean(checked))} />
              {role.label}
            </label>
          ))}
        </div>
        <p className="text-sm leading-5 text-muted-foreground">A higher workspace role also qualifies. Selected members qualify regardless of role.</p>
        {members.length > 0 ? (
          <div className="mt-3 space-y-3 border-t pt-3">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div className="min-w-48 flex-1">
                <FormField label="Specific approvers" htmlFor={`${id}-filter`}>
                  <Input id={`${id}-filter`} type="search" placeholder="Filter members by name or email" value={filter} onChange={(event) => setFilter(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") event.preventDefault() }} />
                </FormField>
              </div>
              <p role="status" className="pb-2 text-sm text-muted-foreground">{configuredUserIds.length} selected</p>
            </div>
            {visibleMembers.length > 0 ? <div className="grid max-h-56 gap-2 overflow-y-auto sm:grid-cols-2">
              {visibleMembers.map((member) => (
                <label key={member.id} className="flex min-w-0 items-center gap-2 text-sm">
                  <Checkbox checked={selectedUsers.has(member.id)} disabled={disabled || (member.disabled && !selectedUsers.has(member.id))} onCheckedChange={(checked) => toggleMember(member.id, Boolean(checked))} />
                  <span className="min-w-0 truncate">{member.displayName || member.email}</span>
                  <span className="shrink-0 text-sm capitalize text-muted-foreground">{member.disabled ? "locked" : member.role}</span>
                </label>
              ))}
            </div> : <p className="text-sm text-muted-foreground">No members match “{filter.trim()}”.</p>}
          </div>
        ) : (
          <p className="mt-3 border-t pt-3 text-sm text-muted-foreground">Add workspace members to select individuals.</p>
        )}
      </fieldset>
    </section>
  )
}
