"use client"

import { Section } from "@/components/section"

import Link from "next/link"
import { useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/panelui/button"
import { Input } from "@/components/panelui/input"
import { Textarea } from "@/components/panelui/textarea"
import { EmptyState, FormField, PageHeading } from "@/components/ui-kit"
import { useWorkspaceSelection } from "@/hooks/workspace-selection"
import { useToast } from "@/components/toast-provider"
import { apiPost, errorMessage } from "@/lib/api"
import type { Workspace } from "@/lib/types"

export default function NewWorkspacePage() {
  const router = useRouter()
  const toast = useToast()
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [busy, setBusy] = useState(false)
  const { canCreateWorkspace } = useWorkspaceSelection()
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true)
    try {
      const workspace = await apiPost<Workspace>("/api/v1/workspaces", { name, description })
      toast.success("Workspace created.")
      router.push(`/workspaces/${workspace.id}/git-sources/new`)
    } catch (cause) { toast.error(errorMessage(cause), cause) } finally { setBusy(false) }
  }
  if (!canCreateWorkspace) return <EmptyState title="Workspace creation is limited to administrators" description="Ask an instance administrator to create a workspace for your team or to add you to an existing one." href="/workspaces" action="View workspaces" />
  return <>
    <PageHeading title="Create workspace" description="A workspace groups your connections, applications, and members." />
    <div className="max-w-2xl"><Section><form className="space-y-5" onSubmit={submit}>
      <FormField label="Workspace name" htmlFor="workspace-name"><Input id="workspace-name" placeholder="payments-platform" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} /></FormField>
      <FormField label="Description" htmlFor="workspace-description" hint="Optional. Shown to workspace members."><Textarea id="workspace-description" className="min-h-24" placeholder="Services and infrastructure for…" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} /></FormField>
      <div className="flex justify-end gap-2 pt-4"><Button nativeButton={false} variant="outline" disabled={busy} render={<Link href="/workspaces" />}>Cancel</Button><Button type="submit" loading={busy} loadingText="Creating workspace…">Create workspace</Button></div>
    </form></Section></div>
  </>
}
