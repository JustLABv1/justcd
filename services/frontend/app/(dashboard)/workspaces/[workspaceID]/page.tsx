"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Tabs } from "@base-ui/react/tabs"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Delete02Icon, Key01Icon, ServerStack01Icon, GitBranchIcon, Folder01Icon, UserAdd01Icon } from "@hugeicons/core-free-icons"
import { ActionMenu, RowActions } from "@/components/action-menu"
import { workspaceConnectionSections } from "@/components/connections/sections"
import { Badge } from "@/components/reui/badge"
import { AppDialog, DialogCancel, DialogFooter } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { WorkspaceSettings } from "@/components/workspace-settings"
import { ErrorNotice } from "@/components/workspace-ui"
import { ConnectionRow, EmptyState, FormField, PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { ListResponse, Workspace, WorkspaceMember } from "@/lib/types"

export default function WorkspaceDetailPage() {
  const { workspaceID } = useParams<{ workspaceID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [error, setError] = useState<unknown | null>(null)
  const [loading, setLoading] = useState(true)
  const [memberEmail, setMemberEmail] = useState("")
  const [memberRole, setMemberRole] = useState("viewer")
  const [memberBusy, setMemberBusy] = useState(false)
  const [memberOpen, setMemberOpen] = useState(false)
  const [memberActionId, setMemberActionId] = useState<string | null>(null)
  const [deletionPlans, setDeletionPlans] = useState<{ applicationId: string; applicationName: string; managedResources: number }[]>([])
  const [activeTab, setActiveTab] = useState("members")

  useEffect(() => {
    const readTab = () => {
      const value = new URLSearchParams(window.location.search).get("tab")
      setActiveTab(["members", "connections", "settings"].includes(value ?? "") ? value! : "members")
    }
    readTab()
    window.addEventListener("popstate", readTab)
    return () => window.removeEventListener("popstate", readTab)
  }, [])

  function selectTab(value: string) {
    setActiveTab(value)
    const url = new URL(window.location.href)
    if (value === "members") url.searchParams.delete("tab")
    else url.searchParams.set("tab", value)
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
      } catch (cause) {
        if (active) setError(cause)
      } finally { if (active) setLoading(false) }
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

  const isOwner = workspace?.role === "owner"
  const roleItems = [{ value: "viewer", label: "Viewer" }, { value: "deployer", label: "Deployer" }, { value: "owner", label: "Owner" }]
  return <>
    <PageHeading
      title={workspace?.name ?? (loading ? "Loading workspace…" : "Workspace not found")}
      description={workspace?.description || "Manage Git-driven applications and access scoped to this workspace."}
      actions={workspace && activeTab === "connections" && isOwner
        ? <ActionMenu label="Connect" variant="default" icon={Add01Icon} items={[
          { label: "Cluster", icon: ServerStack01Icon, href: `/workspaces/${workspace.id}/clusters/new` },
          { label: "Git source", icon: GitBranchIcon, href: `/workspaces/${workspace.id}/git-sources/new` },
          { label: "Credential", icon: Key01Icon, href: `/workspaces/${workspace.id}/connections/credentials` },
          { label: "Namespace access", icon: Folder01Icon, href: `/workspaces/${workspace.id}/connections/namespaces` },
        ]} />
        : undefined}
    />
    {error && <ErrorNotice error={error} />}
    {deletionPlans.length > 0 && <div role="status" className="mb-5 rounded-lg border border-warning/40 bg-warning/10 p-4 text-sm"><p className="font-medium">Deletion plans are ready for review.</p><p className="mt-1 text-sm text-muted-foreground">Approve and apply each application plan. Once all managed resources are gone, choose “Delete workspace” again to remove the workspace record.</p><ul className="mt-3 space-y-1">{deletionPlans.map((item) => <li key={item.applicationId}><Link href={`/applications/${item.applicationId}?tab=changes`} className="font-medium underline underline-offset-4">{item.applicationName} · {item.managedResources} resources</Link></li>)}</ul></div>}
    <Tabs.Root value={activeTab} onValueChange={(value) => selectTab(String(value))}>
      <Tabs.List aria-label="Workspace views" className="mb-6 flex gap-6 overflow-x-auto border-b" activateOnFocus>
        {[["members", "Members", members.length], ...(workspace && workspace.role !== "viewer" ? [["connections", "Connections", null]] : []), ...(isOwner ? [["settings", "Workspace settings", null]] : [])].map(([value, label, count]) => <Tabs.Tab key={String(value)} value={String(value)} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">{label}{count !== null && <Badge size="xs" variant="secondary">{count}</Badge>}</Tabs.Tab>)}
      </Tabs.List>
      <Tabs.Panel value="members" className="max-w-3xl outline-none">
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
      </Tabs.Panel>
      {workspace && workspace.role !== "viewer" && <Tabs.Panel value="connections" className="max-w-3xl outline-none">
        <Panel title="Workspace connections" description={`Repositories, targets, and secrets used by ${workspace.name}.`}>
          <ul className="divide-y">
            {workspaceConnectionSections.map((item) => <ConnectionRow
              key={item.id}
              icon={<HugeiconsIcon icon={item.icon} strokeWidth={1.8} className="size-4" />}
              title={item.title}
              meta={item.description}
              actions={<Button size="sm" variant="outline" aria-label={`Manage ${item.title}`} render={<Link href={`/workspaces/${workspaceID}/connections/${item.id}`} />}>Manage</Button>}
            />)}
          </ul>
        </Panel>
      </Tabs.Panel>}
      {isOwner && workspace && <Tabs.Panel value="settings" className="min-w-0 outline-none">
        <WorkspaceSettings key={workspace.id} workspace={workspace} members={members} onUpdated={setWorkspace} onDelete={deleteWorkspace} />
      </Tabs.Panel>}
    </Tabs.Root>
  </>
}
