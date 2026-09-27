"use client"

import Link from "next/link"
import { useParams, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Tabs } from "@base-ui/react/tabs"
import { Button } from "@/components/ui/button"
import { FormSelect } from "@/components/ui/form-select"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ConfirmDisclosure } from "@/components/confirm-disclosure"
import { ApplicationCollection } from "@/components/application-collection"
import { ApprovalRuleEditor } from "@/components/approval-rule-editor"
import { ActionLink, CollectionSkeleton, ErrorNotice } from "@/components/workspace-ui"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { api, apiPost, errorMessage } from "@/lib/api"
import type { ApprovalPolicy, Application, ListResponse, Workspace, WorkspaceMember } from "@/lib/types"

export default function WorkspaceDetailPage() {
  const { workspaceID } = useParams<{ workspaceID: string }>()
  const router = useRouter()
  const toast = useToast()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [applications, setApplications] = useState<Application[]>([])
  const [members, setMembers] = useState<WorkspaceMember[]>([])
  const [approvalPolicy, setApprovalPolicy] = useState<ApprovalPolicy>({
    sync: { requiredApprovals: 0, approverRoles: ["owner"], approverUserIds: [] },
    deletion: { requiredApprovals: 1, approverRoles: ["owner"], approverUserIds: [] },
  })
  const [error, setError] = useState<unknown | null>(null)
  const [loading, setLoading] = useState(true)
  const [memberEmail, setMemberEmail] = useState("")
  const [memberRole, setMemberRole] = useState("viewer")
  const [memberBusy, setMemberBusy] = useState(false)
  const [memberActionId, setMemberActionId] = useState<string | null>(null)
  const [editName, setEditName] = useState("")
  const [editDescription, setEditDescription] = useState("")
  const [workspaceBusy, setWorkspaceBusy] = useState(false)
  const [approvalPolicyBusy, setApprovalPolicyBusy] = useState(false)
  const [deletePolicy, setDeletePolicy] = useState("keep")
  const [deletionPlans, setDeletionPlans] = useState<{ applicationId: string; applicationName: string; managedResources: number }[]>([])
  const [activeTab, setActiveTab] = useState("applications")

  useEffect(() => {
    const readTab = () => {
      const value = new URLSearchParams(window.location.search).get("tab")
      setActiveTab(["applications", "members", "connections", "settings"].includes(value ?? "") ? value! : "applications")
    }
    readTab()
    window.addEventListener("popstate", readTab)
    return () => window.removeEventListener("popstate", readTab)
  }, [])

  function selectTab(value: string) {
    setActiveTab(value)
    const url = new URL(window.location.href)
    if (value === "applications") url.searchParams.delete("tab")
    else url.searchParams.set("tab", value)
    window.history.pushState(null, "", url)
  }

  useEffect(() => {
    let active = true
    async function load() {
      try {
        const [workspaces, apps, workspaceMembers] = await Promise.all([
          api<ListResponse<Workspace>>("/api/v1/workspaces"),
          api<ListResponse<Application>>(`/api/v1/applications?workspaceId=${encodeURIComponent(workspaceID)}`),
          api<ListResponse<WorkspaceMember>>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/members`),
        ])
        if (!active) return
        const selected = workspaces.items.find((item) => item.id === workspaceID) ?? null
        setWorkspace(selected)
        if (selected) setApprovalPolicy(selected.approvalPolicy)
        setEditName(selected?.name ?? "")
        setEditDescription(selected?.description ?? "")
        setApplications(apps.items)
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
      setMembers(result.items); setMemberEmail("")
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

  async function saveWorkspace(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setWorkspaceBusy(true)
    try {
      await api(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}`, { method: "PUT", body: JSON.stringify({ name: editName, description: editDescription }) })
      setWorkspace((current) => current ? { ...current, name: editName.trim(), description: editDescription.trim() } : current)
      toast.success("Workspace settings saved.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setWorkspaceBusy(false) }
  }

  async function saveApprovalPolicy(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setApprovalPolicyBusy(true)
    try {
      const result = await api<{ approvalPolicy: ApprovalPolicy }>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/approval-policy`, { method: "PUT", body: JSON.stringify(approvalPolicy) })
      setApprovalPolicy(result.approvalPolicy)
      setWorkspace((current) => current ? { ...current, approvalPolicy: result.approvalPolicy } : current)
      toast.success("Workspace approval rules saved. Existing plans are stale.")
    } catch (cause) { toast.error(errorMessage(cause), cause) }
    finally { setApprovalPolicyBusy(false) }
  }

  async function deleteWorkspace() {
    const result = await api<{ deleted: boolean; plans?: typeof deletionPlans }>(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}?resources=${deletePolicy}`, { method: "DELETE" })
    if (result.deleted) { toast.success("Workspace deleted."); router.push("/workspaces"); router.refresh(); return }
    setDeletionPlans(result.plans ?? [])
    setError(null)
  }

  return <>
    <PageHeading title={workspace?.name ?? (loading ? "Loading workspace…" : "Workspace not found")} description={workspace?.description || "Manage Git-driven applications and access scoped to this workspace."} actions={workspace && <>{workspace.role !== "viewer" && <ActionLink href={`/applications/new?workspaceId=${workspace.id}`}><span aria-hidden="true">＋</span> New application</ActionLink>}</>} />
    {error && <ErrorNotice error={error} />}
    {deletionPlans.length > 0 && <div role="status" className="mb-5 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-200"><p className="font-medium">Deletion plans are ready for review.</p><p className="mt-1 text-xs">Approve and apply each application plan. Once all managed resources are gone, choose “Delete resources” here again to remove the workspace record.</p><ul className="mt-3 space-y-1">{deletionPlans.map((item) => <li key={item.applicationId}><Link href={`/applications/${item.applicationId}?tab=changes`} className="underline underline-offset-4">{item.applicationName} · {item.managedResources} resources →</Link></li>)}</ul></div>}
    <Tabs.Root value={activeTab} onValueChange={(value) => selectTab(String(value))}>
      <Tabs.List aria-label="Workspace views" className="mb-6 flex gap-6 overflow-x-auto border-b" activateOnFocus>
        {[["applications", "Applications", applications.length], ["members", "Members", members.length], ...(workspace && workspace.role !== "viewer" ? [["connections", "Connections", null]] : []), ...(workspace?.role === "owner" ? [["settings", "Workspace settings", null]] : [])].map(([value, label, count]) => <Tabs.Tab key={String(value)} value={String(value)} className="flex shrink-0 items-center gap-2 border-b-2 border-transparent px-1 pb-3 text-xs font-medium text-muted-foreground outline-none transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring data-[active]:border-primary data-[active]:text-foreground">{label}{count !== null && <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground">{count}</span>}</Tabs.Tab>)}
      </Tabs.List>
      <Tabs.Panel value="applications" className="outline-none">
        <Panel surface="flat" title="Applications" description="Deployments reconciled by this workspace">
          {loading ? <CollectionSkeleton /> : <ApplicationCollection applications={applications} workspaceRole={workspace?.role} createHref={`/applications/new?workspaceId=${workspaceID}`} canCreate={workspace?.role !== "viewer"} onDeleted={(id) => setApplications((current) => current.filter((app) => app.id !== id))} />}
        </Panel>
      </Tabs.Panel>
      <Tabs.Panel value="members" className="max-w-3xl outline-none">
        <Panel title="Members" description="Users with access to this delivery scope">
          {workspace?.role === "owner" && <form className="grid gap-3 border-b p-4" onSubmit={addMember}>
            <FormField label="User email" htmlFor="member-email" hint="They must have signed in to JustCD at least once."><Input id="member-email" type="email" placeholder="teammate@example.com" value={memberEmail} onChange={(event) => setMemberEmail(event.target.value)} required /></FormField>
            <FormField label="Workspace role" htmlFor="member-role"><FormSelect id="member-role" size="sm" value={memberRole} onValueChange={setMemberRole} items={[{ value: "viewer", label: "Viewer" }, { value: "deployer", label: "Deployer" }, { value: "owner", label: "Owner" }]} /></FormField>
            <Button size="sm" type="submit" className="w-fit" loading={memberBusy} loadingText="Adding member…">Add member</Button>
          </form>}
          {members.length ? <div className="divide-y">{members.map((member) => <div key={member.id} className="flex flex-wrap items-center gap-3 px-5 py-3"><span className="grid size-8 shrink-0 place-items-center rounded-full bg-muted text-[10px] font-semibold uppercase">{(member.displayName || member.email).slice(0, 2)}</span><span className="min-w-0 flex-1"><span className="block truncate text-xs font-medium">{member.displayName || member.email}</span><span className="block truncate text-[10px] text-muted-foreground">{member.email}</span><span className="mt-1 flex flex-wrap gap-1.5">{member.disabled && <span className="rounded-full border border-amber-500/30 bg-amber-500/5 px-2 py-0.5 text-[9px] text-amber-700 dark:text-amber-300">Locked</span>}{member.managedBySSO && <span className="rounded-full border px-2 py-0.5 text-[9px] text-muted-foreground">Managed by SSO</span>}</span></span>{workspace?.role === "owner" && member.editable ? <><FormSelect ariaLabel={`Workspace role for ${member.displayName || member.email}`} size="sm" className="max-w-32 min-w-28" value={member.role} disabled={memberActionId !== null} onValueChange={(role) => { if (role !== member.role) void changeMemberRole(member, role) }} items={[{ value: "viewer", label: "Viewer" }, { value: "deployer", label: "Deployer" }, { value: "owner", label: "Owner" }]} /><ConfirmDisclosure trigger="Remove" triggerVariant="outline" title={`Remove ${member.displayName || member.email}?`} description="They will lose access to this workspace and its applications immediately. Their JustCD account and activity history will remain." confirmLabel="Remove member" onConfirm={() => removeMember(member)} disabled={memberActionId !== null} /></> : <span className="text-[10px] capitalize text-muted-foreground">{member.role}</span>}</div>)}</div> : <p className="px-5 py-6 text-xs text-muted-foreground">Workspace owner is the only member so far.</p>}
        </Panel>
      </Tabs.Panel>
      {workspace && workspace.role !== "viewer" && <Tabs.Panel value="connections" className="outline-none">
        <div className="mb-5 flex flex-wrap items-end justify-between gap-4"><div><h2 className="text-sm font-semibold">Workspace connections</h2><p className="mt-1 text-xs text-muted-foreground">Manage the repositories, targets, and secrets used by {workspace?.name ?? "this workspace"}.</p></div>{workspace.role === "owner" && <Link href={`/workspaces/${workspaceID}/clusters/new`}><Button>Connect cluster</Button></Link>}</div>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[
            { id: "git-sources", title: "Git sources", description: "Repositories tracked by this workspace" },
            { id: "clusters", title: "Kubernetes clusters", description: "Cluster endpoints and authentication" },
            { id: "namespaces", title: "Namespace bindings", description: "Target namespaces and access" },
            { id: "credentials", title: "Credentials", description: "Encrypted Git and Kubernetes secrets" },
            { id: "shares", title: "Shared connections", description: "Offers, accepted shares, and access status" },
          ].map((item) => <Link key={item.id} href={`/workspaces/${workspaceID}/connections/${item.id}`} className="workspace-card group rounded-xl border bg-card p-5"><h3 className="text-sm font-semibold group-hover:text-primary">{item.title}</h3><p className="mt-2 text-xs leading-5 text-muted-foreground">{item.description}</p><span className="mt-5 inline-block text-xs font-medium text-primary">Configure →</span></Link>)}
        </div>
      </Tabs.Panel>}
      {workspace?.role === "owner" && <Tabs.Panel value="settings" className="grid min-w-0 gap-5 outline-none xl:grid-cols-2 xl:items-start">
        <div className="min-w-0 space-y-5">
        <Panel title="Workspace settings" description="Change the workspace name or remove this delivery scope."><div className="space-y-5 p-5"><form className="space-y-3" onSubmit={saveWorkspace}><FormField label="Workspace name" htmlFor="edit-workspace-name"><Input id="edit-workspace-name" value={editName} onChange={(event) => setEditName(event.target.value)} required maxLength={100} /></FormField><FormField label="Description" htmlFor="edit-workspace-description"><Textarea id="edit-workspace-description" value={editDescription} onChange={(event) => setEditDescription(event.target.value)} maxLength={500} /></FormField><Button size="sm" type="submit" loading={workspaceBusy} loadingText="Saving workspace…">Save workspace</Button></form><div className="border-t pt-4"><p className="mb-3 text-xs text-muted-foreground">Deleting this workspace removes its applications and workspace-scoped connections from JustCD.</p><ConfirmDisclosure trigger="Delete workspace" title={`Delete ${workspace.name}?`} description="Choose what happens to resources managed by applications in this workspace. This cannot be undone in JustCD." confirmLabel="Continue" onConfirm={deleteWorkspace}><FormField label="Managed cluster resources" htmlFor="workspace-delete-policy"><FormSelect id="workspace-delete-policy" value={deletePolicy} onValueChange={setDeletePolicy} items={[{ value: "keep", label: "Keep resources in Kubernetes" }, { value: "delete", label: "Delete resources through reviewed plans" }]} /></FormField>{deletePolicy === "delete" && <p className="mt-2 text-xs text-muted-foreground">JustCD prepares deletion plans for each application with managed resources. The workspace is removed only after all plans have been approved, applied, and the inventory is empty.</p>}</ConfirmDisclosure></div></div></Panel>
        <div className="rounded-xl border bg-card p-5"><p className="text-xs font-semibold">Scoped by design</p><p className="mt-2 text-xs leading-5 text-muted-foreground">Git credentials, cluster targets, and namespace bindings are attached to this workspace. Applications cannot deploy outside those namespace bindings.</p><button type="button" onClick={() => selectTab("connections")} className="mt-3 text-xs font-medium text-primary hover:underline">Review workspace connections →</button></div>
        </div>
        <Panel title="Approval rules" description="Set workspace defaults for syncs and application deletion. Application settings can override either rule.">
          <form className="space-y-4 p-5" onSubmit={saveApprovalPolicy}>
            <ApprovalRuleEditor id="workspace-sync-approvals" title="Sync approvals" rule={approvalPolicy.sync} members={members} disabled={approvalPolicyBusy} onChange={(sync) => setApprovalPolicy((current) => ({ ...current, sync }))} />
            <ApprovalRuleEditor id="workspace-deletion-approvals" title="Application deletion approvals" rule={approvalPolicy.deletion} members={members} deletion disabled={approvalPolicyBusy} onChange={(deletion) => setApprovalPolicy((current) => ({ ...current, deletion }))} />
            <div className="flex justify-end"><Button size="sm" type="submit" loading={approvalPolicyBusy} loadingText="Saving rules…">Save approval rules</Button></div>
          </form>
        </Panel>
      </Tabs.Panel>}
    </Tabs.Root>
  </>
}
