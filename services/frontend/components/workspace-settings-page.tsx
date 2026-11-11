"use client"

import { AIAssistant } from "@/components/ai-assistant"
import { WorkspaceAIPolicy } from "@/components/settings/ai-panel"
import Link from "next/link"
import { useParams, useRouter, useSearchParams } from "next/navigation"
import { Suspense, useEffect, useState } from "react"
import { SectionSkeleton } from "@/components/settings/shared"
import { HugeiconsIcon } from "@hugeicons/react"
import { Audit01Icon, Delete02Icon, UserAdd01Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { Avatar } from "@/components/panelui/avatar"
import { Alert } from "@/components/panelui/alert"
import { Badge } from "@/components/panelui/badge"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/app-dialog"
import { Button } from "@/components/panelui/button"
import { FormSelect } from "@/components/form-select"
import { Input } from "@/components/panelui/input"
import { PublishingProfiles } from "@/components/settings/publishing-profiles"
import { WorkspaceSettings } from "@/components/workspace-settings"
import { ErrorNotice } from "@/components/workspace-ui"
import { EmptyState, FormField } from "@/components/ui-kit"
import { SettingsLayout, SettingsNotice, type SettingsNavGroup } from "@/components/settings/layout"
import { SettingsPanel } from "@/components/settings/settings-section"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { ListResponse, Workspace, WorkspaceMember } from "@/lib/types"

const settingsTabs = ["general", "members", "policies", "notifications", "ai", "publishing"]

/**
 * Workspace settings. Each section has its own path, /workspaces/:id/settings/:section, so it can be
 * linked, bookmarked and shown in the breadcrumb. Called without a section (the bare workspace URL,
 * old ?tab= bookmarks) it forwards to the right one.
 */
export function WorkspaceSettingsPage({ section }: { section?: string }) {
  return <Suspense fallback={<SectionSkeleton label="Loading workspace settings" />}><WorkspaceSettingsContent section={section} /></Suspense>
}

function WorkspaceSettingsContent({ section }: { section?: string }) {
  const { workspaceID } = useParams<{ workspaceID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [error, setError] = useState<unknown | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadedWorkspaceId, setLoadedWorkspaceId] = useState("")
  const [memberEmail, setMemberEmail] = useState("")
  const [memberRole, setMemberRole] = useState("viewer")
  const [memberBusy, setMemberBusy] = useState(false)
  const [memberOpen, setMemberOpen] = useState(false)
  const [memberActionId, setMemberActionId] = useState<string | null>(null)
  const [deletionPlans, setDeletionPlans] = useState<{ applicationId: string; applicationName: string; managedResources: number }[]>([])
  const searchParams = useSearchParams()
  const legacyTab = searchParams.get("tab")
  const requestedTab = section ?? legacyTab
  const isOwner = workspace?.role === "owner"
  const activeTab = isOwner
    ? (settingsTabs.includes(requestedTab ?? "") ? requestedTab! : "general")
    : requestedTab === "ai" ? "ai" : "members"

  // Forward the bare workspace URL and old ?tab= bookmarks to their own path.
  useEffect(() => {
    if (section || !workspace || loadedWorkspaceId !== workspaceID) return
    if (legacyTab === "connections") { router.replace(workspace.role === "viewer" ? `/workspaces/${workspaceID}/settings/members` : `/workspaces/${workspaceID}/connections/git-sources`); return }
    const target = workspace.role === "owner" ? (settingsTabs.includes(legacyTab ?? "") ? legacyTab! : "general") : legacyTab === "ai" ? "ai" : "members"
    router.replace(`/workspaces/${workspaceID}/settings/${target}`)
  }, [section, legacyTab, workspace, workspaceID, loadedWorkspaceId, router])

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const [workspaces, workspaceMembers] = await Promise.all([
          api<ListResponse<Workspace>>("/api/v1/workspaces"),
          api<ListResponse<WorkspaceMember>>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/members`),
        ])
        if (!active) return
        const selected = workspaces.items.find((item) => item.id === workspaceID) ?? null
        setWorkspace(selected)
        setMembers(workspaceMembers.items)
        setError(null)
      } catch (cause) {
        if (active) { setWorkspace(null); setError(cause) }
      } finally { if (active) { setLoadedWorkspaceId(workspaceID); setLoading(false) } }
    }
    void load()
    return () => { active = false }
  }, [workspaceID])

  async function addMember(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMemberBusy(true)
    try {
      await apiPost(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/members`, { email: memberEmail, role: memberRole })
      const result = await api<{ items: typeof members }>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/members`)
      setMembers(result.items); setMemberEmail(""); setMemberOpen(false)
      toast.success("Workspace member added.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setMemberBusy(false) }
  }

  async function changeMemberRole(member: WorkspaceMember, role: string) {
    setMemberActionId(member.id)
    try {
      const result = await api<{ role: string }>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/members/${encodeURIComponent(member.id)}`, { method: "PUT", body: JSON.stringify({ role }) })
      setMembers((items) => items.map((item) => item.id === member.id ? { ...item, role: result.role } : item))
      toast.success(`Updated ${member.displayName || member.email}'s workspace role.`)
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setMemberActionId(null) }
  }

  async function removeMember(member: WorkspaceMember) {
    await api<void>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/members/${encodeURIComponent(member.id)}`, { method: "DELETE" })
    setMembers((items) => items.filter((item) => item.id !== member.id))
    toast.success(`Removed ${member.displayName || member.email} from the workspace.`)
  }

  async function deleteWorkspace(deletePolicy: string) {
    const result = await api<{ deleted: boolean; plans?: typeof deletionPlans }>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}?resources=${deletePolicy}`, { method: "DELETE" })
    if (result.deleted) { toast.success("Workspace deleted."); router.push("/workspaces"); router.refresh(); return }
    setDeletionPlans(result.plans ?? [])
    setError(null)
  }

  if (loading || loadedWorkspaceId !== workspaceID || (!section && Boolean(workspace))) return <SectionSkeleton label="Loading workspace settings" />
  if (error) return <ErrorNotice error={error} />
  if (!workspace) return <EmptyState title="Workspace unavailable" description="Choose a workspace you have access to." href="/workspaces" action="View workspaces" />

  const roleItems = [{ value: "viewer", label: "Viewer" }, { value: "deployer", label: "Deployer" }, { value: "owner", label: "Owner" }]
  const navGroups: SettingsNavGroup[] = [
    { label: "Workspace", items: [
      ...(isOwner ? [{ id: "general", label: "General", href: `/workspaces/${workspaceID}/settings/general` }] : []),
      { id: "members", label: "Members & access", href: `/workspaces/${workspaceID}/settings/members`, badge: <Badge size="sm" variant="secondary">{members.length}</Badge> },
    ] },
    ...(isOwner ? [{ label: "Delivery", items: [
      { id: "publishing", label: "Publishing profiles", href: `/workspaces/${workspaceID}/settings/publishing` },
      { id: "policies", label: "Deployment policies", href: `/workspaces/${workspaceID}/settings/policies` },
      { id: "notifications", label: "Notifications", href: `/workspaces/${workspaceID}/settings/notifications` },
    ] }] : []),
    { label: "Assistant", items: [{ id: "ai", label: "AI", href: `/workspaces/${workspaceID}/settings/ai` }] },
  ]

  return <SettingsLayout
    title="Workspace settings"
    description={`Manage ${workspace.name} · ${workspace.description || "Members, deployment policies, and notifications."}`}
    actions={<Button variant="outline" render={<Link href={`/audit?workspaceId=${encodeURIComponent(workspaceID)}`} />} nativeButton={false}><HugeiconsIcon icon={Audit01Icon} strokeWidth={1.8} aria-hidden="true" />Audit trail</Button>}
    nav={navGroups}
    activeId={activeTab}
    navLabel="Workspace settings sections"
  >
      {!isOwner && <SettingsNotice>Workspace owners manage general settings, deployment policies, and notifications. You can view members and their roles here.</SettingsNotice>}
    {deletionPlans.length > 0 && <Alert tone="warning" variant="plain"><Alert.Icon /><Alert.Title>Deletion plans are ready for review.</Alert.Title><Alert.Description>Approve and apply each application plan. Once all managed resources are gone, choose “Delete workspace” again to remove the workspace record.</Alert.Description><ul className="col-start-2 mt-3 space-y-2">{deletionPlans.map((item) => <li key={item.applicationId}><Button variant="outline" size="sm" nativeButton={false} render={<Link href={`/applications/${item.applicationId}?tab=changes`} />} className="h-auto max-w-full whitespace-normal break-words text-left">{item.applicationName} · {item.managedResources} resources</Button></li>)}</ul></Alert>}

    {isOwner && activeTab === "publishing" && <PublishingProfiles key={workspaceID} workspaceId={workspaceID} />}
    {activeTab === "ai" && <div className="space-y-6">{isOwner && <WorkspaceAIPolicy key={workspaceID} workspaceId={workspaceID} />}<AIAssistant presentation="inline" key={`chat-${workspaceID}`} workspaceId={workspaceID} /></div>}
    {activeTab === "members" && <div className="min-w-0">
        <AppDialog open={memberOpen} onOpenChange={setMemberOpen} busy={memberBusy} size="md" title="Add member" description="Give a teammate access to this workspace.">
          <form className="space-y-4" onSubmit={addMember}>
            <FormField label="User email" htmlFor="member-email" hint="They must have signed in to JustCD at least once."><Input id="member-email" type="email" placeholder="teammate@example.com" value={memberEmail} onChange={(event) => setMemberEmail(event.target.value)} required /></FormField>
            <FormField label="Workspace role" htmlFor="member-role"><FormSelect id="member-role" value={memberRole} onValueChange={setMemberRole} items={roleItems} /></FormField>
            <DialogFooter><DialogCancel disabled={memberBusy} /><Button type="submit" loading={memberBusy} loadingText="Adding member…">Add member</Button></DialogFooter>
          </form>
        </AppDialog>
        <SettingsPanel title="Members" description="Users with access to this workspace" action={isOwner ? <Button size="sm" onClick={() => { setMemberEmail(""); setMemberRole("viewer"); setMemberOpen(true) }}><HugeiconsIcon icon={UserAdd01Icon} strokeWidth={1.8} aria-hidden="true" />Add member</Button> : undefined}>
          {members.length ? <ul className="divide-y">{members.map((member) => {
            const name = member.displayName || member.email
            return <li key={member.id} className="flex flex-wrap items-center gap-3 px-5 py-3">
              <Avatar name={name} size="sm" color="muted" aria-hidden="true" />
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-2"><span className="block truncate text-sm font-medium">{name}</span>{member.disabled && <Badge size="sm"  variant="warning">Locked</Badge>}{member.managedBySSO && <Badge size="sm"  variant="outline">Managed by SSO</Badge>}</span>
                <span className="block truncate text-sm text-muted-foreground">{member.email}</span>
              </span>
              {isOwner && member.editable
                ? <div className="flex shrink-0 items-center gap-1.5"><FormSelect ariaLabel={`Workspace role for ${name}`} size="sm" className="max-w-32 min-w-28" value={member.role} disabled={memberActionId !== null} onValueChange={(role) => { if (role !== member.role) void changeMemberRole(member, role) }} items={roleItems} /><RowActions label={name} items={[{ label: "Remove from workspace", icon: Delete02Icon, destructive: true, disabled: memberActionId !== null, confirm: { title: `Remove ${name}?`, description: "They will lose access to this workspace and its applications immediately. Their JustCD account and activity history will remain.", confirmLabel: "Remove member", onConfirm: () => removeMember(member) } }]} /></div>
                : <span className="text-sm capitalize text-muted-foreground">{member.role}</span>}
            </li>
          })}</ul> : <EmptyState title="No additional members yet" description={isOwner ? "The workspace owner is the only member so far. Use “Add member” to invite a teammate." : "The workspace owner is the only member so far."} />}
        </SettingsPanel>
      </div>}
    {isOwner && (activeTab === "general" || activeTab === "policies" || activeTab === "notifications") && <WorkspaceSettings key={`${workspace.id}:${activeTab}`} workspace={workspace} members={members} section={activeTab} onUpdated={setWorkspace} onDelete={deleteWorkspace} />}
  </SettingsLayout>
}
