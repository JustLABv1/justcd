"use client"

import { useState } from "react"
import { ApprovalRuleEditor } from "@/components/approval-rule-editor"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { DeploymentSchedulePanel } from "@/components/deployment-schedule"
import { Badge } from "@/components/reui/badge"
import { SettingsGroup, SettingsRow, SettingsSection } from "@/components/settings/settings-section"
import { useToast } from "@/components/toast-provider"
import { Button } from "@/components/ui/button"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField } from "@/components/ui-kit"
import { api, errorMessage } from "@/lib/api"
import type { ApprovalPolicy, ApprovalRule, Workspace, WorkspaceMember } from "@/lib/types"

type ApprovalKind = "sync" | "deletion"

function approversSummary(rule: ApprovalRule) {
  const roles = rule.approverRoles ?? []
  const roleSummary = roles.includes("viewer") ? "All workspace roles" : roles.includes("deployer") ? "Deployers and owners" : roles.includes("owner") ? "Owners" : ""
  const count = rule.approverUserIds?.length ?? 0
  return [roleSummary, count ? `${count} specific member${count === 1 ? "" : "s"}` : ""].filter(Boolean).join(" · ") || "No eligible approvers configured"
}

export function WorkspaceSettings({ workspace, members, onUpdated, onDelete }: {
  workspace: Workspace
  members: WorkspaceMember[]
  onUpdated: (workspace: Workspace) => void
  onDelete: (policy: string) => Promise<void>
}) {
  const toast = useToast()
  const [profileOpen, setProfileOpen] = useState(false)
  const [name, setName] = useState(workspace.name)
  const [description, setDescription] = useState(workspace.description)
  const [profileBusy, setProfileBusy] = useState(false)
  const [approvalKind, setApprovalKind] = useState<ApprovalKind | null>(null)
  const [approvalRule, setApprovalRule] = useState<ApprovalRule>(workspace.approvalPolicy.sync)
  const [approvalBusy, setApprovalBusy] = useState(false)
  const [editorError, setEditorError] = useState<string | null>(null)
  const [deletePolicy, setDeletePolicy] = useState("keep")

  function editProfile() {
    setName(workspace.name); setDescription(workspace.description); setEditorError(null); setProfileOpen(true)
  }
  function editApprovals(kind: ApprovalKind) {
    setApprovalRule(workspace.approvalPolicy[kind]); setEditorError(null); setApprovalKind(kind)
  }
  async function saveProfile(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setProfileBusy(true); setEditorError(null)
    try {
      await api(`/api/v1/workspaces/${encodeURIComponent(workspace.id)}`, { method: "PUT", body: JSON.stringify({ name, description }) })
      onUpdated({ ...workspace, name: name.trim(), description: description.trim() })
      setProfileOpen(false); toast.success("Workspace details saved.")
    } catch (cause) { setEditorError(errorMessage(cause)) }
    finally { setProfileBusy(false) }
  }
  async function saveApprovals(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!approvalKind) return
    setApprovalBusy(true); setEditorError(null)
    try {
      const result = await api<{ approvalPolicy: ApprovalPolicy }>(`/api/v1/workspaces/${encodeURIComponent(workspace.id)}/approval-policy`, {
        method: "PUT", body: JSON.stringify({ ...workspace.approvalPolicy, [approvalKind]: approvalRule }),
      })
      onUpdated({ ...workspace, approvalPolicy: result.approvalPolicy })
      setApprovalKind(null); toast.success("Approval rule saved. Existing plans need a fresh review.")
    } catch (cause) { setEditorError(errorMessage(cause)) }
    finally { setApprovalBusy(false) }
  }

  return <div className="max-w-5xl space-y-7 pb-6">
    <SettingsSection title="General" description="How this workspace appears to your team.">
      <SettingsGroup>
        <SettingsRow label="Name" value={<span className="font-medium break-words">{workspace.name}</span>} action={<Button variant="outline" size="sm" onClick={editProfile}>Edit details</Button>} />
        <SettingsRow label="Description" value={workspace.description ? <span className="break-words">{workspace.description}</span> : <span className="text-muted-foreground">No description added</span>} />
      </SettingsGroup>
    </SettingsSection>

    <SettingsSection title="Approval rules" description="Defaults for deployment plans. Applications can override these rules.">
      <SettingsGroup>
        {(["sync", "deletion"] as const).map((kind) => {
          const rule = workspace.approvalPolicy[kind]
          const required = rule.requiredApprovals
          const label = kind === "sync" ? "Deployments" : "Application deletion"
          return <SettingsRow key={kind} label={label}
            description={required === 0 ? "Changes can deploy without manual approval." : approversSummary(rule)}
            value={<Badge variant="secondary" radius="full">{required === 0 ? "No approval required" : `${required} approval${required === 1 ? "" : "s"} required`}</Badge>}
            action={<Button variant="outline" size="sm" aria-label={`Edit ${label.toLowerCase()} approval rule`} onClick={() => editApprovals(kind)}>Edit rule</Button>} />
        })}
      </SettingsGroup>
    </SettingsSection>

    <SettingsSection title="Deployment schedule" description="Control when deployments may start. Planning and review are always available.">
      <DeploymentSchedulePanel scope="workspaces" id={workspace.id} canEdit layout="settings" />
    </SettingsSection>

    <SettingsSection title="Delete workspace" description="Permanently remove this workspace from JustCD.">
      <SettingsGroup>
        <SettingsRow label="Delete this workspace" description="Removes applications and workspace connections. This cannot be undone." action={
          <ConfirmDisclosure trigger="Delete workspace" title={`Delete ${workspace.name}?`} description="Choose what happens to resources managed by applications in this workspace. This cannot be undone in JustCD." confirmLabel="Delete workspace" onConfirm={() => onDelete(deletePolicy)}>
            <FormField label="Managed cluster resources" htmlFor="workspace-delete-policy"><FormSelect id="workspace-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through reviewed plans" }]} /></FormField>
            {deletePolicy === "delete" && <p className="mt-2 text-sm text-muted-foreground">JustCD prepares deletion plans for each application with managed resources. The workspace is removed only after all plans have been approved, applied, and the inventory is empty.</p>}
          </ConfirmDisclosure>
        } />
      </SettingsGroup>
    </SettingsSection>

    <AppDialog open={profileOpen} onOpenChange={setProfileOpen} busy={profileBusy} size="md" title="Edit workspace details" description="Give your workspace a recognizable name and a short description.">
      <form className="space-y-4" onSubmit={saveProfile}>
        <FormField label="Workspace name" htmlFor="edit-workspace-name"><Input id="edit-workspace-name" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} disabled={profileBusy} /></FormField>
        <FormField label="Description" htmlFor="edit-workspace-description"><Textarea id="edit-workspace-description" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} disabled={profileBusy} /></FormField>
        {editorError && <p role="alert" className="text-sm text-destructive">{editorError}</p>}
        <DialogFooter><DialogCancel disabled={profileBusy} /><Button type="submit" loading={profileBusy} loadingText="Saving…">Save details</Button></DialogFooter>
      </form>
    </AppDialog>

    <AppDialog open={approvalKind !== null} onOpenChange={(open) => { if (!open) setApprovalKind(null) }} busy={approvalBusy} size="lg" title={approvalKind === "deletion" ? "Application deletion approvals" : "Deployment approvals"} description="Choose the required approvals and who can give them. Saving this rule invalidates existing plans.">
      {approvalKind && <form className="space-y-5" onSubmit={saveApprovals}>
        <ApprovalRuleEditor key={approvalKind} id={`workspace-${approvalKind}-approvals`} title={approvalKind === "deletion" ? "Application deletion" : "Deployments"} rule={approvalRule} members={members} deletion={approvalKind === "deletion"} disabled={approvalBusy} surface="flat" onChange={setApprovalRule} />
        {editorError && <p role="alert" className="text-sm text-destructive">{editorError}</p>}
        <DialogFooter><DialogCancel disabled={approvalBusy} /><Button type="submit" loading={approvalBusy} loadingText="Saving…">Save approval rule</Button></DialogFooter>
      </form>}
    </AppDialog>
  </div>
}
