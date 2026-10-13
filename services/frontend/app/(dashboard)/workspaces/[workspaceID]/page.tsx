"use client"

import Link from "next/link"
import { useParams, useRouter, useSearchParams } from "next/navigation"
import { Suspense, useEffect, useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { SectionSkeleton } from "@/components/settings/shared"
import { HugeiconsIcon } from "@hugeicons/react"
import { Delete02Icon, UserAdd01Icon } from "@hugeicons/core-free-icons"
import { RowActions } from "@/components/action-menu"
import { Badge } from "@/components/reui/badge"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { WorkspaceSettings } from "@/components/workspace-settings"
import { ErrorNotice } from "@/components/workspace-ui"
import { EmptyState, FormField, PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { ListResponse, Workspace, WorkspaceMember } from "@/lib/types"

export default function WorkspaceDetailPage() {
  return <Suspense fallback={<SectionSkeleton label="Loading workspace settings" />}><WorkspaceSettingsPage /></Suspense>
}

function WorkspaceSettingsPage() {
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
  const requestedTab = searchParams.get("tab")
  const isOwner = workspace?.role === "owner"
  const activeTab = isOwner
    ? (["general", "members", "policies", "notifications"].includes(requestedTab ?? "") ? requestedTab! : "general")
    : "members"

  // Preserve old bookmarks without keeping the duplicate connections directory.
  useEffect(() => {
    if (requestedTab === "connections" && workspace && loadedWorkspaceId === workspaceID) {
      router.replace(workspace.role === "viewer" ? `/workspaces/${workspaceID}?tab=members` : `/workspaces/${workspaceID}/connections/git-sources`)
    }
  }, [requestedTab, workspace, workspaceID, loadedWorkspaceId, router])

  function selectTab(value: string) {
    const url = new URL(window.location.href)
    url.searchParams.set("tab", value)
    window.history.pushState(null, "", url)
  }

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

  if (loading || loadedWorkspaceId !== workspaceID || (requestedTab === "connections" && workspace)) return <SectionSkeleton label="Loading workspace settings" />
  if (error) return <ErrorNotice error={error} />
  if (!workspace) return <EmptyState title="Workspace unavailable" description="Choose a workspace you have access to." href="/workspaces" action="View workspaces" />

  const roleItems = [{ value: "viewer", label: "Viewer" }, { value: "deployer", label: "Deployer" }, { value: "owner", label: "Owner" }]
  return <>
    <PageHeading title="Workspace settings" description={`Manage ${workspace.name} · ${workspace.description || "Members, deployment policies, and notifications."}`} />
    {!isOwner && <p className="mb-5 text-sm text-muted-foreground">Workspace owners manage general settings, deployment policies, and notifications. You can view members and their roles here.</p>}
    {deletionPlans.length > 0 && <div role="status" className="mb-5 rounded-lg border border-warning/40 bg-warning/10 p-4 text-sm"><p className="font-medium">Deletion plans are ready for review.</p><p className="mt-1 text-sm text-muted-foreground">Approve and apply each application plan. Once all managed resources are gone, choose “Delete workspace” again to remove the workspace record.</p><ul className="mt-3 space-y-1">{deletionPlans.map((item) => <li key={item.applicationId}><Link href={`/applications/${item.applicationId}?tab=changes`} className="font-medium underline underline-offset-4">{item.applicationName} · {item.managedResources} resources</Link></li>)}</ul></div>}
    <Tabs value={activeTab} onValueChange={(value) => selectTab(String(value))}>
      <div className="sticky top-0 z-10 mb-5 border-b bg-background py-2">
        <TabsList variant="line" aria-label="Workspace settings sections" className="grid w-full grid-cols-2 gap-x-2 gap-y-3 group-data-horizontal/tabs:h-auto sm:flex sm:w-fit sm:gap-4 sm:group-data-horizontal/tabs:h-10" activateOnFocus={false}>
          {isOwner && <TabsTrigger className="h-10 justify-start whitespace-normal text-left leading-4 sm:h-9 sm:justify-center sm:whitespace-nowrap" value="general">General</TabsTrigger>}
          <TabsTrigger className="h-10 justify-start whitespace-normal text-left leading-4 sm:h-9 sm:justify-center sm:whitespace-nowrap" value="members">Members &amp; access <Badge size="xs" variant="secondary">{members.length}</Badge></TabsTrigger>
          {isOwner && <TabsTrigger className="h-10 justify-start whitespace-normal text-left leading-4 sm:h-9 sm:justify-center sm:whitespace-nowrap" value="policies">Deployment policies</TabsTrigger>}
          {isOwner && <TabsTrigger className="h-10 justify-start whitespace-normal text-left leading-4 sm:h-9 sm:justify-center sm:whitespace-nowrap" value="notifications">Notifications</TabsTrigger>}
        </TabsList>
      </div>
      <TabsContent value="members" className="min-w-0">
        <AppDialog open={memberOpen} onOpenChange={setMemberOpen} busy={memberBusy} size="md" title="Add member" description="Give a teammate access to this workspace.">
          <form className="space-y-4" onSubmit={addMember}>
            <FormField label="User email" htmlFor="member-email" hint="They must have signed in to JustCD at least once."><Input id="member-email" type="email" placeholder="teammate@example.com" value={memberEmail} onChange={(event) => setMemberEmail(event.target.value)} required /></FormField>
            <FormField label="Workspace role" htmlFor="member-role"><FormSelect id="member-role" value={memberRole} onValueChange={setMemberRole} items={roleItems} /></FormField>
            <DialogFooter><DialogCancel disabled={memberBusy} /><Button type="submit" loading={memberBusy} loadingText="Adding member…">Add member</Button></DialogFooter>
          </form>
        </AppDialog>
        <Panel title="Members" description="Users with access to this workspace" action={isOwner ? <Button size="sm" onClick={() => { setMemberEmail(""); setMemberRole("viewer"); setMemberOpen(true) }}><HugeiconsIcon icon={UserAdd01Icon} strokeWidth={1.8} aria-hidden="true" />Add member</Button> : undefined}>
          {members.length ? <ul className="divide-y">{members.map((member) => {
            const name = member.displayName || member.email
            return <li key={member.id} className="flex flex-wrap items-center gap-3 px-5 py-3">
              <span aria-hidden="true" className="grid size-8 shrink-0 place-items-center rounded-full bg-muted text-xs font-semibold uppercase">{name.slice(0, 2)}</span>
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-2"><span className="block truncate text-sm font-medium">{name}</span>{member.disabled && <Badge size="xs" radius="full" variant="warning-light">Locked</Badge>}{member.managedBySSO && <Badge size="xs" radius="full" variant="outline">Managed by SSO</Badge>}</span>
                <span className="block truncate text-sm text-muted-foreground">{member.email}</span>
              </span>
              {isOwner && member.editable
                ? <div className="flex shrink-0 items-center gap-1.5"><FormSelect ariaLabel={`Workspace role for ${name}`} size="sm" className="max-w-32 min-w-28" value={member.role} disabled={memberActionId !== null} onValueChange={(role) => { if (role !== member.role) void changeMemberRole(member, role) }} items={roleItems} /><RowActions label={name} items={[{ label: "Remove from workspace", icon: Delete02Icon, destructive: true, disabled: memberActionId !== null, confirm: { title: `Remove ${name}?`, description: "They will lose access to this workspace and its applications immediately. Their JustCD account and activity history will remain.", confirmLabel: "Remove member", onConfirm: () => removeMember(member) } }]} /></div>
                : <span className="text-sm capitalize text-muted-foreground">{member.role}</span>}
            </li>
          })}</ul> : <EmptyState title="No additional members yet" description={isOwner ? "The workspace owner is the only member so far. Use “Add member” to invite a teammate." : "The workspace owner is the only member so far."} />}
        </Panel>
      </TabsContent>
      {isOwner && (["general", "policies", "notifications"] as const).map((section) => <TabsContent key={section} value={section}>
        <WorkspaceSettings key={`${workspace.id}:${section}`} workspace={workspace} members={members} section={section} onUpdated={setWorkspace} onDelete={deleteWorkspace} />
      </TabsContent>)}
    </Tabs>
  </>
}
