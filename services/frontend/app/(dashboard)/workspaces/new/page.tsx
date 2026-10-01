"use client"

import Link from "next/link"
import { useState } from "react"
import { useRouter } from "next/navigation"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { FormField, PageHeading, Panel } from "@/components/ui-kit"
import { useToast } from "@/components/toast-provider"
import { apiPost, errorMessage } from "@/lib/api"
import type { Workspace } from "@/lib/types"

export default function NewWorkspacePage() {
  const router = useRouter()
  const toast = useToast()
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true)
    try {
      const workspace = await apiPost<Workspace>("/api/v1/workspaces", { name, description })
      toast.success("Workspace created.")
      router.push(`/workspaces/${workspace.id}?tab=connections`)
    } catch (cause) { toast.error(errorMessage(cause), cause) } finally { setBusy(false) }
  }
  return <>
    <PageHeading title="Create workspace" description="A workspace groups your connections, applications, and members." />
    <div className="max-w-2xl"><Panel><form className="space-y-5 p-5" onSubmit={submit}>
      <FormField label="Workspace name" htmlFor="workspace-name"><Input id="workspace-name" placeholder="payments-platform" value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} /></FormField>
      <FormField label="Description" htmlFor="workspace-description" hint="Optional. Shown to workspace members."><Textarea id="workspace-description" className="min-h-24" placeholder="Services and infrastructure for…" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} /></FormField>
      <div className="flex justify-end gap-2 border-t pt-4"><Button type="button" variant="outline" disabled={busy} render={<Link href="/workspaces" />}>Cancel</Button><Button type="submit" loading={busy} loadingText="Creating workspace…">Create workspace</Button></div>
    </form></Panel></div>
  </>
}
